package uiapp

import (
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/auth"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/wmsdb"
)

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
