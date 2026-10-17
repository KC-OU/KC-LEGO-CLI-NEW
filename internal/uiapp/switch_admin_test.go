package uiapp

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/auth"
)

// An account with NO admin-level module access goes through the credentialed
// switch instead (switchAdminScreen), which parks the original identity.
// "0. Return" from the admin hub must hand that identity straight back --
// not just navigate off to some other screen while still authenticated as
// the elevated admin account.
func TestReturnFromAdminHubRestoresTheParkedIdentity(t *testing.T) {
	app := newTestApp(t)
	original := &auth.Session{Source: "modernwms", Username: "kcollins", Role: "stock-checker"}
	app.parked = &parkedSession{session: original, cur: scrPickerHub, stack: nil, activeTab: "1"}
	app.session = &auth.Session{Source: "modernwms", Username: "an-admin", Role: "admin"}
	app.authed = true
	app.cur = scrAdminHub
	app.stack = nil

	pressAndDrain(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'0'}})

	if app.parked != nil {
		t.Fatal("Return should have cleared the parked session")
	}
	if app.session != original {
		t.Fatalf("Return should restore the original (parked) session, got %+v", app.session)
	}
	if app.cur != scrPickerHub {
		t.Fatalf("Return should land back on the parked screen (scrPickerHub), got %q", app.cur)
	}
}

// A checker/picker whose group also happens to grant one admin-level module
// (settings.view, say) never sees the credentials prompt: toggleAdminView's
// "already admin-level, no identity switch needed" branch jumps straight
// in. Returning from there must land back on the Picker/Checker hub, not
// fall through to the Main hub this account may never otherwise see.
func TestVSwitchToAdminThenReturnGoesBackToThePickerHubNotTheMainHub(t *testing.T) {
	app := newTestApp(t)
	setPolicy(t, func(p *access.Policy) {
		p.Groups["checker"] = &access.Group{Perms: map[string]string{"settings.view": "allow"}, Restricted: true}
		p.Users[access.Key("modernwms", "kcollins")] = &access.User{Groups: []string{"checker"}}
	})
	app.session = &auth.Session{Source: "modernwms", Username: "kcollins", Role: "stock-checker"}
	app.authed = true
	app.loadPolicy()
	app.cur = scrPickerHub
	app.stack = nil

	pressAndDrain(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	if app.cur != scrAdminHub {
		t.Fatalf("V should jump straight to the admin hub for an account with admin-level module access, got %q", app.cur)
	}

	// adminHubScreen's own "0. Return" row -- app.onBack() -> back() -- not V again.
	pressAndDrain(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'0'}})
	if app.cur != scrPickerHub {
		t.Fatalf("Return from the admin hub landed on %q, want scrPickerHub", app.cur)
	}
}
