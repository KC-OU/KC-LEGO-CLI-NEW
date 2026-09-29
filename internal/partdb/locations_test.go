package partdb

import (
	"testing"
	"time"
)

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

// TestLooseLotsDoesNotDeadlockOnASingleConnection guards a real bug: looseLots
// used to call setLocationIDs (which runs its own query) while its own rows from
// an earlier query were still open. That's invisible with an unbounded
// connection pool — a second connection just opens — but with exactly one
// connection (as some callers may reasonably want, to stop a process's own
// goroutines contending with each other) the second query can never get a
// connection, and the first is never released to give it one: a permanent
// self-deadlock. This forces a single connection and fails fast instead of
// hanging for minutes if the ordering regresses.
func TestLooseLotsDoesNotDeadlockOnASingleConnection(t *testing.T) {
	f := newFixture(t)
	f.DB.SetMaxOpenConns(1)
	loc, err := f.W.ResolveLocation(bg(), append(append([]string{}, SetsLocationPath...), "10254-1 Winter Village Train"))
	if err != nil || loc == 0 {
		t.Fatalf("location: %d %v", loc, err)
	}
	spec := PartSpec{Name: "Plate 1 x 3", IPN: "3623-1", CategoryID: fallbackCategoryID}
	id, _, err := f.W.EnsurePart(bg(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.W.SetLotAt(bg(), id, loc, 4); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := f.W.UpsertPart(bg(), spec, 2)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("UpsertPart did not return within 5s — looks deadlocked on a single connection")
	}
}
