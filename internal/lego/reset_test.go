package lego

import (
	"context"
	"testing"
)

func TestCollectionCountsAndResetClearOwnedDataNotCatalog(t *testing.T) {
	db := newFixture(t)
	if err := db.AddOwnedPart(OwnedPart{PartNum: "3001", Name: "Brick 2 x 4", Category: "Bricks", ColorID: 4, ColorName: "Red", Qty: 10}); err != nil {
		t.Fatal(err)
	}
	order := &Order{SupplierKind: "bricklink", Supplier: "BrickHQ"}
	if err := db.SaveOrder(order); err != nil {
		t.Fatal(err)
	}

	counts, err := db.CollectionCounts()
	if err != nil {
		t.Fatal(err)
	}
	if counts["owned_parts"] != 1 || counts["orders"] != 1 {
		t.Fatalf("counts = %+v, want owned_parts=1 orders=1", counts)
	}

	if err := db.ResetCollection(context.Background(), "test reset"); err != nil {
		t.Fatal(err)
	}

	after, err := db.CollectionCounts()
	if err != nil {
		t.Fatal(err)
	}
	for tbl, n := range after {
		want := 0
		if tbl == "journal" {
			want = 1 // the reset's own journal entry, written after the wipe
		}
		if n != want {
			t.Errorf("table %s has %d row(s) after reset, want %d", tbl, n, want)
		}
	}
	if rows, _ := db.ListOwnedParts(); len(rows) != 0 {
		t.Errorf("owned parts survived a reset: %v", rows)
	}

	// The reset itself leaves exactly one journal row behind (journal was
	// cleared, then this one write happened), not zero.
	hist, err := db.History("", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 1 || hist[0].Action != "reset" {
		t.Fatalf("history = %+v, want exactly one 'reset' entry", hist)
	}
}

func TestResetNeverTouchesTheCatalog(t *testing.T) {
	db := newFixture(t)
	if _, err := db.Exec(`INSERT INTO cat_parts (part_num, name, part_cat_id) VALUES ('3001', 'Brick 2 x 4', 11)`); err != nil {
		t.Fatal(err)
	}
	if err := db.ResetCollection(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM cat_parts`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("the catalog must survive a reset untouched, cat_parts has %d row(s)", n)
	}
}
