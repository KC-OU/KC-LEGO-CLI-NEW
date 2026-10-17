package uiapp

import (
	"regexp"
	"strings"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/loginlock"
)

// TestCreateUserRequiresANumericIDForPickerAndChecker is the MC9100 ask: a
// handheld's awkward keypad means Picker/Checker accounts need a short
// numeric user ID, not a free-form username -- enforced at creation, not
// retroactively on anyone already set up.
func TestCreateUserRequiresANumericIDForPickerAndChecker(t *testing.T) {
	app := newTestApp(t)
	app.goTo(scrUsersCreate)
	app.screens[scrUsersCreate].(*formScreen).submit(app, []string{"alex", "Checker", ""})
	if !app.messageErr {
		t.Fatal("a non-numeric username for a Checker account should be refused")
	}
	if !strings.Contains(app.message, "4-5 digit") {
		t.Errorf("message = %q, want it to explain the required format", app.message)
	}
}

// TestCreateUserIssuesANumericPINForPickerAndChecker confirms a conforming
// numeric ID is accepted and gets a digits-only PIN (not GenerateTempPassword's
// forced mixed-case/symbol mix, which would defeat the point).
func TestCreateUserIssuesANumericPINForPickerAndChecker(t *testing.T) {
	app := newTestApp(t)
	app.goTo(scrUsersCreate)
	app.screens[scrUsersCreate].(*formScreen).submit(app, []string{"42001", "Checker", ""})
	if app.messageErr {
		t.Fatalf("expected success for a conforming numeric ID, got error: %q", app.message)
	}
	pin := extractTempPassword(t, app.message)
	if !regexp.MustCompile(`^\d{6}$`).MatchString(pin) {
		t.Errorf("temp password %q should be a 6-digit numeric PIN", pin)
	}
}

// TestCreateUserLeavesAdminUsernamesFreeForm confirms this is role-scoped,
// not a blanket rule -- an Admin account's username is untouched.
func TestCreateUserLeavesAdminUsernamesFreeForm(t *testing.T) {
	app := newTestApp(t)
	app.goTo(scrUsersCreate)
	app.screens[scrUsersCreate].(*formScreen).submit(app, []string{"alex", "Admin", ""})
	if app.messageErr {
		t.Fatalf("an Admin account's username should still be free-form, got error: %q", app.message)
	}
}

// TestResetPasswordIssuesANumericPINForAnExistingCheckerAndClearsLockout
// covers both halves of the reset path: the numeric username itself (not a
// role lookup -- Part-DB's own user table only distinguishes admin from
// everyone else, not Picker from Checker) is the signal that this account
// was provisioned as a numeric-login one, and a reset folds "give them
// another chance" into the same action instead of a separate
// lockout-clearing screen.
func TestResetPasswordIssuesANumericPINForAnExistingCheckerAndClearsLockout(t *testing.T) {
	app := newTestApp(t)
	app.goTo(scrUsersCreate)
	app.screens[scrUsersCreate].(*formScreen).submit(app, []string{"42002", "Picker", ""})
	if app.messageErr {
		t.Fatalf("setup: creating 42002 failed: %q", app.message)
	}

	for i := 0; i < loginlock.MaxFailures; i++ {
		if _, _, err := loginlock.RecordFailure("42002"); err != nil {
			t.Fatal(err)
		}
	}
	if _, locked, err := loginlock.Locked("42002"); err != nil || !locked {
		t.Fatalf("setup: expected 42002 to be locked, got locked=%v err=%v", locked, err)
	}

	app.goTo(scrUsersReset)
	app.screens[scrUsersReset].(*formScreen).submit(app, []string{"42002"})
	if app.messageErr {
		t.Fatalf("reset failed: %q", app.message)
	}
	pin := extractTempPassword(t, app.message)
	if !regexp.MustCompile(`^\d{6}$`).MatchString(pin) {
		t.Errorf("temp password %q should be a 6-digit numeric PIN (role Picker)", pin)
	}

	if _, locked, err := loginlock.Locked("42002"); err != nil || locked {
		t.Errorf("a reset should clear the lockout too, got locked=%v err=%v", locked, err)
	}
}

// TestResetPasswordLeavesANonNumericUsernameWithTheOldTempPassword confirms
// the numeric PIN only kicks in for a numeric username -- an existing
// free-form (e.g. Admin) account's reset is unaffected.
func TestResetPasswordLeavesANonNumericUsernameWithTheOldTempPassword(t *testing.T) {
	app := newTestApp(t)
	app.goTo(scrUsersCreate)
	app.screens[scrUsersCreate].(*formScreen).submit(app, []string{"alex", "Admin", ""})
	if app.messageErr {
		t.Fatalf("setup: creating alex failed: %q", app.message)
	}

	app.goTo(scrUsersReset)
	app.screens[scrUsersReset].(*formScreen).submit(app, []string{"alex"})
	if app.messageErr {
		t.Fatalf("reset failed: %q", app.message)
	}
	pw := extractTempPassword(t, app.message)
	if regexp.MustCompile(`^\d+$`).MatchString(pw) {
		t.Errorf("a non-numeric username's reset should not get a digits-only PIN, got %q", pw)
	}
}

func extractTempPassword(t *testing.T, msg string) string {
	t.Helper()
	m := regexp.MustCompile(`Temp password: (\S+)`).FindStringSubmatch(msg)
	if len(m) != 2 {
		t.Fatalf("message = %q, want it to contain a temp password", msg)
	}
	return m[1]
}
