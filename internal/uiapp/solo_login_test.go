package uiapp

import (
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/auth"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

// TestDoLoginSubmitUsesLocalAuthInSoloMode is the direct test for the "solo
// mode" login branch (see internal/config.SoloMode / docs/guides/solo-mode.md):
// a personal copy with no access to the ModernWMS/Part-DB containers still
// needs to sign in, via a password stored in access.json rather than either
// backend.
func TestDoLoginSubmitUsesLocalAuthInSoloMode(t *testing.T) {
	app := newTestApp(t)
	t.Setenv(config.SoloMode, "1")

	hash, err := auth.HashPartDB("my-solo-password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := access.Update(func(p *access.Policy) error {
		p.Users[access.Key("local", "kc")] = &access.User{PasswordHash: hash, Groups: []string{"admin"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	app.authed, app.session, app.cur = false, nil, scrLogin
	doLoginSubmit(app, []string{"kc", "my-solo-password"})

	if app.messageErr {
		t.Fatalf("expected a successful local sign-in, got error: %q", app.message)
	}
	if app.session == nil || app.session.Source != "local" {
		t.Fatalf("session = %+v, want Source=local", app.session)
	}
	if mustChangePassword(app, app.session) {
		t.Error("a local session should never be forced to change password")
	}
}

// TestDoLoginSubmitSoloModeRejectsWrongPassword confirms the branch still
// fails closed — solo mode isn't a bypass of authentication, just of which
// backend checks it.
func TestDoLoginSubmitSoloModeRejectsWrongPassword(t *testing.T) {
	app := newTestApp(t)
	t.Setenv(config.SoloMode, "1")

	hash, _ := auth.HashPartDB("correct-password")
	if _, err := access.Update(func(p *access.Policy) error {
		p.Users[access.Key("local", "kc")] = &access.User{PasswordHash: hash}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	app.authed, app.session, app.cur = false, nil, scrLogin
	doLoginSubmit(app, []string{"kc", "wrong"})

	if !app.messageErr {
		t.Fatal("expected the wrong password to be rejected")
	}
	if app.session != nil {
		t.Errorf("session should stay nil on a failed local sign-in, got %+v", app.session)
	}
}
