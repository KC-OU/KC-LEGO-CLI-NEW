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

// RunWebGateway starts ttyd (already installed on this box) wrapping
// `<wmsBinaryPath> tui`, and a Go-native reverse proxy in front of it —
// replacing the original's hand-rolled byte-pipe with
// httputil.ReverseProxy, which forwards WebSocket upgrade requests
// correctly for a same-process HTTP/1.1 target since it streams the
// hijacked connection rather than buffering it. Both processes are torn
// down together, mirroring start_webtui.sh's kill-all-on-any-exit.
func RunWebGateway(ctx context.Context, listenHost string, gatewayPort, ttydPort int, wmsBinaryPath string) error {
	ttydCmd := exec.CommandContext(ctx, "ttyd",
		"-W", "-i", "lo", "-p", fmt.Sprintf("%d", ttydPort), // loopback only: reached through the proxy below
		"-t", "fontSize=18", "-t", "disableLeaveAlert=true",
		wmsBinaryPath, "tui")
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
