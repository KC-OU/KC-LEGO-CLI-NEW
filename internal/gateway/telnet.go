package gateway

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/metrics"
)

// RunTelnetServer listens on every port in listenPorts, dropping each
// accepted connection straight into a `<wmsBinaryPath> tui` session over a
// PTY, mirroring telnet_server.py's handle_telnet_session. testBinaryPath
// and testEnv are "" and nil unless an admin has deliberately turned on the
// Live/Test picker (see WMS_TEST_BINARY_PATH/WMS_TEST_ENV_FILE,
// docs/guides/telnet-and-web.md) — the common, unconfigured case is
// unaffected: no prompt, no behaviour change.
// reconnectGraceSeconds is 0 (off) unless the caller (cmd/wms/gateway.go, from
// config.TelnetReconnectSeconds) turns it on — see reconnectRegistry's doc
// comment for why this defaults off.
func RunTelnetServer(ctx context.Context, listenHost string, listenPorts []int, wmsBinaryPath, testBinaryPath string, testEnv []string, reconnectGraceSeconds int) error {
	var wg sync.WaitGroup
	th := NewThrottle()
	rc := newReconnectRegistry(time.Duration(reconnectGraceSeconds) * time.Second)
	defer rc.closeAll()

	for _, port := range listenPorts {
		ln, err := net.Listen("tcp", net.JoinHostPort(listenHost, strconv.Itoa(port)))
		if err != nil {
			return fmt.Errorf("listening on port %d: %w", port, err)
		}
		wg.Add(1)
		go func(ln net.Listener) {
			defer wg.Done()
			acceptLoop(ctx, ln, wmsBinaryPath, testBinaryPath, testEnv, th, rc)
		}(ln)

		go func(ln net.Listener) {
			<-ctx.Done()
			_ = ln.Close()
		}(ln)
	}

	wg.Wait()
	return nil
}

func acceptLoop(ctx context.Context, ln net.Listener, wmsBinaryPath, testBinaryPath string, testEnv []string, th *Throttle, rc *reconnectRegistry) {
	var backoff time.Duration
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
			}
			// A persistent Accept error (e.g. fd exhaustion) must not spin
			// this loop at 100% CPU; back off like net/http's server does.
			if backoff == 0 {
				backoff = 5 * time.Millisecond
			} else {
				backoff *= 2
			}
			if max := time.Second; backoff > max {
				backoff = max
			}
			time.Sleep(backoff)
			continue
		}
		backoff = 0
		go handleTelnetSession(ctx, conn, wmsBinaryPath, testBinaryPath, testEnv, th, rc)
	}
}

// promptLiveOrTest asks a freshly-connected session whether to open the
// normal (live) session or the dev-build "Test" one — only ever called when
// an admin has configured WMS_TEST_BINARY_PATH/WMS_TEST_ENV_FILE. Reuses the
// same IACState/FilterIAC machinery as the main read loop below so telnet
// protocol bytes (option negotiation, a NAWS window-size report) already in
// the raw stream can't be mistaken for the answer. Anything other than a
// clean "2" — a bare Enter, garbage, a timeout, a dropped connection — means
// Live, same as if this prompt did not exist.
func promptLiveOrTest(conn net.Conn) string {
	_, _ = conn.Write([]byte("1) Live  2) Test (dev build)  [1]: "))
	_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	defer conn.SetReadDeadline(time.Time{})

	st := &IACState{}
	buf := make([]byte, 64)
	var answer []byte
	for len(answer) < 8 {
		n, err := conn.Read(buf)
		if n > 0 {
			answer = append(answer, FilterIAC(buf[:n], st)...)
			if bytes.ContainsAny(answer, "\r\n") {
				break
			}
		}
		if err != nil {
			break
		}
	}
	_, _ = conn.Write([]byte("\r\n"))
	return strings.TrimSpace(string(bytes.Trim(answer, "\r\n")))
}

// childEnv is the environment the spawned `<wms> tui` process runs under.
//
// TERM=dumb (rather than xterm-256color) makes termenv's background-color
// probe (github.com/muesli/termenv's termStatusReport, invoked by
// bubbletea's package init via lipgloss.HasDarkBackground) skip its OSC
// query and the 5-second termenv.OSCTimeout wait entirely — termenv
// special-cases any TERM starting with "screen"/"tmux"/"dumb" as unable to
// answer such queries. A raw telnet client (Windows telnet.exe, BusyBox
// telnet, a Cisco/IBM-style console client, or anything else that isn't a
// real xterm) never answers that query, so without this every single
// connection hung on a black screen for ~5s before the sign-on panel
// appeared — regardless of theme correctness, this alone made the gateway
// unusable as a router/console-style telnet target. COLORTERM=truecolor
// keeps termenv.ColorProfile() picking full color despite TERM=dumb
// otherwise mapping to no color at all — safe here since the theme
// (internal/ui) only ever uses base-16 ANSI color codes, so there's no
// truecolor/256-color rendering to actually lose.
// WMS_GATEWAY_SESSION marks this as a network-reachable session so the TUI
// can require 2FA regardless of the account's local opt-in setting (see
// internal/uiapp login.go) — set here and in internal/gateway/web.go, the
// two ways a session can be spawned over the network instead of locally.
func childEnv() []string {
	return append(os.Environ(), "TERM=dumb", "COLORTERM=truecolor", "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "WMS_GATEWAY_SESSION=1")
}

// Upper bounds on a client-reported window size: the values come straight off
// the network, and nothing legitimate is larger than these.
const (
	maxCols = 500
	maxRows = 200
)

func clampDim(v, max uint16) uint16 {
	if v > max {
		return max
	}
	return v
}

// telnetIdleTimeout closes a connection that has sent nothing at all for this long —
// well past the TUI's own idle-lock (15 min by default), so it only ever catches a
// client that has gone away without a clean close (a dropped Wi-Fi link, a hung NAT),
// not someone reading a screen slowly. Without it, such a connection's child process
// runs forever.
const telnetIdleTimeout = 30 * time.Minute

func handleTelnetSession(ctx context.Context, conn net.Conn, wmsBinaryPath, testBinaryPath string, testEnv []string, th *Throttle, rc *reconnectRegistry) {
	defer conn.Close()

	host, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
	release, why := th.Admit(host)
	if release == nil {
		metrics.TelnetThrottled.Inc()
		_, _ = conn.Write([]byte(why + "\r\n"))
		return
	}
	metrics.TelnetConnections.Inc()
	defer release()

	if _, err := conn.Write(negotiationPreamble); err != nil {
		return
	}

	var sess *session

	if ps := rc.claim(host); ps != nil {
		metrics.TelnetReconnected.Inc()
		sess = ps.sess
	} else {
		binaryPath, extraEnv := wmsBinaryPath, []string(nil)
		if testBinaryPath != "" && promptLiveOrTest(conn) == "2" {
			binaryPath, extraEnv = testBinaryPath, testEnv
		}

		cmd := exec.CommandContext(ctx, binaryPath, "tui")
		cmd.Env = append(mergeEnv(childEnv(), extraEnv), "WMS_TRANSPORT=telnet")
		if host != "" {
			// Lets the TUI pin a 2FA grace window to this client's address.
			cmd.Env = append(cmd.Env, "WMS_REMOTE_ADDR="+host)
		}

		ptmx, err := pty.Start(cmd)
		if err != nil {
			return
		}
		sess = newSession(cmd, ptmx)
	}
	// 80x24 (the classic terminal) until the client reports its real window size via NAWS (see
	// FilterIAC/TakeResize below); a client that never does keeps this. 24 rather than 25: a
	// 25-row picture on a 24-row window scrolls the header off, while the reverse only leaves a
	// blank line. Re-applied on a resumed session too since this is a fresh TCP connection whose
	// own NAWS report (if any) hasn't arrived yet, and the resize also makes the TUI (bubbletea)
	// repaint its full screen — the resumed client's one and only "redraw" after reattaching.
	_ = pty.Setsize(sess.ptmx, &pty.Winsize{Rows: 24, Cols: 80})

	done := make(chan struct{})
	var once sync.Once
	closeDone := func() { once.Do(func() { close(done) }) }

	sess.attach(conn, closeDone)

	go func() {
		st := &IACState{}
		buf := make([]byte, 4096)
		for {
			_ = conn.SetReadDeadline(time.Now().Add(telnetIdleTimeout))
			n, err := conn.Read(buf)
			if n > 0 {
				clean := FilterIAC(buf[:n], st)
				if cols, rows, ok := st.TakeResize(); ok {
					// The kernel signals SIGWINCH to the child, so the TUI
					// re-lays itself out at the client's full window size.
					_ = pty.Setsize(sess.ptmx, &pty.Winsize{Rows: clampDim(rows, maxRows), Cols: clampDim(cols, maxCols)})
				}
				if len(clean) > 0 {
					if _, werr := sess.ptmx.Write(clean); werr != nil {
						closeDone()
						return
					}
				}
			}
			if err != nil {
				closeDone()
				return
			}
		}
	}()

	<-done
	sess.detach(conn)

	// Reconnect enabled and this client is identifiable by address: keep the
	// child alive for the grace window instead of killing it now, so a
	// reconnect from the same host resumes mid-session (see reconnectRegistry).
	if rc != nil && rc.grace > 0 && host != "" {
		rc.park(host, sess)
		return
	}

	killSession(sess)
	if sess.cmd.ProcessState != nil && sess.cmd.ProcessState.ExitCode() == ExitTooManyFailures {
		th.Strike(host)
	}
}
