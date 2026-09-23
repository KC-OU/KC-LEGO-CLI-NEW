package gateway

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/exports"
)

// webSessionName is the tmux session every web-terminal connection shares.
const webSessionName = "wms-web"

// webCommand is the argv ttyd spawns per websocket connection. Without tmux this is a
// fresh `<wmsBinaryPath> tui` every time — a new process with no way to prove "this is
// the same browser that already signed in," so 2FA is asked on every reconnect and the
// terminal state is lost (see internal/uiapp login.go: graceOrigin is "" for web,
// on purpose, because a freshly spawned process can't be trusted with an address).
//
// With tmux on $PATH, every connection instead attaches to the SAME persistent
// session (`-A`: attach if it exists, else create it) — reopening a tab, the PWA
// resuming after being backgrounded, or a dropped mobile connection all resume
// exactly where you left off, already signed in, for as long as the session lives.
// Logging out (Q at the hub) already returns to the sign-on screen within that same
// process via resetSession() — correct for the next person to reconnect into, so
// there is nothing extra to tear down. The session only ends if wms-gateway itself
// restarts, at which point `-A` simply creates a fresh one.
//
// This is one shared screen for every device that opens the web terminal (not one
// per browser) — the simplest option, with no new trust boundary: nothing about who
// you are is inferred from the connection, only "an already-verified session exists."
func webCommand(wmsBinaryPath string) []string {
	if _, err := exec.LookPath("tmux"); err != nil {
		return []string{wmsBinaryPath, "tui"} // no tmux: today's behaviour, one process per connection
	}
	return []string{"tmux", "new-session", "-A", "-s", webSessionName, wmsBinaryPath, "tui"}
}

// RunWebGateway starts ttyd (already installed on this box) wrapping the command from
// webCommand, and a Go-native reverse proxy in front of it —
// replacing the original's hand-rolled byte-pipe with
// httputil.ReverseProxy, which forwards WebSocket upgrade requests
// correctly for a same-process HTTP/1.1 target since it streams the
// hijacked connection rather than buffering it. Both processes are torn
// down together, mirroring start_webtui.sh's kill-all-on-any-exit.
func RunWebGateway(ctx context.Context, listenHost string, gatewayPort, ttydPort int, wmsBinaryPath string) error {
	// A tmux session outlives ttyd (tmux has its own background server), so without
	// this a gateway restart — every deploy — would silently keep running whatever
	// binary was already inside the persisted session instead of picking up the new
	// one. Killing any stale session here means "the gateway (re)started" always gets
	// a clean session on the current binary, while still persisting BETWEEN restarts,
	// which is the actual point of webCommand's tmux wrapping.
	_ = exec.Command("tmux", "kill-session", "-t", webSessionName).Run()
	command := webCommand(wmsBinaryPath)
	ttydCmd := exec.CommandContext(ctx, "ttyd",
		append([]string{"-W", "-i", "lo", "-p", fmt.Sprintf("%d", ttydPort), // loopback only: reached through the proxy below
			"-t", "fontSize=18", "-t", "disableLeaveAlert=true"}, command...)...)
	// WMS_GATEWAY_SESSION marks this as a network-reachable session so the
	// TUI can require 2FA regardless of the account's local opt-in setting
	// (see internal/uiapp login.go) — mirrors internal/gateway/telnet.go's
	// childEnv() for the same reason. WMS_TOUCH_MODE enables mouse-tap
	// support (internal/uiapp App.handleMouse) — set only here, not in
	// telnet.go's childEnv(), because ttyd's xterm.js frontend is the one
	// touch-tablet path (see the APK/PWA served alongside this proxy);
	// raw telnet clients never get mouse escape codes sent to them at all.
	ttydCmd.Env = append(os.Environ(), "WMS_GATEWAY_SESSION=1", "WMS_TOUCH_MODE=1")

	target, err := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", ttydPort))
	if err != nil {
		return err
	}
	proxy := httputil.NewSingleHostReverseProxy(target)

	mux := http.NewServeMux()
	servePWAAssets(mux)
	dl := downloadHandler(exports.Dir, auditDownload)
	mux.Handle("/dl/", dl)
	mux.Handle("/DL/", dl) // QR codes carry the link upper case (see exports.URL)
	share := shareHandler(exports.Dir, auditShare)
	mux.Handle("/share/", share)
	mux.Handle("/SHARE/", share)
	mux.Handle("/", proxy)

	srv := &http.Server{
		Addr:              net.JoinHostPort(listenHost, strconv.Itoa(gatewayPort)),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 2)
	go func() { errCh <- ttydCmd.Run() }()
	go func() { errCh <- srv.ListenAndServe() }()

	select {
	case <-ctx.Done():
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			_ = srv.Close()
			if ttydCmd.Process != nil {
				_ = ttydCmd.Process.Kill()
			}
			return err
		}
	}

	_ = srv.Close()
	if ttydCmd.Process != nil {
		_ = ttydCmd.Process.Kill()
	}
	return nil
}
