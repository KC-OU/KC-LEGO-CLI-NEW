package partdb

import "testing"

func TestSetLotsAreSeparateFromLooseStock(t *testing.T) {
	f := newFixture(t)
	loc, err := f.W.ResolveLocation(bg(), append(append([]string{}, SetsLocationPath...), "75192 Millennium Falcon"))
	if err != nil || loc == 0 {
		t.Fatalf("location: %d %v", loc, err)
	}
	again, _ := f.W.ResolveLocation(bg(), append(append([]string{}, SetsLocationPath...), "75192 Millennium Falcon"))
	if again != loc {
		t.Fatalf("resolve must reuse the location: %d vs %d", again, loc)
	}
	spec := PartSpec{Name: "Brick 2 x 4 - Red", IPN: "3001-4", CategoryID: fallbackCategoryID}
	id, created, err := f.W.EnsurePart(bg(), spec)
	if err != nil || !created {
		t.Fatalf("ensure: %v %v", created, err)
	}
	if _, err := f.W.SetLotAt(bg(), id, loc, 12); err != nil {
		t.Fatal(err)
	}
	// The loose sync sets the loose stock only; the set's 12 stay.
	res, err := f.W.UpsertPart(bg(), spec, 3)
	if err != nil {
		t.Fatal(err)
	}
	if res.PrevQty != 0 || res.NewQty != 3 {
		t.Fatalf("loose stock should start at 0 (the set lot is not loose): %+v", res)
	}
	total, _ := f.W.Stock(id)
	if total != 15 {
		t.Fatalf("total = %v, want 12 in the set + 3 loose", total)
	}
	if prev, _ := f.W.SetLotAt(bg(), id, loc, 11); prev != 12 {
		t.Fatalf("set lot before = %v", prev)
	}
	if total, _ = f.W.Stock(id); total != 14 {
		t.Fatalf("total after recount = %v", total)
	}
}
