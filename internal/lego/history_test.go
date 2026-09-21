package lego

import (
	"context"
	"fmt"
	"testing"
)

func TestEveryChangeIsJournaledWithWhoAndBeforeAfter(t *testing.T) {
	d := openScratchDB(t)
	d.SetActor("alex")
	own(t, d, "3001", 4, "Red", 5)  // add
	own(t, d, "3001", 4, "Red", 5)  // same quantity: no entry
	own(t, d, "3001", 4, "Red", 12) // change
	_ = d.SetMinQty("3001", 4, "Red", 8)
	_ = d.SetMinQty("3001", 4, "Red", 8) // unchanged: no entry
	_ = d.UpsertSet(Set{SetNum: "75192", Name: "Falcon", Qty: 1})
	_ = d.UpsertSet(Set{SetNum: "75192", Name: "Falcon", Qty: 2})
	_ = d.DeleteOwnedPart("3001", 4, "Red")

	h, err := d.History("", 20)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range h { // newest first
		got = append(got, r.Action+"/"+r.ItemType+"/"+r.Item)
	}
	want := []string{"delete/part/3001", "change/set/75192", "add/set/75192", "min/part/3001", "change/part/3001", "add/part/3001"}
	if len(got) != len(want) {
		t.Fatalf("journal = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d = %s, want %s", i, got[i], want[i])
		}
	}
	if r := h[4]; r.Before != 5 || r.After != 12 || r.Actor != "alex" || r.ColorName != "Red" || r.At.IsZero() {
		t.Errorf("the change entry = %+v", r)
	}
	if r := h[3]; r.Note != "minimum 0 -> 8" {
		t.Errorf("min note = %q", r.Note)
	}
	if only, _ := d.History("75192", 20); len(only) != 2 {
		t.Errorf("filtering by item: %d entries", len(only))
	}
	if lim, _ := d.History("", 2); len(lim) != 2 {
		t.Errorf("limit: %d", len(lim))
	}
	other := openScratchDB(t)
	own(t, other, "1", -1, "", 1)
	if h, _ := other.History("", 5); h[0].Actor != "system" {
		t.Errorf("an unnamed actor is 'system': %+v", h[0])
	}
}

func TestADailySnapshotHoldsTheStateBeforeTheFirstChange(t *testing.T) {
	d := openScratchDB(t)
	own(t, d, "3001", 4, "Red", 5) // the collection was empty: no snapshot is worth taking
	if s, _ := d.Snapshots(10); len(s) != 0 {
		t.Fatalf("no snapshot of an empty collection: %+v", s)
	}
	// simulate the next day: forget today's (there is none) and change again
	d.Exec(`INSERT INTO snapshots (at, day, label, data) VALUES ('2026-01-01T00:00:00', '2026-01-01', 'old', '{}')`)
	own(t, d, "3001", 4, "Red", 9)
	s, _ := d.Snapshots(10)
	if len(s) != 2 {
		t.Fatalf("the first change of a new day snapshots the state before it: %+v", s)
	}
	if s[0].Pieces != 5 || s[0].Lines != 1 || s[0].Label != "start of day, before the first change" {
		t.Errorf("the snapshot holds the collection BEFORE the change (5 pieces): %+v", s[0])
	}
	own(t, d, "3001", 4, "Red", 20) // second change the same day: no second snapshot
	if s, _ := d.Snapshots(10); len(s) != 2 {
		t.Errorf("one snapshot per day: %d", len(s))
	}
}

func TestRestoreBringsBackAnEarlierDayAndCanItselfBeUndone(t *testing.T) {
	d := openScratchDB(t)
	own(t, d, "3001", 4, "Red", 10)
	own(t, d, "3023", 1, "Blue", 6)
	_ = d.UpsertSet(Set{SetNum: "75192", Name: "Falcon", Qty: 1, PartsQty: 7541})
	_ = d.SetSyncedPartID(mustOwned(t, d, "3001", 4, "Red").ID, 77) // linked to Part-DB part 77
	goodID, err := d.TakeSnapshot("good state")
	if err != nil {
		t.Fatal(err)
	}
	// then things go wrong
	own(t, d, "3001", 4, "Red", 1)
	_ = d.DeleteOwnedPart("3023", 1, "Blue")
	own(t, d, "9999", -1, "Odd", 50)
	_ = d.UpsertSet(Set{SetNum: "75192", Name: "Falcon", Qty: 5})
	_ = d.UpsertSet(Set{SetNum: "10497", Name: "Galileo", Qty: 1})

	snap, err := d.FindSnapshot(fmt.Sprint(goodID)) // id 1 is the automatic start-of-day one
	if err != nil || snap.ID != goodID {
		t.Fatalf("FindSnapshot: %+v %v", snap, err)
	}
	plan, err := d.PlanRestore(*snap)
	if err != nil {
		t.Fatal(err)
	}
	if plan.PartsAdded != 1 || plan.PartsChanged != 1 || plan.PartsRemoved != 1 || plan.SetsChanged != 1 || plan.SetsRemoved != 1 || plan.SetsAdded != 0 || len(plan.Samples) == 0 {
		t.Fatalf("plan = %+v", plan)
	}
	if got := mustOwned(t, d, "3001", 4, "Red"); got.Qty != 1 {
		t.Fatal("planning must change nothing")
	}

	if _, err := d.RestoreSnapshot(context.Background(), *snap); err != nil {
		t.Fatal(err)
	}
	if r := mustOwned(t, d, "3001", 4, "Red"); r.Qty != 10 || r.SyncedPartID != 77 {
		t.Errorf("3001 restored with its Part-DB link kept: %+v", r)
	}
	if r, _ := d.GetOwnedPart("3023", 1, "Blue"); r == nil || r.Qty != 6 {
		t.Errorf("the deleted part is back: %+v", r)
	}
	if r, _ := d.GetOwnedPart("9999", -1, "Odd"); r != nil {
		t.Errorf("a part added after the snapshot is gone: %+v", r)
	}
	if s, _ := d.GetSetByNum("75192"); s == nil || s.Qty != 1 || s.PartsQty != 7541 {
		t.Errorf("set restored: %+v", s)
	}
	if s, _ := d.GetSetByNum("10497"); s != nil {
		t.Errorf("a set added later is gone: %+v", s)
	}
	h, _ := d.History("", 1)
	if len(h) != 1 || h[0].Action != "restore" {
		t.Errorf("the restore is journaled: %+v", h)
	}

	// The restore saved the present first, so it can be undone.
	all, _ := d.Snapshots(10)
	var before *Snapshot
	for i := range all {
		if all[i].Label == fmt.Sprintf("before restoring snapshot #%d", goodID) {
			before = &all[i]
		}
	}
	if before == nil {
		t.Fatalf("a 'before restoring' snapshot must exist: %+v", all)
	}
	if _, err := d.RestoreSnapshot(context.Background(), *before); err != nil {
		t.Fatal(err)
	}
	if r := mustOwned(t, d, "3001", 4, "Red"); r.Qty != 1 {
		t.Errorf("undoing the restore brings the bad state back: %+v", r)
	}
	if r, _ := d.GetOwnedPart("9999", -1, "Odd"); r == nil {
		t.Error("...including the part that was added")
	}
}

func mustOwned(t *testing.T, d *DB, num string, color int, name string) *OwnedPart {
	t.Helper()
	p, err := d.GetOwnedPart(num, color, name)
	if err != nil || p == nil {
		t.Fatalf("no owned part %s/%d: %v", num, color, err)
	}
	return p
}

func TestFindSnapshotByDateAndBadReferences(t *testing.T) {
	d := openScratchDB(t)
	own(t, d, "3001", 4, "Red", 1)
	d.Exec(`INSERT INTO snapshots (at, day, label, pieces, data) VALUES ('2026-03-04T10:00:00', '2026-03-04', 'a', 7, '{"parts":[],"sets":[]}'), ('2026-03-04T18:00:00', '2026-03-04', 'b', 8, '{"parts":[],"sets":[]}')`)
	s, err := d.FindSnapshot("2026-03-04")
	if err != nil || s.Label != "a" {
		t.Errorf("a date finds that day's first snapshot: %+v %v", s, err)
	}
	for _, bad := range []string{"", "yesterday", "2026-13-45", "9999", "2030-01-01"} {
		if _, err := d.FindSnapshot(bad); err == nil {
			t.Errorf("%q must be an error", bad)
		}
	}
}

func TestRestoreRefusesADamagedSnapshotWithoutChangingAnything(t *testing.T) {
	d := openScratchDB(t)
	own(t, d, "3001", 4, "Red", 5)
	d.Exec(`INSERT INTO snapshots (at, day, label, data) VALUES ('2026-03-04T10:00:00', '2026-03-04', 'broken', 'not json')`)
	s, _ := d.FindSnapshot("2026-03-04")
	if _, err := d.RestoreSnapshot(context.Background(), *s); err == nil {
		t.Fatal("a damaged snapshot must be refused")
	}
	if r := mustOwned(t, d, "3001", 4, "Red"); r.Qty != 5 {
		t.Errorf("nothing changed: %+v", r)
	}
}

func TestSnapshotRetentionAndImportJournal(t *testing.T) {
	d := openScratchDB(t)
	own(t, d, "3001", 4, "Red", 1)
	for i := 0; i < maxSnapshots+5; i++ {
		d.TakeSnapshot("x")
	}
	if s, _ := d.Snapshots(1000); len(s) != maxSnapshots {
		t.Errorf("snapshots are capped at %d: %d", maxSnapshots, len(s))
	}
	plan, _ := d.PlanImport([]ImportRow{{PartNum: "3001", ColorID: 4, ColorName: "Red", Qty: 5}}, "add")
	if err := d.ApplyImport(plan); err != nil {
		t.Fatal(err)
	}
	h, _ := d.History("3001", 1)
	if len(h) != 1 || h[0].Action != "import" || h[0].Before != 1 || h[0].After != 6 {
		t.Errorf("import is journaled: %+v", h)
	}
}

func TestSparkline(t *testing.T) {
	if got := Sparkline([]float64{1, 2, 3, 4, 5, 6, 7, 8}); got != "▁▂▃▄▅▆▇█" {
		t.Errorf("rising = %q", got)
	}
	if got := Sparkline([]float64{5, 5, 5}); len([]rune(got)) != 3 {
		t.Errorf("flat = %q", got)
	}
	if Sparkline(nil) != "" {
		t.Error("empty")
	}
	if got := Sparkline([]float64{10, 0}); got != "█▁" {
		t.Errorf("falling = %q", got)
	}
}
