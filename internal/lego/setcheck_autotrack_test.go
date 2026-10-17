package lego

import (
	"context"
	"testing"
)

// TestFinishCheckAutoTracksAMissingPartAsLowStock is roadmap item 7 ("a
// check's missing findings feed the low-stock alert directly, no separate
// manual flag step"): finishing a check that comes back short on a part
// with no min_qty tracked yet should make that part show up in LowStock()
// from then on, with no admin action required.
func TestFinishCheckAutoTracksAMissingPartAsLowStock(t *testing.T) {
	d := buildDB(t)
	_ = d.UpsertSet(Set{SetNum: "1", Name: "Fire Station", Qty: 1, PartsQty: 100})
	c, err := d.NewCheck(context.Background(), nil, "1-1", CheckIntake, "kc")
	if err != nil {
		t.Fatal(err)
	}
	for i := range c.Lines {
		if c.Lines[i].PartNum == "3001" {
			c.Lines[i].Have = 57 // need 60: 3 short
		}
	}
	if before, _ := d.LowStock(); len(before) != 0 {
		t.Fatalf("nothing should be low before the check finishes: %v", before)
	}
	if _, err := d.FinishCheck(c); err != nil {
		t.Fatal(err)
	}

	low, err := d.LowStock()
	if err != nil {
		t.Fatal(err)
	}
	if len(low) != 1 || low[0].PartNum != "3001" || low[0].MinQty != 3 || low[0].Qty != 0 {
		t.Fatalf("LowStock() = %+v, want one row: 3001 min=3 qty=0", low)
	}

	p, err := d.GetOwnedPart("3001", 4, "Red")
	if err != nil || p == nil {
		t.Fatalf("owned part 3001 should now exist: %v, %v", p, err)
	}
}

// TestFinishCheckNeverOverridesAnExistingMinQty: a part the admin already
// chose a threshold for must keep that threshold, even if a check later
// finds a different (larger or smaller) shortfall.
func TestFinishCheckNeverOverridesAnExistingMinQty(t *testing.T) {
	d := buildDB(t)
	own(t, d, "3001", 4, "Red", 2)
	if err := d.SetMinQty("3001", 4, "Red", 10); err != nil {
		t.Fatal(err)
	}
	_ = d.UpsertSet(Set{SetNum: "1", Name: "Fire Station", Qty: 1, PartsQty: 100})
	c, err := d.NewCheck(context.Background(), nil, "1-1", CheckIntake, "kc")
	if err != nil {
		t.Fatal(err)
	}
	for i := range c.Lines {
		if c.Lines[i].PartNum == "3001" {
			c.Lines[i].Have = 57 // 3 short -- must NOT overwrite the admin's min of 10
		}
	}
	if _, err := d.FinishCheck(c); err != nil {
		t.Fatal(err)
	}

	p, err := d.GetOwnedPart("3001", 4, "Red")
	if err != nil || p == nil {
		t.Fatalf("owned part 3001: %v, %v", p, err)
	}
	if p.MinQty != 10 {
		t.Errorf("MinQty = %d, want unchanged 10 (the admin's own threshold)", p.MinQty)
	}
	if p.Qty != 2 {
		t.Errorf("Qty = %d, want unchanged 2 -- a missing finding must never touch held quantity", p.Qty)
	}
}

// TestDamagedPersistsThroughSaveAndReload: a condition flag survives a
// save/reload round trip the same as Have/Extra, and a recount starts fresh
// on it (re-verified, not carried forward indefinitely) the same as Extra.
func TestDamagedPersistsThroughSaveAndReload(t *testing.T) {
	d := buildDB(t)
	_ = d.UpsertSet(Set{SetNum: "1", Name: "Fire Station", Qty: 1, PartsQty: 100})
	c, err := d.NewCheck(context.Background(), nil, "1-1", CheckIntake, "kc")
	if err != nil {
		t.Fatal(err)
	}
	for i := range c.Lines {
		if c.Lines[i].PartNum == "3001" {
			c.Lines[i].Damaged = 2
		}
	}
	if err := d.SaveCheck(c); err != nil {
		t.Fatal(err)
	}

	reloaded, err := d.GetCheck(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	var got int
	for _, l := range reloaded.Lines {
		if l.PartNum == "3001" {
			got = l.Damaged
		}
	}
	if got != 2 {
		t.Fatalf("Damaged after reload = %d, want 2", got)
	}

	if _, err := d.FinishCheck(c); err != nil {
		t.Fatal(err)
	}
	recount, err := d.NewCheck(context.Background(), nil, "1-1", CheckRecount, "kc")
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range recount.Lines {
		if l.Damaged != 0 {
			t.Errorf("recount should start fresh on Damaged, got %d for %s", l.Damaged, l.PartNum)
		}
	}
}

// TestFinishCheckDoesNotAutoTrackOptionalOrFullyPresentParts: nothing to
// flag when nothing is actually short, or when the part is optional
// (CheckLine.Missing() is already 0 for an optional line).
func TestFinishCheckDoesNotAutoTrackOptionalOrFullyPresentParts(t *testing.T) {
	d := buildDB(t)
	_ = d.UpsertSet(Set{SetNum: "1", Name: "Fire Station", Qty: 1, PartsQty: 100})
	c, err := d.NewCheck(context.Background(), nil, "1-1", CheckIntake, "kc")
	if err != nil {
		t.Fatal(err)
	}
	for i := range c.Lines {
		c.Lines[i].Have = c.Lines[i].Need // everything present
	}
	if _, err := d.FinishCheck(c); err != nil {
		t.Fatal(err)
	}
	if low, _ := d.LowStock(); len(low) != 0 {
		t.Errorf("a fully-present check should auto-track nothing: %v", low)
	}
}
