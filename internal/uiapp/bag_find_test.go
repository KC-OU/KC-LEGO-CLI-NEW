package uiapp

import (
	"strings"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

// TestCompletionDashboardShowsPerBagMissingBreakdown is the TUI half of the
// completion dashboard's per-bag breakdown (see DB.MissingByBag): an
// incomplete set's "By bag" column splits its shortfall by which bag each
// short part actually belongs in, not just one set-wide total.
func TestCompletionDashboardShowsPerBagMissingBreakdown(t *testing.T) {
	app, _ := flowApp(t)
	seedOfflineCatalog(t, app)
	if _, err := app.legoDB.Exec(`INSERT INTO cat_inventories (id, version, set_num) VALUES (1, 1, '75192-1');
		INSERT INTO cat_inventory_parts (inventory_id, part_num, color_id, quantity) VALUES (1,'3001',4,10),(1,'3023',1,5)`); err != nil {
		t.Fatal(err)
	}
	if err := app.legoDB.SetBagSize("3023", lego.BagSmall); err != nil {
		t.Fatal(err)
	}

	app.cur, app.stack = scrLegoHub, nil
	startCheck(app, "75192-1", lego.CheckIntake)
	st := app.checking
	if st == nil {
		t.Fatal("expected startCheck to populate app.checking")
	}
	for i := range st.check.Lines {
		switch st.check.Lines[i].PartNum {
		case "3001":
			st.check.Lines[i].Have -= 2 // main bag, 2 short
		case "3023":
			st.check.Lines[i].Have -= 1 // small bag, 1 short
		}
	}
	if _, err := app.legoDB.FinishCheck(st.check); err != nil {
		t.Fatal(err)
	}

	headers, rows, keys := completionRows(app)
	bagCol := -1
	for i, h := range headers {
		if h == "By bag" {
			bagCol = i
		}
	}
	if bagCol < 0 {
		t.Fatalf("headers = %+v, want a By bag column", headers)
	}
	idx := -1
	for i, k := range keys {
		if k == "75192-1" {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("keys = %+v, want 75192-1", keys)
	}
	breakdown := rows[idx][bagCol]
	if !strings.Contains(breakdown, "Main 2") || !strings.Contains(breakdown, "Small Parts 1") {
		t.Errorf("breakdown = %q, want it to mention Main 2 and Small Parts 1", breakdown)
	}
}

// TestBagFindAskScreenResolvesAFreshCodeAndShowsItsDetail is the TUI's lost-
// bag lookup (see DB.FindBag): typing a code that was printed but never
// scanned into anything still resolves it from its own shape, independent
// of any active checking session.
func TestBagFindAskScreenResolvesAFreshCodeAndShowsItsDetail(t *testing.T) {
	app, _ := flowApp(t)
	seedOfflineCatalog(t, app)

	app.cur, app.stack = scrLegoHub, nil
	app.goTo(scrBagFindAsk)
	app.screens[scrBagFindAsk].(*formScreen).submit(app, []string{lego.BagCode("75192-1", lego.BagMain)})

	if app.cur != scrBagFind {
		t.Fatalf("expected to land on scrBagFind, got %q (msg %q)", app.cur, app.message)
	}
	found := false
	for _, row := range app.bagFindRows {
		if row[0] == "Belongs to" && strings.Contains(row[1], "75192-1") {
			found = true
		}
	}
	if !found {
		t.Errorf("bagFindRows = %+v, want a row naming set 75192-1", app.bagFindRows)
	}
}

// TestBagFindShowsProvenanceAndResultToAdminsOnly covers "allow the admin to
// scan or type the bag label to see who made it and what mess-ups it made":
// Printed by/Result/Disputed rows appear for an admin session, and are
// absent for anyone else -- same data, same screen, no new permission.
func TestBagFindShowsProvenanceAndResultToAdminsOnly(t *testing.T) {
	app, _ := flowApp(t)
	seedOfflineCatalog(t, app)
	if _, err := app.legoDB.Exec(`INSERT INTO cat_inventories (id, version, set_num) VALUES (1, 1, '75192-1');
		INSERT INTO cat_inventory_parts (inventory_id, part_num, color_id, quantity) VALUES (1,'3001',4,10)`); err != nil {
		t.Fatal(err)
	}
	app.cur, app.stack = scrLegoHub, nil
	startCheck(app, "75192-1", lego.CheckIntake)
	st := app.checking
	if st == nil {
		t.Fatal("expected startCheck to populate app.checking")
	}
	bag, err := app.legoDB.EnsureCheckBag(st.check.ID, st.check.SetNum, lego.BagSmall, "pat")
	if err != nil {
		t.Fatal(err)
	}
	for i := range st.check.Lines {
		if st.check.Lines[i].PartNum == "3001" {
			st.check.Lines[i].Have -= 1
		}
	}
	if _, err := app.legoDB.FinishCheck(st.check); err != nil {
		t.Fatal(err)
	}

	lookup := func() [][]string {
		app.cur, app.stack = scrLegoHub, nil
		app.goTo(scrBagFindAsk)
		app.screens[scrBagFindAsk].(*formScreen).submit(app, []string{bag.Code})
		return app.bagFindRows
	}

	rows := lookup()
	var printedBy, result string
	for _, row := range rows {
		if row[0] == "Printed by" {
			printedBy = row[1]
		}
		if row[0] == "Result" {
			result = row[1]
		}
	}
	if !strings.Contains(printedBy, "pat") {
		t.Errorf("admin should see who printed it, got rows = %+v", rows)
	}
	if !strings.Contains(result, "1 missing") {
		t.Errorf("admin should see the check's result, got rows = %+v", rows)
	}

	app.session.Permissions.IsAdmin = false
	rows = lookup()
	for _, row := range rows {
		if row[0] == "Printed by" || row[0] == "Result" || row[0] == "Disputed" {
			t.Errorf("a non-admin should not see provenance/outcome rows, got %+v", rows)
		}
	}
}
