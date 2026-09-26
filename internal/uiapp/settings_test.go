package uiapp

import (
	"path/filepath"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/auth"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/wmsdb"
)

// TestSettingsHubDeniesNonAdmin guards the stricter admin-only gate: a
// session with CanWrite but not IsAdmin (e.g. an ordinary Picker who can
// write stock quantities) must still be bounced out of Settings.
func TestSettingsHubDeniesNonAdmin(t *testing.T) {
	app := newTestApp(t)
	app.session = &auth.Session{
		Source: "modernwms", Username: "picker1", Role: "Picker",
		Permissions: &wmsdb.Permissions{IsAdmin: false, CanWrite: true, Menus: []string{"stockManagement"}},
	}
	app.cur = scrHub
	app.goTo(scrSettingsHub)

	if app.cur == scrSettingsHub {
		t.Fatal("non-admin session reached the Settings screen")
	}
	if app.message == "" || !app.messageErr {
		t.Fatalf("expected an access-denied error message, got %q (err=%v)", app.message, app.messageErr)
	}
}

// TestMenuClickMapsToOption is the runnable check for the touch-mode row
// math shared between ui.MenuOptionBodyRow and menuScreen.HandleClick: a
// click on option i's rendered row must trigger the same navigation a
// keypress on that option's Key would.
func TestMenuClickMapsToOption(t *testing.T) {
	app := newTestApp(t)
	app.touchMode = true
	app.cur = scrHub

	opts := hubOptions(app)
	if len(opts) < 2 {
		t.Fatalf("expected at least 2 hub options for an admin session, got %d", len(opts))
	}
	target := opts[1] // Operations, in a default admin session

	// What clicking option 1 should land on — found the same way a keypress on its
	// own Key would (a fresh app, so the click below can't influence this), not just
	// "navigated somewhere": a one-row mapping error could still hit a different,
	// nearby option and this must catch that.
	want := newTestApp(t)
	want.cur = scrHub
	target.Go(want)

	scr := app.screens[scrHub].(clickable)
	_ = app.View() // a real tap always follows a render — this is what sets menuScreen.wrapped
	scr.HandleClick(app, menuOptionBodyRowForTest(app.theme.Classic, 1))

	if app.cur == scrHub {
		t.Fatalf("click on option %q did not navigate away from the hub", target.Label)
	}
	if app.cur != want.cur {
		t.Fatalf("click on option %q landed on %q, want %q (the row math is off by a screen)", target.Label, app.cur, want.cur)
	}
}

// menuOptionBodyRowForTest recomputes ui.MenuOptionBodyRow's formulas
// independently (classic: caption row then tight rows; legacy: blank line
// between options) so this test actually cross-checks the two rather than
// asserting a tautology against the same function HandleClick calls. +1
// either way for the rounded panel the hub screen wraps in (it's small
// enough to always fit one — see menuScreen.Body/wrapped in screen.go; a
// menu too tall for the terminal falls back to no panel, and no +1).
func menuOptionBodyRowForTest(classic bool, i int) int {
	const panelTopBorder = 1
	if classic {
		return panelTopBorder + 1 + i
	}
	return panelTopBorder + 2 + 2*i
}

// TestRebrickableSettingsRoundTrip drives the actual Settings screen submit
// handler (not config.SetOverride directly, which config_test.go already
// covers) to confirm the screen wires into it correctly and that a
// subsequent lego.Client picks up the change without restarting the process.
func TestRebrickableSettingsRoundTrip(t *testing.T) {
	t.Setenv(config.SettingsFile, filepath.Join(t.TempDir(), "settings.json"))
	app := newTestApp(t)
	app.cur = scrHub
	app.goTo(scrSettingsRebrickable)

	scr := app.screens[scrSettingsRebrickable]
	form := scr.ActiveForm()
	if form == nil {
		t.Fatal("expected an active form after entering the Rebrickable settings screen")
	}
	form.Fields[1].Value = "newkey123"
	scr.(*formScreen).submit(app, form.Values())

	if got := config.Get(config.RebrickableAPIKey); got != "newkey123" {
		t.Fatalf("config.Get after settings submit: got %q, want %q", got, "newkey123")
	}
	if !app.rebrick.Enabled() {
		t.Fatal("app.rebrick was not refreshed with the new key")
	}
}
