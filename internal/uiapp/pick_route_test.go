package uiapp

import (
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/partdb"
)

// seedLocatedPart links a part+colour to a Part-DB shelf location the same
// way a real sync would (owned_parts.synced_part_id -> a part_lots row) —
// "Default" (id 1) is the location every scratch Part-DB already has (see
// partdbtest.New); any other name creates a new one.
func seedLocatedPart(t *testing.T, app *App, partNum string, colorID int, locName string) {
	t.Helper()
	locID := 1
	if locName != "Default" {
		if _, err := app.pdb.Exec(`INSERT INTO storelocations (name, is_full, only_single_part, limit_to_existing_parts, comment, not_selectable) VALUES (?,0,0,0,'',0)`, locName); err != nil {
			t.Fatal(err)
		}
		if err := app.pdb.QueryRow(`SELECT id FROM storelocations WHERE name = ?`, locName).Scan(&locID); err != nil {
			t.Fatal(err)
		}
	}
	pid, _, err := app.pdbw.EnsurePart(app.ctx(), partdb.PartSpec{Name: partNum, IPN: partNum, CategoryID: 7})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.pdbw.SetLotAt(app.ctx(), pid, locID, 5); err != nil {
		t.Fatal(err)
	}
	if err := app.legoDB.AddOwnedPart(lego.OwnedPart{PartNum: partNum, ColorID: colorID, Qty: 5}); err != nil {
		t.Fatal(err)
	}
	op, err := app.legoDB.GetOwnedPart(partNum, colorID, "")
	if err != nil || op == nil {
		t.Fatalf("GetOwnedPart(%q): %+v, %v", partNum, op, err)
	}
	if err := app.legoDB.SetSyncedPartID(op.ID, pid); err != nil {
		t.Fatal(err)
	}
}

func TestSortCheckLinesByLocation(t *testing.T) {
	app, _ := newTestEnv(t)
	seedLocatedPart(t, app, "3001", 4, "Shelf B") // "Default" < "Shelf B" alphabetically
	seedLocatedPart(t, app, "3002", 4, "Default")

	lines := []lego.CheckLine{
		{PartNum: "3001", ColorID: 4, Need: 1},
		{PartNum: "3002", ColorID: 4, Need: 1},
		{PartNum: "9999", ColorID: 4, Need: 1}, // never stocked in Part-DB: no known location
	}
	app.sortCheckLinesByLocation(lines)

	got := []string{lines[0].PartNum, lines[1].PartNum, lines[2].PartNum}
	want := []string{"3002", "3001", "9999"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v (Default, then Shelf B, unlocated last)", got, want)
		}
	}
}

func TestSortCheckLinesByLocationLeavesOrderAloneWhenNothingsLocated(t *testing.T) {
	app, _ := newTestEnv(t)
	lines := []lego.CheckLine{{PartNum: "a"}, {PartNum: "b"}, {PartNum: "c"}}
	app.sortCheckLinesByLocation(lines)
	if lines[0].PartNum != "a" || lines[1].PartNum != "b" || lines[2].PartNum != "c" {
		t.Errorf("with nothing stocked yet, order should be unchanged: %+v", lines)
	}
}

func TestSortedOrderLinesDoesNotMutateTheCaller(t *testing.T) {
	app, _ := newTestEnv(t)
	seedLocatedPart(t, app, "3001", 4, "Shelf B")
	seedLocatedPart(t, app, "3002", 4, "Default")

	orig := []lego.OrderLine{
		{ID: 1, PartNum: "3001", ColorID: 4},
		{ID: 2, PartNum: "3002", ColorID: 4},
	}
	sorted := app.sortedOrderLines(orig)

	if orig[0].ID != 1 || orig[1].ID != 2 {
		t.Errorf("the caller's slice must not be reordered in place: %+v", orig)
	}
	if sorted[0].ID != 2 || sorted[1].ID != 1 {
		t.Errorf("sorted order = %+v, want Default (id 2) before Shelf B (id 1)", sorted)
	}
}
