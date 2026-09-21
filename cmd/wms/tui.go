package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/audit"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/gateway"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/partdb"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/uiapp"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/wmsdb"
)

// runTUI launches the interactive 5250-style terminal. internal/gateway's
// telnet/ttyd wiring execs `<wms binary> tui` as its child process, so this
// must run cleanly as a PTY child, not just in a local terminal.
func runTUI(cmd *cobra.Command, args []string) error {
	db, err := partdb.Open("")
	if err != nil {
		return fmt.Errorf("opening Part-DB: %w", err)
	}
	legoDB, err := openLego()
	if err != nil {
		return fmt.Errorf("opening LEGO collection store: %w", err)
	}
	requireTwoFA := os.Getenv("WMS_GATEWAY_SESSION") == "1"
	touchMode := os.Getenv("WMS_TOUCH_MODE") == "1"
	app := uiapp.NewApp(wmsdb.NewClient(), db, legoDB, audit.New(), requireTwoFA, touchMode)
	app.SetGraceOrigin(graceOrigin(requireTwoFA, os.Getenv("WMS_REMOTE_ADDR")))
	if requireTwoFA {
		app.SetTransport(os.Getenv("WMS_TRANSPORT"), os.Getenv("WMS_REMOTE_ADDR"))
	}

	opts := []tea.ProgramOption{tea.WithAltScreen()}
	if touchMode {
		// Only the ttyd/web gateway path sets WMS_TOUCH_MODE (see
		// internal/gateway/web.go) — a raw telnet client (internal/gateway/
		// telnet.go) never gets mouse-mode escape codes sent to it, so this
		// can't affect the plain 5250-style path at all.
		opts = append(opts, tea.WithMouseCellMotion())
	}
	_, err = tea.NewProgram(app, opts...).Run()
	if err == nil && requireTwoFA && app.LockedOut() {
		// Tells the telnet gateway this session burned its sign-in attempts.
		os.Exit(gateway.ExitTooManyFailures)
	}
	return err
}

func newTUICmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Launch the interactive 5250-style terminal (default when no subcommand is given)",
		RunE:  runTUI,
	}
}

// graceOrigin is what a 2FA grace window is pinned to: "local" for a session
// started straight from a shell on this box, "telnet:<address>" when the
// telnet gateway passed the client's address, and "" — never trusted — for
// anything else, notably the web terminal, where ttyd hides the client's
// address. WMS_REMOTE_ADDR is only believed on gateway sessions, so setting
// it in an ordinary shell can't fake a remote origin.
func graceOrigin(gatewaySession bool, remoteAddr string) string {
	switch {
	case !gatewaySession:
		return "local"
	case remoteAddr != "":
		return "telnet:" + remoteAddr
	}
	return ""
}
