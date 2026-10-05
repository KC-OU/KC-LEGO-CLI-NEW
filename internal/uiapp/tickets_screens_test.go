package uiapp

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/auth"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/wmsdb"
)

// TestReopenKeyOpensTheReasonScreenForAnAdmin is the direct test for
// "Admin: reopen a wrong check/order" 's TUI entry point: R on a Completion
// dashboard row goes straight to the reason prompt (no "which one?" step —
// the target's already under the cursor), and submitting creates a fresh,
// open ticket via lego.DB.ReopenTicket.
func TestReopenKeyOpensTheReasonScreenForAnAdmin(t *testing.T) {
	app := newTestApp(t)
	app.cur, app.stack = scrCompletion, nil

	completionKeys(app, "75192-1", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})

	if app.cur != scrReopenReason {
		t.Fatalf("R should open the reopen-reason screen, got %q (%s)", app.cur, app.message)
	}
	if app.assign == nil || app.assign.kind != lego.TicketCheck || app.assign.target != "75192-1" {
		t.Fatalf("assign draft = %+v, want a check ticket targeting 75192-1", app.assign)
	}

	app.screens[scrReopenReason].(*formScreen).submit(app, []string{"", "miscounted the dark grey plates"})

	open, err := app.legoDB.OpenTickets(lego.TicketCheck, "anyone")
	if err != nil || len(open) != 1 || open[0].Target != "75192-1" || open[0].AssignedTo != "" {
		t.Fatalf("open tickets after reopening = %+v, %v, want one open ticket for 75192-1", open, err)
	}
}

// TestReopenKeyDeniesANonAdmin confirms the gate actually gates: a governed
// checker-only account pressing R must not reach the reason screen or
// create a ticket.
func TestReopenKeyDeniesANonAdmin(t *testing.T) {
	app := newTestApp(t)
	dualSkilledSignOn(t, app, "dave")
	app.cur, app.stack = scrCompletion, nil

	completionKeys(app, "75192-1", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})

	if app.cur == scrReopenReason {
		t.Fatal("a non-admin pressing R must not reach the reopen-reason screen")
	}
	if open, _ := app.legoDB.OpenTickets(lego.TicketCheck, "anyone"); len(open) != 0 {
		t.Errorf("a denied reopen must not create a ticket, got %+v", open)
	}
}

func TestToggleAdminViewRoundTrip(t *testing.T) {
	app := newTestApp(t)
	app.cur, app.stack = scrPickerHub, nil
	app.toggleAdminView()
	if app.cur != scrAdminHub {
		t.Fatalf("V, already admin-level, should jump straight to Admin, got %q", app.cur)
	}
	app.toggleAdminView()
	if app.cur != scrPickerHub {
		t.Fatalf("V again should jump back to the picker/checker hub, got %q", app.cur)
	}
}

func TestToggleAdminViewNoSessionIsANoop(t *testing.T) {
	app := newTestApp(t)
	app.cur, app.stack = scrPickerHub, nil
	app.session = nil
	app.toggleAdminView()
	if app.cur != scrPickerHub {
		t.Errorf("with no session, V must do nothing, got %q", app.cur)
	}
}

// TestToggleAdminViewAsksForCredentialsWhenNotAdmin is the actual "quick
// switch" case: a governed session with only checker-level access must be
// asked for a separate admin identity, not walked straight into Admin.
func TestToggleAdminViewAsksForCredentialsWhenNotAdmin(t *testing.T) {
	app := newTestApp(t)
	setPolicy(t, func(p *access.Policy) {
		p.Users["partdb:checker3"] = &access.User{Groups: []string{"checker"}, TwoFA: access.TwoFAExempt}
	})
	signOn(app, "checker3", "")
	app.cur, app.stack = scrPickerHub, nil

	app.toggleAdminView()
	if app.cur != scrSwitchAdmin {
		t.Fatalf("a checker-only account pressing V should be asked for admin credentials, got %q", app.cur)
	}
	if app.parked != nil {
		t.Error("nothing should be parked until a switch actually succeeds")
	}
}

// TestRestoreParkedSessionReturnsExactIdentity exercises the return half of a
// switch directly (the credential-entry form itself isn't unit-tested here —
// see switch_admin.go): once a.parked is set, V must restore precisely what
// was parked and never ask for anything.
func TestRestoreParkedSessionReturnsExactIdentity(t *testing.T) {
	app := newTestApp(t)
	original := &auth.Session{Source: "partdb", Username: "checker3", Role: "PartDB User", Permissions: &wmsdb.Permissions{CanWrite: true}}
	app.parked = &parkedSession{session: original, cur: scrPickerHub, activeTab: "1"}
	app.session = &auth.Session{Source: "modernwms", Username: "admin", Role: "Admin", Permissions: &wmsdb.Permissions{IsAdmin: true}}
	app.stack = []string{scrAdminHub, scrJobQueue}
	app.cur = scrAssignPick

	app.toggleAdminView()

	if app.parked != nil {
		t.Error("restoring must clear the parked identity")
	}
	if app.session != original {
		t.Errorf("session after restore = %+v, want the parked original", app.session)
	}
	if app.cur != scrPickerHub {
		t.Errorf("cur after restore = %q, want the parked screen", app.cur)
	}
}
