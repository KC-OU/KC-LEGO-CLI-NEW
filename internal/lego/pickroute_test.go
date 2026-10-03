package lego

import (
	"context"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/partdb"
)

// seedLocatedPart links a part+colour to a Part-DB shelf location the same
// way a real sync would (owned_parts.synced_part_id -> a part_lots row) —
// "Default" (id 1) is the location every scratch Part-DB already has.
func seedLocatedPart(t *testing.T, d *DB, w *partdb.Writer, pdb *partdb.DB, partNum string, colorID int, locName string) {
	t.Helper()
	locID := 1
	if locName != "Default" {
		if _, err := pdb.Exec(`INSERT INTO storelocations (name, is_full, only_single_part, limit_to_existing_parts, comment, not_selectable) VALUES (?,0,0,0,'',0)`, locName); err != nil {
			t.Fatal(err)
		}
		if err := pdb.QueryRow(`SELECT id FROM storelocations WHERE name = ?`, locName).Scan(&locID); err != nil {
			t.Fatal(err)
		}
	}
	pid, _, err := w.EnsurePart(context.Background(), partdb.PartSpec{Name: partNum, IPN: partNum, CategoryID: 7})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.SetLotAt(context.Background(), pid, locID, 5); err != nil {
		t.Fatal(err)
	}
	if err := d.AddOwnedPart(OwnedPart{PartNum: partNum, ColorID: colorID, Qty: 5}); err != nil {
		t.Fatal(err)
	}
	op, err := d.GetOwnedPart(partNum, colorID, "")
	if err != nil || op == nil {
		t.Fatalf("GetOwnedPart(%q): %+v, %v", partNum, op, err)
	}
	if err := d.SetSyncedPartID(op.ID, pid); err != nil {
		t.Fatal(err)
	}
}

func TestLocationNamesResolvesAndLeavesUnstockedPartsBlank(t *testing.T) {
	s, pdb, _ := newSyncer(t)
	w, d := s.Writer, s.Lego
	seedLocatedPart(t, d, w, pdb, "3001", 4, "Shelf B")

	got := d.LocationNames(pdb, []string{"3001", "9999"}, []int{4, 4}, []string{"", ""})
	if got[0] != "Shelf B" {
		t.Errorf("located part = %q, want Shelf B", got[0])
	}
	if got[1] != "" {
		t.Errorf("never-stocked part = %q, want blank", got[1])
	}
}

func TestSortCheckLinesByLocationPublicAPI(t *testing.T) {
	s, pdb, _ := newSyncer(t)
	w, d := s.Writer, s.Lego
	seedLocatedPart(t, d, w, pdb, "3001", 4, "Shelf B") // "Default" < "Shelf B" alphabetically
	seedLocatedPart(t, d, w, pdb, "3002", 4, "Default")

	lines := []CheckLine{
		{PartNum: "3001", ColorID: 4, Need: 1},
		{PartNum: "3002", ColorID: 4, Need: 1},
		{PartNum: "9999", ColorID: 4, Need: 1}, // never stocked: no known location
	}
	d.SortCheckLinesByLocation(pdb, lines)

	got := []string{lines[0].PartNum, lines[1].PartNum, lines[2].PartNum}
	want := []string{"3002", "3001", "9999"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v (Default, then Shelf B, unlocated last)", got, want)
		}
	}
}
