package uiapp

import (
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

// The "checker" group already grants both sets.check and orders.manage
// (internal/access/access.go) — every checker is already dual-skilled by
// the existing permission model; these tests are about the UI actually
// reflecting that, not a new permission to invent.

func dualSkilledSignOn(t *testing.T, app *App, user string) {
	t.Helper()
	setPolicy(t, func(p *access.Policy) {
		p.Users["partdb:"+user] = &access.User{Groups: []string{"checker"}, TwoFA: access.TwoFAExempt}
	})
	signOn(app, user, "")
}

func TestRoleForAppDefaultsToCheckerWithNothingClaimed(t *testing.T) {
	app := newTestApp(t)
	dualSkilledSignOn(t, app, "dave")

	if role := roleForApp(app); role != lego.AccuracyChecker {
		t.Fatalf("roleForApp with nothing claimed = %q, want checker (today's fallback)", role)
	}
}

func TestRoleForAppFollowsAClaimedOrder(t *testing.T) {
	app := newTestApp(t)
	dualSkilledSignOn(t, app, "dave")

	tk, err := app.legoDB.AssignTicket(lego.TicketOrder, "42", "Order #42", "", "", "", "dave")
	if err != nil {
		t.Fatal(err)
	}
	if err := app.legoDB.ClaimTicket(tk.ID, "dave"); err != nil {
		t.Fatal(err)
	}

	if role := roleForApp(app); role != lego.AccuracyPicker {
		t.Fatalf("roleForApp with an order claimed = %q, want picker", role)
	}
	if label := catalogDynamicLabels["request"](app); label != "Request an order to pick" {
		t.Errorf("dynamic request label = %q, want the picker phrasing", label)
	}
}

func TestRoleForAppSwitchesBackAfterTheOrderIsDone(t *testing.T) {
	app := newTestApp(t)
	dualSkilledSignOn(t, app, "dave")

	tk, err := app.legoDB.AssignTicket(lego.TicketOrder, "42", "Order #42", "", "", "", "dave")
	if err != nil {
		t.Fatal(err)
	}
	if err := app.legoDB.ClaimTicket(tk.ID, "dave"); err != nil {
		t.Fatal(err)
	}
	if role := roleForApp(app); role != lego.AccuracyPicker {
		t.Fatalf("role while the order is claimed = %q, want picker", role)
	}

	if err := app.legoDB.FinishTicket(lego.TicketOrder, "42", "dave"); err != nil {
		t.Fatal(err)
	}
	if role := roleForApp(app); role != lego.AccuracyChecker {
		t.Fatalf("role after finishing the order = %q, want back to checker (nothing claimed)", role)
	}

	ck, err := app.legoDB.AssignTicket(lego.TicketCheck, "75192-1", "75192-1 Falcon", "", "", "", "dave")
	if err != nil {
		t.Fatal(err)
	}
	if err := app.legoDB.ClaimTicket(ck.ID, "dave"); err != nil {
		t.Fatal(err)
	}
	if role := roleForApp(app); role != lego.AccuracyChecker {
		t.Fatalf("role with a check claimed = %q, want checker", role)
	}
}

func TestRoleForAppSingleSkilledPickerIsUnaffected(t *testing.T) {
	app := newTestApp(t)
	setPolicy(t, func(p *access.Policy) {
		p.Users["partdb:pam"] = &access.User{Groups: []string{"picker"}, TwoFA: access.TwoFAExempt}
	})
	signOn(app, "pam", "")

	if role := roleForApp(app); role != lego.AccuracyPicker {
		t.Fatalf("a picker-only account's role = %q, want picker regardless of any ticket", role)
	}
}

// TestRequestJobRowsShowsBothKindsForADualSkilledUserWithNothingClaimed is
// the "so it's fair" case: with nothing claimed yet, roleForApp has no
// current-ticket signal, so the request screen must not silently hide one
// kind of open work behind the other.
func TestRequestJobRowsShowsBothKindsForADualSkilledUserWithNothingClaimed(t *testing.T) {
	app := newTestApp(t)
	dualSkilledSignOn(t, app, "dave")

	if _, err := app.legoDB.AssignTicket(lego.TicketCheck, "75192-1", "75192-1 Falcon", "", "", "", "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.legoDB.AssignTicket(lego.TicketOrder, "42", "Order #42", "", "", "", "admin"); err != nil {
		t.Fatal(err)
	}

	_, rows, _ := requestJobRows(app)
	if len(rows) != 2 {
		t.Fatalf("rows = %+v, want both the open check and the open order listed", rows)
	}
	var sawCheck, sawOrder bool
	for _, r := range rows {
		if len(r) == 0 {
			continue
		}
		switch {
		case r[0] == "[Check] 75192-1 Falcon":
			sawCheck = true
		case r[0] == "[Order] Order #42":
			sawOrder = true
		}
	}
	if !sawCheck || !sawOrder {
		t.Fatalf("rows = %+v, want one [Check] row and one [Order] row", rows)
	}
}

func TestRequestJobRowsStaysSingleKindOnceSomethingIsClaimed(t *testing.T) {
	app := newTestApp(t)
	dualSkilledSignOn(t, app, "dave")

	tk, err := app.legoDB.AssignTicket(lego.TicketOrder, "42", "Order #42", "", "", "", "dave")
	if err != nil {
		t.Fatal(err)
	}
	if err := app.legoDB.ClaimTicket(tk.ID, "dave"); err != nil {
		t.Fatal(err)
	}
	// A second, still-open order (someone else's job dave could still see
	// via "first come, first served") plus an open check neither of these
	// calls should surface once dave already has an order claimed.
	if _, err := app.legoDB.AssignTicket(lego.TicketOrder, "43", "Order #43", "", "", "", "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.legoDB.AssignTicket(lego.TicketCheck, "75192-1", "75192-1 Falcon", "", "", "", "admin"); err != nil {
		t.Fatal(err)
	}

	_, rows, _ := requestJobRows(app)
	if len(rows) != 1 || rows[0][0] != "[Order] Order #43" {
		t.Fatalf("rows = %+v, want exactly the other open order, no check leaking in", rows)
	}
}
