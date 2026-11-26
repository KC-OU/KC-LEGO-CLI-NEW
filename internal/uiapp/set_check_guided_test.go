package uiapp

import (
	"bytes"
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/auth"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/exports"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/wmsdb"
)

// TestGuidedWalkTogglesAndShowsGoToBanner covers the sentry-wms-inspired "W"
// view: a big GO TO banner and item-by-item stepping, as an alternate
// presentation of the exact same check — not a different screen, not
// different keys, see set_check.go's guidedBody.
func TestGuidedWalkTogglesAndShowsGoToBanner(t *testing.T) {
	app, _ := flowApp(t)
	app.rebrick = &lego.Client{}
	seedOfflineCatalog(t, app)
	for _, q := range []string{
		`INSERT INTO cat_inventories (id, version, set_num) VALUES (1, 1, '75192-1')`,
		`INSERT INTO cat_inventory_parts (inventory_id, part_num, color_id, quantity) VALUES (1,'3001',4,10),(1,'3001',1,6)`,
	} {
		if _, err := app.legoDB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	app.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	app.cur, app.stack = scrLegoHub, nil
	startCheck(app, "75192-1", lego.CheckIntake)

	v := plain(app.View())
	if strings.Contains(v, "GO TO:") {
		t.Fatalf("table view must be the default, not guided:\n%s", v)
	}
	if !strings.Contains(v, "W guided walk") {
		t.Fatalf("table view's footer should hint at W:\n%s", v)
	}

	typeKeys(app, "w")
	v = plain(app.View())
	if !strings.Contains(v, "Guided walk: 75192-1") || !strings.Contains(v, "GO TO:") || !strings.Contains(v, "Item 1 of 2") {
		t.Fatalf("guided view after W:\n%s", v)
	}
	if !strings.Contains(v, "Next:") {
		t.Fatalf("with two lines, guided view should preview the next one:\n%s", v)
	}

	// H (have all) still works exactly as it does in table mode — guided is
	// only a different Body(), not different behaviour.
	typeKeys(app, "h")
	v = plain(app.View())
	if !strings.Contains(v, "Item 2 of 2") {
		t.Fatalf("H should mark the current line and step forward, got:\n%s", v)
	}
	if !strings.Contains(v, "Last item in this walk.") {
		t.Fatalf("on the final line, guided view should say so instead of a Next: line:\n%s", v)
	}

	typeKeys(app, "w")
	v = plain(app.View())
	if strings.Contains(v, "GO TO:") {
		t.Fatalf("W again should toggle back to the table view:\n%s", v)
	}
}

// TestPrintPartsSheetSavesAPDFExport covers the "scanning stays optional"
// request: P on the Parts Check screen saves a printable barcode sheet for
// the current check, the same export-and-link path every other download in
// this app uses.
func TestPrintPartsSheetSavesAPDFExport(t *testing.T) {
	app, _ := flowApp(t)
	app.rebrick = &lego.Client{}
	seedOfflineCatalog(t, app)
	for _, q := range []string{
		`INSERT INTO cat_inventories (id, version, set_num) VALUES (1, 1, '75192-1')`,
		`INSERT INTO cat_inventory_parts (inventory_id, part_num, color_id, quantity) VALUES (1,'3001',4,10),(1,'3001',1,6)`,
	} {
		if _, err := app.legoDB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	app.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	app.cur, app.stack = scrLegoHub, nil
	startCheck(app, "75192-1", lego.CheckIntake)

	typeKeys(app, "p")

	files, err := exports.ListExports(exports.Dir(), "admin")
	if err != nil || len(files) != 1 {
		t.Fatalf("ListExports = %+v, %v, want one saved sheet", files, err)
	}
	body, err := os.ReadFile(files[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(body, []byte("%PDF-1.4")) {
		t.Error("the saved export should be a well-formed PDF")
	}
	if !strings.Contains(app.message, "Parts barcode sheet") {
		t.Errorf("message = %q, want it to confirm the sheet was made", app.message)
	}
}

// TestBorrowedKeyCountsThePartAsPresentNotMissing covers L: part of a line's shortfall was borrowed from
// another set, so the check shows it as borrowed rather than missing, and finishing needs the record.
func TestBorrowedKeyCountsThePartAsPresentNotMissing(t *testing.T) {
	app, _ := flowApp(t)
	app.rebrick = &lego.Client{}
	seedOfflineCatalog(t, app)
	for _, q := range []string{
		`INSERT INTO cat_inventories (id, version, set_num) VALUES (1, 1, '75192-1')`,
		`INSERT INTO cat_inventory_parts (inventory_id, part_num, color_id, quantity) VALUES (1,'3001',4,10)`,
	} {
		if _, err := app.legoDB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	app.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	app.cur, app.stack = scrLegoHub, nil
	startCheck(app, "75192-1", lego.CheckIntake)

	enter := func() { app.Update(tea.KeyMsg{Type: tea.KeyEnter}) }
	typeKeys(app, "m3")
	enter() // 3 missing
	if m := app.checking.check.Lines[0].Missing(); m != 3 {
		t.Fatalf("missing = %d, want 3", m)
	}
	typeKeys(app, "l")
	enter() // 3 borrowed (the prompt pre-fills the shortfall)
	typeKeys(app, "10696")
	enter()
	l := app.checking.check.Lines[0]
	if l.Borrowed != 3 || l.BorrowedFrom != "10696-1" || l.Missing() != 0 {
		t.Fatalf("line = %+v (missing %d), want 3 borrowed from 10696-1 and none missing", l, l.Missing())
	}
	b, _ := app.legoDB.BorrowedForSet("75192-1")
	if len(b) != 1 || b[0].Qty != 3 || b[0].FromSetNum != "10696-1" {
		t.Fatalf("borrowed list = %+v, want one record of 3 from 10696-1", b)
	}
}

// TestOnboardingGateBlocksACheckerUntilTheirPaperCheckIsVerified: a checker (not an admin) cannot start a first
// check of a set until an admin has verified their paper check; verifying it opens the door.
func TestOnboardingGateBlocksACheckerUntilTheirPaperCheckIsVerified(t *testing.T) {
	app, _ := flowApp(t)
	app.rebrick = &lego.Client{}
	seedOfflineCatalog(t, app)
	for _, q := range []string{
		`INSERT INTO cat_inventories (id, version, set_num) VALUES (1, 1, '75192-1')`,
		`INSERT INTO cat_inventory_parts (inventory_id, part_num, color_id, quantity) VALUES (1,'3001',4,10)`,
	} {
		if _, err := app.legoDB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	app.session = &auth.Session{Source: "wms", Username: "4821", Role: "Checker", Permissions: &wmsdb.Permissions{CanWrite: true, Menus: []string{"*"}}}
	app.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	app.cur, app.stack = scrLegoHub, nil
	startCheck(app, "75192-1", lego.CheckIntake)
	if app.checking != nil || !strings.Contains(app.message, "onboarding first") {
		t.Fatalf("the check started without a verified paper check (message %q)", app.message)
	}
	f, _ := app.legoDB.StartFlow("75192-1", "4821")
	_ = app.legoDB.SubmitPaper(f.ID, "4821", "upload", "x.pdf", "")
	if _, err := app.legoDB.VerifyFlow(f.ID, "admin", true, "", ""); err != nil {
		t.Fatal(err)
	}
	startCheck(app, "75192-1", lego.CheckIntake)
	if app.checking == nil {
		t.Fatalf("the check should start once the paper check is verified (message %q)", app.message)
	}
}
