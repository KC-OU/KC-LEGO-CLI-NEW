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

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/auth"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/botapi"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/exports"
)

// webSessionName is the tmux session every default web-terminal connection shares.
const webSessionName = "wms-web"

// soloBasePath is where the independent (non-shared) web terminal lives — see
// RunWebGateway and soloCommand.
const soloBasePath = "/solo"

// testBasePath is the dev-build "Test" web terminal — see RunWebGateway's
// testBinaryPath/testEnv params and WMS_TEST_BINARY_PATH/WMS_TEST_ENV_FILE
// (docs/guides/telnet-and-web.md). Only ever started when both are set.
const testBasePath = "/test"

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
//
// webSessionLockdown chains two more commands onto every webCommand invocation,
// scoped to this one session (`-t webSessionName`, never `-g`) — a security
// boundary, not a feature. Without it, anyone attached to the shared session
// (just by opening the web terminal in a browser) can press tmux's own prefix
// key (Ctrl-B by default) and issue a tmux command directly: "c" opens a
// brand-new window running whatever shell this process itself runs under —
// root, per the gateway's systemd unit — a straight escape from the wrapped
// `wms tui` app to a root shell, with none of the app's own permission
// checks ever in the path. tmux handles its own key bindings before the
// wrapped program ever sees the keystroke, so this can't be fixed inside
// uiapp; it has to be fixed at the multiplexer.
//
// This is deliberately NOT a `tmux -f <config>` flag: that only takes effect
// when tmux is starting a brand-new server process — if any other tmux
// session already exists on the box for an unrelated reason (an admin's own
// interactive session, say), the gateway's own client is attaching to that
// SAME already-running server, and -f is silently ignored, leaving the
// prefix key live. A `-t`-scoped set-option, chained onto the same
// invocation and run as its own command after the session is created or
// attached, works regardless of server history and touches only this one
// session — never any other session already on that server. "None" is
// tmux's own special value for "no key bound to this"; with no prefix key
// and tmux's own mouse handling (which has its own bound actions) also off,
// there is no way into tmux's command layer at all — every keystroke and
// click goes straight through to `wms tui`, exactly how /solo and telnet
// (which never wrap in tmux at all) already behave.
var webSessionLockdown = []string{
	";", "set-option", "-t", webSessionName, "prefix", "None",
	";", "set-option", "-t", webSessionName, "mouse", "off",
}

func webCommand(wmsBinaryPath string) []string {
	if _, err := exec.LookPath("tmux"); err != nil {
		return []string{wmsBinaryPath, "tui"} // no tmux: today's behaviour, one process per connection
	}
	return append([]string{"tmux", "new-session", "-A", "-s", webSessionName, wmsBinaryPath, "tui"}, webSessionLockdown...)
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
// soloBasePath — behind one Go-native reverse proxy, plus two optional extra ttyd
// instances (the dev-build "Test" terminal, and a base-path-free clone of /solo —
// see soloDirectPort), and runs them until ctx is done. httputil.ReverseProxy
// (rather than the original's hand-rolled byte-pipe) forwards WebSocket upgrade
// requests correctly for a same-process HTTP/1.1 target since it streams the
// hijacked connection rather than buffering it. Every process started here is
// torn down together, mirroring start_webtui.sh's kill-all-on-any-exit.
func RunWebGateway(ctx context.Context, listenHost string, gatewayPort, ttydPort int, wmsBinaryPath, testBinaryPath string, testEnv []string, soloDirectPort string, botSrv *botapi.Server, discordPublicKey string, authWMS auth.WMSAuthenticator, authPDB auth.PartDBAuthenticator) error {
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
	mux.Handle("/exports", exportsHandler(exports.Dir, authWMS, authPDB, auditExports))
	// Discord's Interactions Endpoint URL can point at this path on any
	// public hostname already proxying here (e.g. https://tui.example.com
	// /discord/interactions) — no separate tunnel hostname or port needed.
	// Off entirely (same "" = off convention as every other optional port in
	// this project) until WMS_DISCORD_PUBLIC_KEY is set — see
	// docs/guides/remote-bot-discord.md.
	if botSrv != nil && discordPublicKey != "" {
		mux.Handle("/discord/interactions", botapi.InteractionsHandler(botSrv, discordPublicKey))
	}
	mux.Handle(soloBasePath+"/", soloProxy) // more specific than "/", ServeMux prefers it

	// The dev-build "Test" terminal: a third ttyd, same independent-process
	// shape as /solo, exec'ing testBinaryPath with the test instance's own
	// env layered on top — only started when an admin has configured both
	// (see cmd/wms/gateway.go). Left out entirely otherwise: no third
	// process, no third path, today's exact behaviour.
	var testCmd *exec.Cmd
	if testBinaryPath != "" {
		testPort := ttydPort + 2
		testCmd = exec.CommandContext(ctx, "ttyd",
			append([]string{"-W", "-i", "lo", "-p", fmt.Sprintf("%d", testPort), "-b", testBasePath,
				"-t", "fontSize=18", "-t", "disableLeaveAlert=true"}, soloCommand(testBinaryPath)...)...)
		testCmd.Env = mergeEnv(env, testEnv)
		testTarget, err := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", testPort))
		if err != nil {
			return err
		}
		mux.Handle(testBasePath+"/", httputil.NewSingleHostReverseProxy(testTarget))
	}

	mux.Handle("/", proxy)

	// /solo reached via its own path (above) needs the gateway's own proxy
	// to forward that exact path, which only works because the backend
	// itself was started with -b soloBasePath. A reverse proxy or tunnel
	// that can't rewrite the request path before forwarding (some can't)
	// has no way to make a bare "/" request reach that path-scoped backend
	// — so when configured, this starts a second, otherwise-identical solo
	// backend on its own port with no base path, reachable at its own root.
	// Independent of the proxied /solo above; nothing is shared between
	// them beyond both exec'ing the same wmsBinaryPath per connection.
	var soloDirectCmd *exec.Cmd
	if soloDirectPort != "" {
		soloDirectCmd = exec.CommandContext(ctx, "ttyd",
			append([]string{"-W", "-i", "lo", "-p", soloDirectPort,
				"-t", "fontSize=18", "-t", "disableLeaveAlert=true"}, soloCommand(wmsBinaryPath)...)...)
		soloDirectCmd.Env = env
	}

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
		if testCmd != nil && testCmd.Process != nil {
			_ = testCmd.Process.Kill()
		}
		if soloDirectCmd != nil && soloDirectCmd.Process != nil {
			_ = soloDirectCmd.Process.Kill()
		}
	}

	errCh := make(chan error, 5)
	go func() { errCh <- ttydCmd.Run() }()
	go func() { errCh <- soloCmd.Run() }()
	if testCmd != nil {
		go func() { errCh <- testCmd.Run() }()
	}
	if soloDirectCmd != nil {
		go func() { errCh <- soloDirectCmd.Run() }()
	}
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
