package uiapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/auth"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/tools"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/wmsdb"
)

func hubApp(t *testing.T) (*App, *toolsScreen) {
	t.Helper()
	app := newTestApp(t)
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	app.cur, app.stack = scrHub, nil
	app.goTo(scrScripts)
	return app, app.screens[scrScripts].(*toolsScreen)
}

func selectTool(t *testing.T, s *toolsScreen, id string) {
	t.Helper()
	for i, r := range s.rows {
		if r.ID == id {
			s.cursor = i
			return
		}
	}
	t.Fatalf("%s is not in the Script Hub", id)
}

func TestScriptHubOffersOnlyWhatMayRunInsideATelnetSession(t *testing.T) {
	_, s := hubApp(t)
	ids := map[string]bool{}
	for _, r := range s.rows {
		ids[r.ID] = true
	}
	for _, want := range []string{"status", "doctor", "lego-stats", "backup-lego"} {
		if !ids[want] {
			t.Errorf("%s should be offered", want)
		}
	}
	for _, banned := range []string{"deploy", "publish", "restart-gateway", "restart-all", "logs-wms", "audit-follow", "user-reset", "receive", "preflight"} {
		if ids[banned] {
			t.Errorf("%s must not be runnable from a remote session", banned)
		}
	}
}

func TestScriptHubRunsAToolAuditsItAndHandsOverTheTerminal(t *testing.T) {
	app, s := hubApp(t)
	selectTool(t, s, "status")
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if app.pendingCmd != nil {
		t.Fatal("pendingCmd is handed to bubbletea and cleared by Update")
	}
	// Update returned the command; run the screen path again to inspect the hand-off itself
	s.run(app, *s.selected())
	if app.pendingCmd == nil {
		t.Fatal("running a tool must ask bubbletea to run a process")
	}
	app.pendingCmd = nil
	log := auditText(t, app)
	if !strings.Contains(log, "TOOL_RUN") || !strings.Contains(log, "tool=status via=script-hub") || !strings.Contains(log, "STARTED") {
		t.Errorf("the start is audited:\n%s", log)
	}
	app.Update(toolDoneMsg{ID: "status"})
	if !strings.Contains(app.message, "Finished: status") || !strings.Contains(auditText(t, app), "SUCCESS") {
		t.Errorf("completion is audited and shown: %q", app.message)
	}
	app.Update(toolDoneMsg{ID: "status", Err: os.ErrNotExist})
	if !app.messageErr || !strings.Contains(auditText(t, app), "FAILED") {
		t.Error("a failure is audited and shown as an error")
	}
}

func TestScriptHubAsksBeforeRiskyToolsAndNeedsAnAdminForAdminTools(t *testing.T) {
	app, s := hubApp(t)
	selectTool(t, s, "backup") // risky and admin
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if s.confirm == nil || !strings.Contains(app.message, "Press Y") {
		t.Fatalf("a risky tool asks first: %q", app.message)
	}
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if s.confirm != nil || app.pendingCmd != nil || !strings.Contains(app.message, "Cancelled") {
		t.Fatal("anything but Y cancels")
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if cmd == nil {
		t.Fatal("Y runs it")
	}

	app.session.Permissions = &wmsdb.Permissions{CanWrite: true, Menus: []string{"*"}} // no longer an administrator
	app.session.Role = "Picker"
	selectTool(t, s, "backup")
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if s.confirm != nil || !strings.Contains(app.message, "admin role required") {
		t.Errorf("an admin-only tool is refused before it asks: %q", app.message)
	}
	if log := auditText(t, app); !strings.Contains(log, "DENIED_ADMIN_RESTRICTION") {
		t.Errorf("the refusal is audited:\n%s", log)
	}
	_ = auth.Session{}
}

func TestScriptHubFilterAndPin(t *testing.T) {
	app, s := hubApp(t)
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if !s.capturing() {
		t.Fatal("/ starts the filter")
	}
	for _, r := range "lego" {
		app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if len(s.rows) == 0 || len(s.rows) >= len(s.all) {
		t.Fatalf("the filter narrows the list: %d of %d", len(s.rows), len(s.all))
	}
	for _, r := range s.rows {
		if !strings.Contains(strings.ToLower(r.ID+r.Group+r.Title+r.Hint), "lego") {
			t.Errorf("%s does not match", r.ID)
		}
	}
	first := s.rows[len(s.rows)-1].ID
	s.cursor = len(s.rows) - 1
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	if !s.state.IsPinned(first) {
		t.Error("Ctrl-P pins")
	}
	if back := tools.LoadState(tools.StatePath()); !back.IsPinned(first) {
		t.Error("pins are saved where the shell launcher reads them")
	}
}

func TestScriptHubReadsTheUsersOwnToolsAndIgnoresABadFile(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "tools.json")
	os.WriteFile(good, []byte(`[{"id":"hello","title":"Say hello","argv":["echo","hi"],"where":"both"},{"id":"shellonly","title":"Shell only","argv":["echo"]}]`), 0o600)
	t.Setenv("WMS_TOOLS_FILE", good)
	_, s := hubApp(t)
	var hello, shellOnly bool
	for _, r := range s.all {
		hello = hello || r.ID == "hello"
		shellOnly = shellOnly || r.ID == "shellonly"
	}
	if !hello || shellOnly {
		t.Errorf("your tool is offered only where you said: hello=%v shellonly=%v", hello, shellOnly)
	}

	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, []byte(`[{"id":"BAD ID"}]`), 0o600)
	t.Setenv("WMS_TOOLS_FILE", bad)
	app, s := hubApp(t)
	if len(s.all) < 10 || !strings.Contains(plain(app.View()), "tools.json ignored") {
		t.Errorf("a bad file is reported and the built-in tools still work (%d)", len(s.all))
	}
}

func TestTheWrapperKeepsOutputOnScreenUntilEnterAndReportsTheStatus(t *testing.T) {
	cmd, err := toolCommand(tools.Tool{ID: "x", Argv: []string{"sh", "-c", "echo hello-from-tool; exit 3"}})
	if err != nil {
		t.Fatal(err)
	}
	out, _ := cmd.Output() // no terminal: read sees EOF at once
	if !strings.Contains(string(out), "hello-from-tool") || !strings.Contains(string(out), "[exit 3] press Enter to return") {
		t.Errorf("%q", out)
	}
}
