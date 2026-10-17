package uiapp

import (
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

// TestSetElementIDScreenRecordsItAndUpdatesTheLineInPlace covers the
// checker/admin-usable "add a missing element id" flow reached from the
// Where to Buy (shop links) screen's E key: it writes through
// DB.SetElementID (so it survives) and also updates the in-memory
// ShoppingLine the shop-links screen is already showing, so it reflects
// immediately without a re-fetch.
func TestSetElementIDScreenRecordsItAndUpdatesTheLineInPlace(t *testing.T) {
	app := newTestApp(t)
	app.workshop = &workshopState{
		set: "75192-1",
		lines: []lego.ShoppingLine{
			{CheckLine: lego.CheckLine{PartNum: "3001", ColorID: 4, ColorName: "Red", PartName: "Brick 2 x 4"}},
		},
		line: 0,
	}

	screen := setElementIDScreen().(*formScreen)
	fields := screen.build(app)
	if len(fields) != 3 || fields[0].Value != "3001" || fields[1].Value != "Red" || fields[2].Value != "" {
		t.Fatalf("build() = %+v, want Part/Colour prefilled and a blank element id", fields)
	}

	screen.submit(app, []string{"3001", "Red", "6644777"})
	if app.messageErr {
		t.Fatalf("submit failed: %q", app.message)
	}
	if app.ws().lines[0].ElementID != "6644777" {
		t.Errorf("in-memory line = %+v, want ElementID 6644777", app.ws().lines[0])
	}
	if got := app.legoDB.ElementFor("3001", 4); got != "6644777" {
		t.Errorf("DB.ElementFor = %q, want 6644777", got)
	}

	// Rebuilding the form shows what was just saved.
	fields = screen.build(app)
	if fields[2].Value != "6644777" {
		t.Errorf("rebuilt field = %+v, want the saved value", fields[2])
	}
}

func TestSetElementIDScreenRejectsANonNumericValue(t *testing.T) {
	app := newTestApp(t)
	app.workshop = &workshopState{
		lines: []lego.ShoppingLine{{CheckLine: lego.CheckLine{PartNum: "3001", ColorID: 4, ColorName: "Red"}}},
		line:  0,
	}
	screen := setElementIDScreen().(*formScreen)
	screen.submit(app, []string{"3001", "Red", "not-digits"})
	if !app.messageErr {
		t.Error("expected an error for a non-numeric element id")
	}
}

// TestSetElementIDScreenIsReachableByCheckersNotJustAdmins confirms the
// permission gate matches "checker and admin", per the literal ask -- not
// lego.edit, which operators (not checkers) hold.
func TestSetElementIDScreenIsReachableByCheckersNotJustAdmins(t *testing.T) {
	if perm := screenPerm[scrSetElementID]; perm != "sets.check" {
		t.Errorf("screenPerm[scrSetElementID] = %q, want sets.check (checkers hold this, lego.edit they don't)", perm)
	}
}
