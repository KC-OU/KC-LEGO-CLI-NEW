package lego

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

// TestMigrationAddsBaggingTables confirms a pre-v5 lego.db (rota/clock
// tables already present, no part_bag_size/check_bags) picks them up.
func TestMigrationAddsBaggingTables(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lego.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = raw.Exec(`
		CREATE TABLE owned_parts (id INTEGER PRIMARY KEY AUTOINCREMENT, part_num TEXT NOT NULL, name TEXT NOT NULL DEFAULT '',
			category TEXT NOT NULL DEFAULT '', color_id INTEGER NOT NULL DEFAULT -1, color_name TEXT NOT NULL DEFAULT '',
			qty INTEGER NOT NULL DEFAULT 0, min_qty INTEGER NOT NULL DEFAULT 0, synced_part_id INTEGER NOT NULL DEFAULT 0, updated_at TEXT NOT NULL);
		CREATE TABLE set_state (set_num TEXT PRIMARY KEY, location TEXT NOT NULL DEFAULT '', condition TEXT NOT NULL DEFAULT '',
			condition_note TEXT NOT NULL DEFAULT '', missing_qty INTEGER NOT NULL DEFAULT 0, last_check_id INTEGER NOT NULL DEFAULT 0,
			pdb_location_id INTEGER NOT NULL DEFAULT 0, image_url TEXT NOT NULL DEFAULT '');
		PRAGMA user_version = 4`)
	raw.Close()
	if err != nil {
		t.Fatal(err)
	}

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open (migrating v4 -> v5): %v", err)
	}
	defer db.Close()

	var version int
	_ = db.QueryRow("PRAGMA user_version").Scan(&version)
	if version != schemaVersion {
		t.Errorf("user_version = %d, want %d", version, schemaVersion)
	}
	if err := db.SetBagSize("3001", BagSmall); err != nil {
		t.Fatalf("SetBagSize after migration: %v", err)
	}
	if err := db.RecordCheckBag(1, "BAG-1", "dave"); err != nil {
		t.Fatalf("RecordCheckBag after migration: %v", err)
	}
}

func bagDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "lego.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestPartBagSizeDefaultsToMain(t *testing.T) {
	db := bagDB(t)
	if size, err := db.PartBagSize("3001"); err != nil || size != BagMain {
		t.Fatalf("PartBagSize with no override = %q, %v, want %q", size, err, BagMain)
	}

	if err := db.SetBagSize("3001", BagSmall); err != nil {
		t.Fatal(err)
	}
	if size, err := db.PartBagSize("3001"); err != nil || size != BagSmall {
		t.Fatalf("PartBagSize after SetBagSize = %q, %v, want %q", size, err, BagSmall)
	}

	// Switching it back explicitly also works (not just clearing).
	if err := db.SetBagSize("3001", BagMain); err != nil {
		t.Fatal(err)
	}
	if size, err := db.PartBagSize("3001"); err != nil || size != BagMain {
		t.Fatalf("PartBagSize after switching back = %q, %v, want %q", size, err, BagMain)
	}

	if err := db.SetBagSize("3001", "huge"); err == nil {
		t.Error("an invalid size must be rejected")
	}
}

func TestRecordCheckBagRoundTrip(t *testing.T) {
	db := bagDB(t)
	if code, err := db.CheckBagCode(1); err != nil || code != "" {
		t.Fatalf("no bag recorded yet: %q, %v, want \"\"", code, err)
	}
	if err := db.RecordCheckBag(1, "", "dave"); err == nil {
		t.Error("an empty bag code must be rejected")
	}
	if err := db.RecordCheckBag(1, "BAG-0042", "dave"); err != nil {
		t.Fatal(err)
	}
	if code, err := db.CheckBagCode(1); err != nil || code != "BAG-0042" {
		t.Fatalf("CheckBagCode = %q, %v, want BAG-0042", code, err)
	}
	// A mis-scan corrected: upsert, not a duplicate-key error.
	if err := db.RecordCheckBag(1, "BAG-0043", "dave"); err != nil {
		t.Fatal(err)
	}
	if code, _ := db.CheckBagCode(1); code != "BAG-0043" {
		t.Fatalf("expected the corrected code to replace the first, got %q", code)
	}
	// A different check's bag is independent.
	if code, err := db.CheckBagCode(2); err != nil || code != "" {
		t.Fatalf("check 2 has no bag recorded: %q, %v, want \"\"", code, err)
	}
}

// TestSmallBagPropagatesThroughNewCheckAndRecount confirms the end-to-end
// wiring: a part marked small-bag shows up that way on a fresh check, and
// still does after a finish + recount reload (mirrors
// TestStickersDefaultToOptionalAndDontCountAsMissing's Optional coverage).
func TestSmallBagPropagatesThroughNewCheckAndRecount(t *testing.T) {
	d := buildDB(t)
	if err := d.SetBagSize("3023", BagSmall); err != nil {
		t.Fatal(err)
	}

	c, err := d.NewCheck(context.Background(), nil, "1-1", CheckIntake, "kc")
	if err != nil {
		t.Fatal(err)
	}
	var found3001, found3023 bool
	for _, l := range c.Lines {
		switch l.PartNum {
		case "3001":
			found3001 = true
			if l.SmallBag {
				t.Error("3001 has no override: should not be flagged small-bag")
			}
		case "3023":
			found3023 = true
			if !l.SmallBag {
				t.Error("3023 was explicitly set to the small bag")
			}
			l.Have = l.Need
		}
	}
	if !found3001 || !found3023 {
		t.Fatalf("expected both parts in the check: %+v", c.Lines)
	}
	if got := SmallBagLines(c); len(got) != 1 || got[0].PartNum != "3023" {
		t.Errorf("SmallBagLines = %+v, want just 3023", got)
	}

	if _, err := d.FinishCheck(c); err != nil {
		t.Fatal(err)
	}
	rc, err := d.NewCheck(context.Background(), nil, "1-1", CheckRecount, "kc")
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range rc.Lines {
		if l.PartNum == "3023" && !l.SmallBag {
			t.Error("the small-bag flag should still apply after a recount reload")
		}
	}
}

func TestSmallBagLinesFiltersToOnlyFlaggedLines(t *testing.T) {
	c := &SetCheck{Lines: []CheckLine{
		{PartNum: "3001", SmallBag: false},
		{PartNum: "3024", SmallBag: true},
		{PartNum: "973", SmallBag: true},
	}}
	got := SmallBagLines(c)
	if len(got) != 2 || got[0].PartNum != "3024" || got[1].PartNum != "973" {
		t.Errorf("SmallBagLines = %+v, want just 3024 and 973", got)
	}
}
