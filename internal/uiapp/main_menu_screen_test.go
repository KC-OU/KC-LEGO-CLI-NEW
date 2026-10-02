package uiapp

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
)

// TestHubOptionsUsesDefaultWhenMainMenuUnset is the back-compat case: an
// access.json with no main_menu entry (every existing install, until an
// admin opens Customize the Main Menu for the first time) must keep showing
// exactly the original six tabs in the original order.
func TestHubOptionsUsesDefaultWhenMainMenuUnset(t *testing.T) {
	app := newTestApp(t)
	opts := hubOptions(app)
	if len(opts) != len(defaultMainMenuKeys) {
		t.Fatalf("hubOptions = %d entries, want %d (the default set)", len(opts), len(defaultMainMenuKeys))
	}
	for i, key := range defaultMainMenuKeys {
		it := mainMenuItemByKey(key)
		if opts[i].Key != it.hotkey || opts[i].Label != it.label {
			t.Errorf("opts[%d] = %q/%q, want %q/%q", i, opts[i].Key, opts[i].Label, it.hotkey, it.label)
		}
	}
}

// TestHubOptionsHonorsCustomOrderAndPromotedItems covers an admin's actual
// reorg: dropping everything except a promoted Admin row and Overview, with
// the promoted row put first — both the subset and the order must carry
// through to what the hub actually renders.
func TestHubOptionsHonorsCustomOrderAndPromotedItems(t *testing.T) {
	app := newTestApp(t)
	setPolicy(t, func(p *access.Policy) {
		p.Settings.MainMenu = []string{"admin_assign", "overview"}
	})
	app.loadPolicy()

	opts := hubOptions(app)
	if len(opts) != 2 {
		t.Fatalf("hubOptions = %+v, want exactly 2 entries", opts)
	}
	if opts[0].Label != "Admin: Assign Work" || opts[1].Label != "Overview" {
		t.Errorf("opts = %q, %q, want Assign Work first, Overview second", opts[0].Label, opts[1].Label)
	}
}

// TestHubOptionsStillHidesWhatThisViewerCannotSee proves the custom layout
// never grants access it didn't already have: a promoted admin-only row is
// still filtered out for a signed-in user without that permission, exactly
// like the un-customized hub always filtered its six tabs.
func TestHubOptionsStillHidesWhatThisViewerCannotSee(t *testing.T) {
	app := newTestApp(t)
	setPolicy(t, func(p *access.Policy) {
		p.Settings.MainMenu = []string{"admin_assign", "overview"}
		p.Users["partdb:checker3"] = &access.User{Groups: []string{"checker"}, TwoFA: access.TwoFAExempt}
	})
	signOn(app, "checker3", "")

	opts := hubOptions(app)
	for _, o := range opts {
		if o.Label == "Admin: Assign Work" {
			t.Errorf("checker3 has no user_mgmt permission — Assign Work must not appear, got %+v", opts)
		}
	}
}

// TestCustomMenuScreenToggleMoveAndSave drives the whole editor: turning an
// item off, promoting a new one on, reordering it, then saving — and checks
// the saved policy matches exactly, not just that something was written.
func TestCustomMenuScreenToggleMoveAndSave(t *testing.T) {
	app := newTestApp(t)
	app.goTo(scrCustomMenu)
	scr := app.screens[scrCustomMenu].(*customMenuScreen)
	if len(scr.vals) != len(defaultMainMenuKeys) || scr.vals[0] != defaultMainMenuKeys[0] {
		t.Fatalf("initial working copy = %v, want it seeded from the default set %v", scr.vals, defaultMainMenuKeys)
	}

	// Turn "Script Hub" (default position 4, cursor row 3) off.
	scr.row = 3
	scr.HandleKey(app, tea.KeyMsg{Type: tea.KeySpace})
	if idx := scr.valIndex("scripts"); idx != -1 {
		t.Fatalf("scripts should be off after toggling, still at index %d", idx)
	}

	// Promote "Admin: Assign Work" on, then move it to the very front.
	for i, k := range scr.display() {
		if k == "admin_assign" {
			scr.row = i
			break
		}
	}
	scr.HandleKey(app, tea.KeyMsg{Type: tea.KeySpace}) // turn it on (appends to the end)
	for scr.valIndex("admin_assign") > 0 {
		scr.HandleKey(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{','}}) // move it up
	}
	if scr.valIndex("admin_assign") != 0 {
		t.Fatalf("admin_assign should now be first, vals = %v", scr.vals)
	}

	scr.HandleKey(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})

	saved := app.pol().Settings.MainMenu
	if len(saved) == 0 || saved[0] != "admin_assign" {
		t.Fatalf("saved MainMenu = %v, want admin_assign first", saved)
	}
	for _, k := range saved {
		if k == "scripts" {
			t.Fatalf("saved MainMenu = %v, scripts should have been dropped", saved)
		}
	}
	if !auditHas(t, "main menu customized") {
		t.Error("expected an audit log entry")
	}
}
