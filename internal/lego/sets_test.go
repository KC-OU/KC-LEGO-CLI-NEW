package lego

import "testing"

func TestUpsertAndSearchSets(t *testing.T) {
	db := newFixture(t)

	if err := db.UpsertSet(Set{SetNum: "71788", Name: "Lloyd's Ninja Street Bike", Theme: "Ninjago Core", Year: 2023, Qty: 1, PartsQty: 56}); err != nil {
		t.Fatalf("UpsertSet: %v", err)
	}

	sets, err := db.SearchSets("Ninja")
	if err != nil {
		t.Fatalf("SearchSets: %v", err)
	}
	if len(sets) != 1 || sets[0].SetNum != "71788" {
		t.Fatalf("unexpected search result: %+v", sets)
	}

	// Re-upsert with a changed qty — must update in place, not duplicate.
	if err := db.UpsertSet(Set{SetNum: "71788", Name: "Lloyd's Ninja Street Bike", Theme: "Ninjago Core", Year: 2023, Qty: 2, PartsQty: 56}); err != nil {
		t.Fatalf("UpsertSet (update): %v", err)
	}
	sets, err = db.SearchSets("")
	if err != nil {
		t.Fatalf("SearchSets(all): %v", err)
	}
	if len(sets) != 1 {
		t.Fatalf("expected upsert to update in place, got %d rows", len(sets))
	}
	if sets[0].Qty != 2 {
		t.Errorf("expected qty updated to 2, got %d", sets[0].Qty)
	}

	got, err := db.GetSetByNum("71788")
	if err != nil {
		t.Fatalf("GetSetByNum: %v", err)
	}
	if got.Name != "Lloyd's Ninja Street Bike" {
		t.Errorf("unexpected name: %q", got.Name)
	}

	if _, err := db.GetSetByNum("nope"); err == nil {
		t.Error("expected error for unknown set number")
	}
}

func TestMarkPartedOut(t *testing.T) {
	db := newFixture(t)
	if err := db.UpsertSet(Set{SetNum: "1", Name: "Test Set"}); err != nil {
		t.Fatalf("UpsertSet: %v", err)
	}
	if err := db.MarkPartedOut("1", true); err != nil {
		t.Fatalf("MarkPartedOut: %v", err)
	}
	got, err := db.GetSetByNum("1")
	if err != nil {
		t.Fatalf("GetSetByNum: %v", err)
	}
	if !got.PartedOut {
		t.Error("expected parted_out to be true")
	}

	if err := db.MarkPartedOut("missing", true); err == nil {
		t.Error("expected error marking an unknown set")
	}
}
