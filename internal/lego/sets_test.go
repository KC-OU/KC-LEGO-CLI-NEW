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

// TestGetSetByNumResolvesABoxBarcode covers the "scan the box instead" path:
// once a barcode is registered via SetBoxBarcode, GetSetByNum resolves it to
// the real set exactly like the set number itself would -- the one choke
// point every caller (TUI, CLI, mobile) already goes through.
func TestGetSetByNumResolvesABoxBarcode(t *testing.T) {
	db := newFixture(t)
	if err := db.UpsertSet(Set{SetNum: "71788", Name: "Lloyd's Ninja Street Bike"}); err != nil {
		t.Fatalf("UpsertSet: %v", err)
	}
	if err := db.SetBoxBarcode("71788", "673419331765"); err != nil {
		t.Fatalf("SetBoxBarcode: %v", err)
	}

	got, err := db.GetSetByNum("673419331765")
	if err != nil {
		t.Fatalf("GetSetByNum(barcode): %v", err)
	}
	if got.SetNum != "71788" {
		t.Errorf("resolved to %q, want 71788", got.SetNum)
	}

	// Still works by set number too.
	if got, err := db.GetSetByNum("71788"); err != nil || got.SetNum != "71788" {
		t.Errorf("GetSetByNum(set num) after registering a barcode: %v %+v", err, got)
	}

	// Registering the same barcode to a second set is rejected -- two sets
	// sharing one barcode would make scanning ambiguous.
	if err := db.UpsertSet(Set{SetNum: "75192", Name: "Millennium Falcon"}); err != nil {
		t.Fatalf("UpsertSet: %v", err)
	}
	if err := db.SetBoxBarcode("75192", "673419331765"); err == nil {
		t.Error("expected an error registering a barcode already used by another set")
	}

	// Clearing it (blank) and re-registering to the new set works.
	if err := db.SetBoxBarcode("71788", ""); err != nil {
		t.Fatalf("clearing SetBoxBarcode: %v", err)
	}
	if err := db.SetBoxBarcode("75192", "673419331765"); err != nil {
		t.Fatalf("SetBoxBarcode after clearing the old owner: %v", err)
	}
	if got, err := db.GetSetByNum("673419331765"); err != nil || got.SetNum != "75192" {
		t.Errorf("GetSetByNum(barcode) after reassignment: %v %+v", err, got)
	}
}

// TestSetInstructionBookRecordsItDirectly covers assigning a set's own
// instruction-book number(s), not only through catalog import -- including
// the existing slash-separated convention for a multi-book set.
func TestSetInstructionBookRecordsItDirectly(t *testing.T) {
	db := newFixture(t)
	if err := db.UpsertSet(Set{SetNum: "75192", Name: "Millennium Falcon"}); err != nil {
		t.Fatalf("UpsertSet: %v", err)
	}

	if err := db.SetInstructionBook("75192", "6644777", 1); err != nil {
		t.Fatalf("SetInstructionBook: %v", err)
	}
	got, err := db.GetSetByNum("75192")
	if err != nil {
		t.Fatalf("GetSetByNum: %v", err)
	}
	if got.InstructionBookNumber != "6644777" || got.InstructionBookCount != 1 {
		t.Errorf("got = %+v, want 6644777 / 1", got)
	}

	// A multi-book set, the existing slash-separated convention.
	if err := db.SetInstructionBook("75192", "6644777/6644778/6644779", 3); err != nil {
		t.Fatalf("SetInstructionBook (multi): %v", err)
	}
	if got, err := db.GetSetByNum("75192"); err != nil || got.InstructionBookNumber != "6644777/6644778/6644779" || got.InstructionBookCount != 3 {
		t.Errorf("got = %+v, err = %v, want the 3-book string", got, err)
	}

	if err := db.SetInstructionBook("nope", "123", 1); err == nil {
		t.Error("expected an error for an unknown set")
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
