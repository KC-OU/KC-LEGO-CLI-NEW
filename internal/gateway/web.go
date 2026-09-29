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

// webSessionName is the tmux session every default web-terminal connection shares.
const webSessionName = "wms-web"

// soloBasePath is where the independent (non-shared) web terminal lives — see
// RunWebGateway and soloCommand.
const soloBasePath = "/solo"

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

// soloCommand is the independent web terminal's command: always a fresh process
// per connection, the same as a telnet connection or a no-tmux webCommand — no
// tmux wrapping, so nothing here is shared with the default (`/`) web terminal or
// with any other /solo connection. It trades away webCommand's seamless-reconnect
// behaviour (a dropped connection here is really gone: reopening it is a brand
// new sign-on, 2FA included) for what it's for: a second, genuinely independent
// person — or the same person deliberately testing as a second account — able to
// use the web terminal at the same time as whoever is on the shared one, without
// either sharing the other's screen or waiting on it.
func soloCommand(wmsBinaryPath string) []string {
	return []string{wmsBinaryPath, "tui"}
}

// RunWebGateway starts two ttyd instances (already installed on this box) — the
// default shared-session terminal at "/", and a second, always-independent one at
// soloBasePath — behind one Go-native reverse proxy, and runs them until ctx is
// done. httputil.ReverseProxy (rather than the original's hand-rolled byte-pipe)
// forwards WebSocket upgrade requests correctly for a same-process HTTP/1.1
// target since it streams the hijacked connection rather than buffering it. All
// three processes (proxy, both ttyd instances) are torn down together, mirroring
// start_webtui.sh's kill-all-on-any-exit.
func RunWebGateway(ctx context.Context, listenHost string, gatewayPort, ttydPort int, wmsBinaryPath string) error {
	// A tmux session outlives ttyd (tmux has its own background server), so without
	// this a gateway restart — every deploy — would silently keep running whatever
	// binary was already inside the persisted session instead of picking up the new
	// one. Killing any stale session here means "the gateway (re)started" always gets
	// a clean session on the current binary, while still persisting BETWEEN restarts,
	// which is the actual point of webCommand's tmux wrapping. soloCommand has no
	// such persisted state to clean up — each of its connections already starts fresh.
	_ = exec.Command("tmux", "kill-session", "-t", webSessionName).Run()

	// WMS_GATEWAY_SESSION marks this as a network-reachable session so the TUI can
	// require 2FA regardless of the account's local opt-in setting (see internal/
	// uiapp login.go) — mirrors internal/gateway/telnet.go's childEnv() for the
	// same reason. WMS_TOUCH_MODE enables mouse-tap support (internal/uiapp
	// App.handleMouse) — set only here, not in telnet.go's childEnv(), because
	// ttyd's xterm.js frontend is the one touch-tablet path (see the APK/PWA
	// served alongside this proxy); raw telnet clients never get mouse escape
	// codes sent to them at all.
	env := append(os.Environ(), "WMS_GATEWAY_SESSION=1", "WMS_TOUCH_MODE=1")

	ttydCmd := exec.CommandContext(ctx, "ttyd",
		append([]string{"-W", "-i", "lo", "-p", fmt.Sprintf("%d", ttydPort), // loopback only: reached through the proxy below
			"-t", "fontSize=18", "-t", "disableLeaveAlert=true"}, webCommand(wmsBinaryPath)...)...)
	ttydCmd.Env = env

	soloPort := ttydPort + 1
	soloCmd := exec.CommandContext(ctx, "ttyd",
		append([]string{"-W", "-i", "lo", "-p", fmt.Sprintf("%d", soloPort), "-b", soloBasePath,
			"-t", "fontSize=18", "-t", "disableLeaveAlert=true"}, soloCommand(wmsBinaryPath)...)...)
	soloCmd.Env = env

	target, err := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", ttydPort))
	if err != nil {
		return err
	}
	soloTarget, err := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", soloPort))
	if err != nil {
		return err
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	soloProxy := httputil.NewSingleHostReverseProxy(soloTarget)

	mux := http.NewServeMux()
	servePWAAssets(mux)
	dl := downloadHandler(exports.Dir, auditDownload)
	mux.Handle("/dl/", dl)
	mux.Handle("/DL/", dl) // QR codes carry the link upper case (see exports.URL)
	share := shareHandler(exports.Dir, auditShare)
	mux.Handle("/share/", share)
	mux.Handle("/SHARE/", share)
	mux.Handle(soloBasePath+"/", soloProxy) // more specific than "/", ServeMux prefers it
	mux.Handle("/", proxy)

	srv := &http.Server{
		Addr:              net.JoinHostPort(listenHost, strconv.Itoa(gatewayPort)),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	kill := func() {
		if ttydCmd.Process != nil {
			_ = ttydCmd.Process.Kill()
		}
		if soloCmd.Process != nil {
			_ = soloCmd.Process.Kill()
		}
	}

	errCh := make(chan error, 3)
	go func() { errCh <- ttydCmd.Run() }()
	go func() { errCh <- soloCmd.Run() }()
	go func() { errCh <- srv.ListenAndServe() }()

	select {
	case <-ctx.Done():
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			_ = srv.Close()
			kill()
			return err
		}
	}

	_ = srv.Close()
	kill()
	return nil
}
