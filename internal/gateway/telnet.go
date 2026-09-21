package gateway

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/creack/pty"
)

// RunTelnetServer listens on every port in listenPorts, dropping each
// accepted connection straight into a `<wmsBinaryPath> tui` session over a
// PTY, mirroring telnet_server.py's handle_telnet_session.
func RunTelnetServer(ctx context.Context, listenHost string, listenPorts []int, wmsBinaryPath string) error {
	var wg sync.WaitGroup
	th := NewThrottle()

	for _, port := range listenPorts {
		ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", listenHost, port))
		if err != nil {
			return fmt.Errorf("listening on port %d: %w", port, err)
		}
		wg.Add(1)
		go func(ln net.Listener) {
			defer wg.Done()
			acceptLoop(ctx, ln, wmsBinaryPath, th)
		}(ln)

		go func(ln net.Listener) {
			<-ctx.Done()
			_ = ln.Close()
		}(ln)
	}

	wg.Wait()
	return nil
}

func acceptLoop(ctx context.Context, ln net.Listener, wmsBinaryPath string, th *Throttle) {
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
		go handleTelnetSession(ctx, conn, wmsBinaryPath, th)
	}
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

func handleTelnetSession(ctx context.Context, conn net.Conn, wmsBinaryPath string, th *Throttle) {
	defer conn.Close()

	host, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
	release, why := th.Admit(host)
	if release == nil {
		_, _ = conn.Write([]byte(why + "\r\n"))
		return
	}
	defer release()

	if _, err := conn.Write(negotiationPreamble); err != nil {
		return
	}

	cmd := exec.CommandContext(ctx, wmsBinaryPath, "tui")
	cmd.Env = append(childEnv(), "WMS_TRANSPORT=telnet")
	if host != "" {
		// Lets the TUI pin a 2FA grace window to this client's address.
		cmd.Env = append(cmd.Env, "WMS_REMOTE_ADDR="+host)
	}

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return
	}
	defer ptmx.Close()
	// 80x24 (the classic terminal) until the client reports its real window size via NAWS (see
	// FilterIAC/TakeResize below); a client that never does keeps this. 24 rather than 25: a
	// 25-row picture on a 24-row window scrolls the header off, while the reverse only leaves a
	// blank line.
	_ = pty.Setsize(ptmx, &pty.Winsize{Rows: 24, Cols: 80})

	done := make(chan struct{})
	var once sync.Once
	closeDone := func() { once.Do(func() { close(done) }) }

	go func() {
		st := &IACState{}
		buf := make([]byte, 4096)
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				clean := FilterIAC(buf[:n], st)
				if cols, rows, ok := st.TakeResize(); ok {
					// The kernel signals SIGWINCH to the child, so the TUI
					// re-lays itself out at the client's full window size.
					_ = pty.Setsize(ptmx, &pty.Winsize{Rows: clampDim(rows, maxRows), Cols: clampDim(cols, maxCols)})
				}
				if len(clean) > 0 {
					if _, werr := ptmx.Write(clean); werr != nil {
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

	go func() {
		_, _ = io.Copy(conn, ptmx)
		closeDone()
	}()

	<-done
	_ = cmd.Process.Kill()
	waitDone := make(chan struct{})
	go func() { _ = cmd.Wait(); close(waitDone) }()
	select {
	case <-waitDone:
		if cmd.ProcessState != nil && cmd.ProcessState.ExitCode() == ExitTooManyFailures {
			th.Strike(host)
		}
	case <-time.After(time.Second):
	}
}
