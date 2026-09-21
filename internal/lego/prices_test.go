package lego

import (
	"context"
	"errors"
	"testing"
	"time"
)

func timeAt(y, m, d, h, min int) time.Time {
	return time.Date(y, time.Month(m), d, h, min, 0, 0, time.UTC)
}

type fakePrices struct {
	prices map[string]float64 // "PART/3001/5/U" -> avg
	calls  []string
	failAt int // fail (with errBudgetTest) on this call number (1-based), 0 = never
}

var errBudgetTest = errors.New("budget spent")

func (f *fakePrices) AvgPrice(_ context.Context, itemType, no string, blColor int, cond string) (float64, string, bool, error) {
	key := itemType + "/" + no + "/" + itoa(blColor) + "/" + cond
	f.calls = append(f.calls, key)
	if f.failAt > 0 && len(f.calls) == f.failAt {
		return 0, "", false, errBudgetTest
	}
	if p, ok := f.prices[key]; ok {
		return p, "GBP", true, nil
	}
	return 0, "", false, nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	if neg {
		s = "-" + s
	}
	return s
}

func valueDB(t *testing.T) *DB {
	d := openScratchDB(t)
	d.SetBLColors([]BLColorRow{{RBID: 4, BLID: 5, Name: "Red"}, {RBID: 1, BLID: 7, Name: "Blue"}})
	own(t, d, "3001", 4, "Red", 100)
	own(t, d, "3023", 1, "Blue", 50)
	own(t, d, "9999", -1, "Sparkly", 10) // free-text colour: priced as colourless
	_ = d.UpsertSet(Set{SetNum: "75192", Name: "Falcon", Qty: 1, PartsQty: 7541})
	return d
}

func TestValueIsPricedInDailySlicesAndNeverCountsMissingAsZero(t *testing.T) {
	d := valueDB(t)
	f := &fakePrices{prices: map[string]float64{
		"PART/3001/5/U": 0.05, "PART/3023/7/U": 0.10, "SET/75192-1/0/U": 400,
	}}
	ctx := context.Background()

	if v, _ := d.CollectionValue("U"); v.Priced != 0 || v.Lines != 4 || v.Total != 0 {
		t.Fatalf("nothing fetched yet: %+v", v)
	}
	got, unk, err := d.RefreshPrices(ctx, f, "U", 2) // a small daily slice
	if err != nil || got+unk != 2 || len(f.calls) != 2 {
		t.Fatalf("slice 1: fetched=%d unknown=%d err=%v calls=%v", got, unk, err, f.calls)
	}
	got, unk, err = d.RefreshPrices(ctx, f, "U", 10)
	if err != nil || got+unk != 2 {
		t.Fatalf("slice 2 finishes the rest: fetched=%d unknown=%d err=%v", got, unk, err)
	}
	v, err := d.CollectionValue("U")
	if err != nil || v.Priced != 3 || v.Lines != 4 {
		t.Fatalf("value = %+v %v", v, err)
	}
	if want := 100*0.05 + 50*0.10 + 400; v.Total < want-1e-9 || v.Total > want+1e-9 || v.Currency != "GBP" || v.Sets != 400 || v.Top[0].Total != 400 {
		t.Errorf("value = %+v, want total %v", v, want)
	}
	// nothing is due now, so no more calls
	n := len(f.calls)
	if got, unk, _ := d.RefreshPrices(ctx, f, "U", 10); got+unk != 0 || len(f.calls) != n {
		t.Errorf("fresh prices are not fetched again (calls %d -> %d)", n, len(f.calls))
	}
	// the unknown item is remembered so it is not asked about again
	if p, _ := d.getPrice("PART", "9999", 0, "U"); p == nil || !p.Missing {
		t.Errorf("an unknown item is remembered as missing: %+v", p)
	}
}

func TestRefreshStopsAtTheFirstErrorAndKeepsWhatItGot(t *testing.T) {
	d := valueDB(t)
	f := &fakePrices{prices: map[string]float64{"PART/3001/5/U": 0.05, "PART/3023/7/U": 0.10}, failAt: 2}
	got, unk, err := d.RefreshPrices(context.Background(), f, "U", 10)
	if !errors.Is(err, errBudgetTest) || got+unk != 1 || len(f.calls) != 2 {
		t.Fatalf("fetched=%d unknown=%d err=%v calls=%v", got, unk, err, f.calls)
	}
	if p, _ := d.getPrice("PART", "9999", 0, "U"); p == nil {
		t.Errorf("what was learned before the error is kept")
	}
	if p, _ := d.getPrice("PART", "3023", 7, "U"); p != nil {
		t.Errorf("the call that failed stored nothing: %+v", p)
	}
}

func TestWatchAlertsOnceThenOnlyWhenThePriceFallsFurther(t *testing.T) {
	d := valueDB(t)
	if err := d.AddWatch(Watch{ItemType: "PART", ItemNo: "3001", ColorID: 4, ColorName: "Red", Cond: "U", MaxPrice: 0.06}); err != nil {
		t.Fatal(err)
	}
	if err := d.AddWatch(Watch{ItemType: "SET", ItemNo: "75192", ColorID: -1, MaxPrice: 300}); err != nil { // "-1" is added
		t.Fatal(err)
	}
	ctx := context.Background()
	f := &fakePrices{prices: map[string]float64{"PART/3001/5/U": 0.08, "SET/75192-1/0/U": 350}}
	if hits, err := d.CheckWatches(ctx, f); err != nil || len(hits) != 0 {
		t.Fatalf("above the limit: %v %v", hits, err)
	}
	f.prices["PART/3001/5/U"] = 0.055
	hits, _ := d.CheckWatches(ctx, f)
	if len(hits) != 1 || hits[0].ItemNo != "3001" || hits[0].Price != 0.055 || hits[0].Currency != "GBP" {
		t.Fatalf("under the limit: %+v", hits)
	}
	f.prices["PART/3001/5/U"] = 0.058 // bouncing around the limit: no repeat
	if hits, _ := d.CheckWatches(ctx, f); len(hits) != 0 {
		t.Errorf("no repeat alert inside the cooldown: %+v", hits)
	}
	f.prices["PART/3001/5/U"] = 0.04 // a real further drop alerts again
	if hits, _ := d.CheckWatches(ctx, f); len(hits) != 1 {
		t.Errorf("a further drop alerts again: %+v", hits)
	}
	ws, _ := d.ListWatches()
	if len(ws) != 2 || ws[1].LastPrice == 0 || ws[1].LastChecked.IsZero() {
		t.Errorf("watches record what they saw: %+v", ws)
	}
}

func TestWatchValidationAndRemoval(t *testing.T) {
	d := openScratchDB(t)
	for _, w := range []Watch{{ItemType: "PART", ItemNo: "1", MaxPrice: 0}, {ItemType: "PART", ItemNo: "1", MaxPrice: -1}, {ItemType: "MINIFIG", ItemNo: "1", MaxPrice: 1}} {
		if err := d.AddWatch(w); err == nil {
			t.Errorf("%+v must be refused", w)
		}
	}
	_ = d.AddWatch(Watch{ItemType: "PART", ItemNo: "3001", ColorID: 4, MaxPrice: 1})
	_ = d.AddWatch(Watch{ItemType: "PART", ItemNo: "3001", ColorID: 4, MaxPrice: 2}) // updates the limit, no duplicate
	ws, _ := d.ListWatches()
	if len(ws) != 1 || ws[0].MaxPrice != 2 || ws[0].Cond != "U" {
		t.Fatalf("watches = %+v", ws)
	}
	if err := d.RemoveWatch(ws[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := d.RemoveWatch(ws[0].ID); err == nil {
		t.Error("removing twice must say so")
	}
}

func TestBudgetIsSharedAcrossHandlesAndResetsDaily(t *testing.T) {
	d := openScratchDB(t)
	d2, err := Open(d.path())
	if err != nil {
		t.Fatal(err)
	}
	defer d2.Close()
	day1 := timeAt(2026, 9, 20, 23, 59)
	for i := 1; i <= 3; i++ {
		db := d
		if i%2 == 0 {
			db = d2 // another process, same file
		}
		if used, ok, err := db.SpendAPIBudget("bricklink", 3, day1); err != nil || !ok || used != i {
			t.Fatalf("call %d: used=%d ok=%v err=%v", i, used, ok, err)
		}
	}
	if _, ok, _ := d2.SpendAPIBudget("bricklink", 3, day1); ok {
		t.Error("the fourth call of the day must be refused, whichever process asks")
	}
	if got := d.APIUsage("bricklink", day1); got != 3 {
		t.Errorf("usage = %d", got)
	}
	day2 := timeAt(2026, 9, 21, 0, 1)
	if used, ok, _ := d.SpendAPIBudget("bricklink", 3, day2); !ok || used != 1 {
		t.Errorf("a new UTC day starts at zero: used=%d ok=%v", used, ok)
	}
	if got := d.APIUsage("bricklink", day1); got != 0 {
		t.Errorf("yesterday's counter is dropped: %d", got)
	}
}

func TestBLColorMapping(t *testing.T) {
	d := loadedCatalog(t)
	rows, unmatched, err := d.MatchBLColorsByName(map[int]string{5: "Red", 7: "blue", 11: "Black", 999: "Chrome Antique Brass Sparkle"})
	if err != nil || len(rows) != 3 || len(unmatched) != 1 || unmatched[0] != "Chrome Antique Brass Sparkle" {
		t.Fatalf("rows=%+v unmatched=%v err=%v", rows, unmatched, err)
	}
	if err := d.SetBLColors(rows); err != nil {
		t.Fatal(err)
	}
	if bl, ok := d.BLColorFor(4); !ok || bl != 5 {
		t.Errorf("Rebrickable Red (4) is BrickLink 5: %d %v", bl, ok)
	}
	if c, ok := d.RBColorFor(7); !ok || c.Name != "Blue" {
		t.Errorf("BrickLink 7 is Blue: %+v %v", c, ok)
	}
	if _, ok := d.BLColorFor(12345); ok {
		t.Error("unmapped colours are not found")
	}
	if d.BLColorCount() != 3 {
		t.Errorf("count = %d", d.BLColorCount())
	}
}
