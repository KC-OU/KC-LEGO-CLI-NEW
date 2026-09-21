package uiapp

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/auth"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/twofa"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/wmsdb"
)

// graceApp is a signed-out app whose password step has "just succeeded" for
// an account with 2FA enabled, backed by a scratch 2FA store. The ModernWMS
// client points at a container that doesn't exist, so the forced-password-
// change lookup fails fast (and reads as "no") instead of touching live data.
func graceApp(t *testing.T, origin string) (*App, *auth.Session, string) {
	t.Helper()
	app := newTestApp(t) // first: it points TWOFA_FILE at a temp file, which this test then replaces with its own
	f, err := os.CreateTemp(t.TempDir(), "2fa-*.json")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	t.Setenv("TWOFA_FILE", f.Name())

	secret, _, err := twofa.Enroll("graceuser", "modernwms")
	if err != nil {
		t.Fatal(err)
	}
	code, _ := totp.GenerateCode(secret, time.Now())
	if _, err := twofa.Confirm("graceuser", "modernwms", code); err != nil {
		t.Fatal(err)
	}

	app.wms = &wmsdb.Client{Container: "no-such-container-for-tests", DBPath: "/x", Timeout: 2 * time.Second}
	app.SetGraceOrigin(origin)
	sess := &auth.Session{Source: "modernwms", Username: "graceuser", Role: "Picker",
		Permissions: &wmsdb.Permissions{CanWrite: true, Menus: []string{"stockManagement"}}}
	app.session, app.authed, app.cur = sess, false, scrLogin
	return app, sess, secret
}

func auditText(t *testing.T, app *App) string {
	t.Helper()
	b, _ := os.ReadFile(app.audit.Path)
	return string(b)
}

func TestGraceSkipsCodeFromSameOrigin(t *testing.T) {
	app, sess, _ := graceApp(t, "telnet:10.1.1.5")
	if err := twofa.MarkVerified("graceuser", "modernwms", "telnet:10.1.1.5", 30*time.Minute); err != nil {
		t.Fatal(err)
	}

	continueSignOn(app, sess)
	if app.cur != scrHub || !app.authed {
		t.Fatalf("a sign-in inside the window should go straight to the hub (cur=%q authed=%v)", app.cur, app.authed)
	}
	if !strings.Contains(app.message, "2FA code not needed") {
		t.Errorf("the user should be told why no code was asked for: %q", app.message)
	}
	if !strings.Contains(auditText(t, app), "LOGIN_2FA_GRACE") {
		t.Error("skipping the code must be written to the audit log")
	}
}

func TestGraceNotGrantedElsewhere(t *testing.T) {
	for name, origin := range map[string]string{"another address": "telnet:10.9.9.9", "web terminal (unknown address)": ""} {
		app, sess, _ := graceApp(t, origin)
		_ = twofa.MarkVerified("graceuser", "modernwms", "telnet:10.1.1.5", 30*time.Minute)

		continueSignOn(app, sess)
		if app.cur != scrTwoFACode || app.authed {
			t.Errorf("%s must still be asked for a code (cur=%q authed=%v)", name, app.cur, app.authed)
		}
	}
}

func TestGraceOffWhenWindowIsZero(t *testing.T) {
	app, sess, _ := graceApp(t, "local")
	_ = twofa.MarkVerified("graceuser", "modernwms", "local", 30*time.Minute)
	t.Setenv("TWOFA_GRACE_MINUTES", "0")

	continueSignOn(app, sess)
	if app.cur != scrTwoFACode {
		t.Fatalf("TWOFA_GRACE_MINUTES=0 must turn the window off (cur=%q)", app.cur)
	}
}

// The window starts from a real code, not from a password, and a wrong code
// starts nothing.
func TestRealCodeStartsTheWindow(t *testing.T) {
	app, sess, secret := graceApp(t, "local")

	continueSignOn(app, sess) // first sign-in: no window yet
	if app.cur != scrTwoFACode {
		t.Fatalf("first sign-in must ask for the code, got %q", app.cur)
	}
	doTwoFACodeSubmit(app, []string{"000000"})
	if _, ok := twofa.WithinGrace("graceuser", "modernwms", "local", time.Hour); ok {
		t.Fatal("a wrong code must not start a grace window")
	}

	// The enrolment code spent this 30 s step (codes are single-use), so log in with the next step's code, which the +-1 step tolerance accepts.
	code, _ := totp.GenerateCode(secret, time.Now().Add(30*time.Second))
	doTwoFACodeSubmit(app, []string{code})
	if app.cur != scrHub || !app.authed {
		t.Fatalf("a valid code should finish sign-on (cur=%q authed=%v)", app.cur, app.authed)
	}

	// Sign out and back in: now inside the window.
	app.logout()
	app.session = sess
	continueSignOn(app, sess)
	if app.cur != scrHub {
		t.Fatalf("signing back in within the window should skip the code, got %q", app.cur)
	}
}

// A grace sign-in must not refresh the window, or it could be extended forever.
func TestGraceLoginDoesNotExtendTheWindow(t *testing.T) {
	app, sess, _ := graceApp(t, "local")
	_ = twofa.MarkVerified("graceuser", "modernwms", "local", 30*time.Minute)
	entriesBefore, _ := os.ReadFile(os.Getenv("TWOFA_FILE"))

	continueSignOn(app, sess)
	entriesAfter, _ := os.ReadFile(os.Getenv("TWOFA_FILE"))
	if string(entriesBefore) != string(entriesAfter) {
		t.Fatal("a grace sign-in must leave the 2FA store untouched")
	}
}

func TestTwoFAReplayAndLockoutMessages(t *testing.T) {
	app, sess, secret := graceApp(t, "telnet:10.9.9.9")
	continueSignOn(app, sess)

	code, _ := totp.GenerateCode(secret, time.Now().Add(30*time.Second))
	doTwoFACodeSubmit(app, []string{code})
	if !app.authed {
		t.Fatal("a fresh code must sign in")
	}
	app.logout()
	app.session = sess
	app.graceOrigin = "telnet:10.8.8.8" // a different address: no grace, the prompt appears
	continueSignOn(app, sess)
	if app.cur != scrTwoFACode {
		t.Fatalf("expected the code prompt, on %q", app.cur)
	}
	doTwoFACodeSubmit(app, []string{code}) // the same code again
	if app.authed || !strings.Contains(app.message, "already used") {
		t.Fatalf("a replayed code must be refused with an explanation: authed=%v msg=%q", app.authed, app.message)
	}
	if !strings.Contains(auditText(t, app), "LOGIN_2FA_REPLAY") {
		t.Error("a replay should be audited as such")
	}

	for i := 0; i < 5; i++ {
		doTwoFACodeSubmit(app, []string{"000000"})
	}
	if !strings.Contains(app.message, "locked") || !strings.Contains(app.message, "wms users 2fa unlock graceuser") {
		t.Errorf("the lockout message should say how to recover: %q", app.message)
	}
	if !strings.Contains(auditText(t, app), "LOGIN_2FA_LOCKED") {
		t.Error("a lockout should be audited")
	}
	next, _ := totp.GenerateCode(secret, time.Now().Add(60*time.Second))
	doTwoFACodeSubmit(app, []string{next})
	if app.authed {
		t.Error("a correct code while locked must still be refused")
	}
}
