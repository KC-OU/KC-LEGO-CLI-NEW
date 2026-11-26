package lego

import (
	"context"
	"math"
	"testing"
)

func TestComputePartOutNeverCountsUnpricedLinesAsZero(t *testing.T) {
	items := []InvItem{{PartNum: "a", Qty: 10}, {PartNum: "b", Qty: 10}, {PartNum: "c", Qty: 20}}
	prices := map[string]float64{"a": 0.5, "b": 1.0} // c has no price
	got := computePartOut(items, func(it InvItem) (float64, string, bool) {
		p, ok := prices[it.PartNum]
		return p, "GBP", ok
	}, 10, 20)
	if got.Gross != 15 || got.Priced != 2 || got.Lines != 3 {
		t.Fatalf("gross/priced/lines = %v/%d/%d", got.Gross, got.Priced, got.Lines)
	}
	if got.Coverage != 0.5 { // 20 of 40 pieces
		t.Errorf("coverage = %v, want 0.5", got.Coverage)
	}
	if math.Abs(got.BreakEven-10.5) > 1e-9 { // 15 - 1.5 fees - 3 labour
		t.Errorf("break-even = %v, want 10.5", got.BreakEven)
	}
	if got.Currency != "GBP" {
		t.Errorf("currency = %q", got.Currency)
	}
}

type fixedPrice struct{ calls []string }

func (f *fixedPrice) AvgPrice(_ context.Context, _, no string, _ int, _ string) (float64, string, bool, error) {
	f.calls = append(f.calls, no)
	return 0.5, "GBP", true, nil
}

func TestRefreshSetPricesSpendsTheLimitOnTheBiggestLinesAndPartOutUsesThem(t *testing.T) {
	db := loadedCatalog(t)
	f := &fixedPrice{}
	n, _, err := db.RefreshSetPrices(context.Background(), f, "75192-1", "U", 1)
	if err != nil || n != 1 || len(f.calls) != 1 || f.calls[0] != "3001" { // 3001 x10 beats 3023 x6
		t.Fatalf("fetched=%d calls=%v err=%v", n, f.calls, err)
	}
	po, err := db.PartOutValue("75192-1", "U", 0, 0)
	if err != nil || po.Priced != 1 || po.Gross != 5 { // 10 x 0.50
		t.Fatalf("partout = %+v err=%v", po, err)
	}
}
