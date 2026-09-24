package uiapp

import (
	"os"
	"strings"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

// A barcode scanner is just a keyboard that types fast: scanning a badge into the
// Username field is exactly like typing a long random string there, so this drives
// doLoginSubmit the same way a real scan would — no scanner hardware needed.
//
// newTestApp's ModernWMS client points at a container that doesn't exist, and
// AuthenticateUser always asks it for role permissions even on the Part-DB path,
// so no sign-in can fully succeed in this harness (a pre-existing constraint,
// not one badge sign-in adds). Every test here is a deliberately failed attempt,
// checked through the audit log to prove the badge was resolved to the real
// username before authentication ran, not that authentication itself succeeded.
func TestBadgeResolvesToTheRealUsernameBeforeAuthenticating(t *testing.T) {
	app := newTestApp(t)
	if _, err := access.Update(func(p *access.Policy) error {
		p.Users[access.Key("partdb", "alex")] = &access.User{BadgeToken: "BADGE123XYZ"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	app.authed, app.session, app.cur = false, nil, scrLogin
	doLoginSubmit(app, []string{"BADGE123XYZ", "whatever-password"})
	if app.authed {
		t.Fatal("no such Part-DB/ModernWMS account exists in this scratch environment, so this must still fail")
	}
	b, err := os.ReadFile(config.Get(config.AuditLogFile))
	if err != nil {
		t.Fatal(err)
	}
	log := string(b)
	if !strings.Contains(log, "USER:alex ") {
		t.Errorf("the failed attempt should be logged against the resolved username 'alex':\n%s", log)
	}
	if strings.Contains(log, "BADGE123XYZ") {
		t.Errorf("the badge token itself must never reach the audit log:\n%s", log)
	}
}

func TestAnUnrecognisedBadgeIsTreatedAsALiteralUsername(t *testing.T) {
	app := newTestApp(t)
	app.authed, app.session, app.cur = false, nil, scrLogin
	doLoginSubmit(app, []string{"not-a-badge-or-a-user", "whatever"})
	if app.authed {
		t.Fatal("an unrecognised badge/username must fail exactly like any other unknown user")
	}
	b, _ := os.ReadFile(config.Get(config.AuditLogFile))
	if !strings.Contains(string(b), "USER:not-a-badge-or-a-user ") {
		t.Errorf("with no badge match, the typed text should be logged as-is (treated as a plain username):\n%s", b)
	}
}
