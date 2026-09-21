package lego

import (
	"database/sql"
	"path/filepath"
	"sync"
	"testing"
)

func TestOwnedPartsAreKeyedByPartAndColour(t *testing.T) {
	db := newFixture(t)
	add := func(colorID int, colorName string, qty int) {
		t.Helper()
		if err := db.AddOwnedPart(OwnedPart{PartNum: "3001", Name: "Brick 2 x 4", Category: "Bricks", ColorID: colorID, ColorName: colorName, Qty: qty}); err != nil {
			t.Fatalf("AddOwnedPart: %v", err)
		}
	}
	add(4, "Red", 10)
	add(1, "Blue", 5)
	if rows, _ := db.ListOwnedParts(); len(rows) != 2 {
		t.Fatalf("a part in two colours is two rows, got %d", len(rows))
	}

	add(4, "Red", 25) // same colour again: the total you hold, updated in place
	red, _ := db.GetOwnedPart("3001", 4, "Red")
	if red == nil || red.Qty != 25 {
		t.Fatalf("red should be updated to 25, got %+v", red)
	}
	add(4, "Bright Red", 30) // a known colour is matched by id, whatever it is called
	if rows, _ := db.ListOwnedParts(); len(rows) != 2 {
		t.Fatalf("a renamed known colour must not create a third row, got %d", len(rows))
	}

	// Free-text colours (id -1) are told apart by name; unknown (no name) is its own row.
	add(NoColor, "Glow", 2)
	add(NoColor, "Teal", 3)
	add(NoColor, "", 4)
	add(NoColor, "Glow", 9)
	if rows, _ := db.ListOwnedParts(); len(rows) != 5 {
		t.Fatalf("expected red, blue, glow, teal, unknown = 5 rows, got %d", len(rows))
	}
	if g, _ := db.GetOwnedPart("3001", NoColor, "Glow"); g == nil || g.Qty != 9 {
		t.Errorf("Glow should have been updated to 9, got %+v", g)
	}
	if all, _ := db.OwnedPartsOf("3001"); len(all) != 5 {
		t.Errorf("OwnedPartsOf should list every colour, got %d", len(all))
	}
	if none, _ := db.GetOwnedPart("3001", 99, ""); none != nil {
		t.Error("a colour you don't hold must come back nil")
	}

	if err := db.SetSyncedPartID(red.ID, 42); err != nil {
		t.Fatal(err)
	}
	red, _ = db.GetOwnedPart("3001", 4, "")
	if red.SyncedPartID != 42 {
		t.Errorf("SyncedPartID = %d", red.SyncedPartID)
	}
	add(4, "Red", 26) // updating the quantity must keep the Part-DB link
	if red, _ = db.GetOwnedPart("3001", 4, ""); red.SyncedPartID != 42 {
		t.Errorf("an update must not forget the Part-DB link, got %d", red.SyncedPartID)
	}
}

// legacyDB writes a lego.db as the previous version created it: owned_parts
// with UNIQUE(part_num), no colour, user_version 0.
func legacyDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "lego.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	_, err = raw.Exec(`CREATE TABLE owned_parts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		part_num TEXT NOT NULL UNIQUE,
		name TEXT NOT NULL DEFAULT '',
		category TEXT NOT NULL DEFAULT '',
		qty INTEGER NOT NULL DEFAULT 0,
		synced_part_id INTEGER NOT NULL DEFAULT 0,
		updated_at TEXT NOT NULL
	);
	INSERT INTO owned_parts (part_num, name, category, qty, synced_part_id, updated_at) VALUES ('3001', 'Brick 2x4', 'Bricks', 7, 42, '2026-01-01T00:00:00.000000')`)
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestMigrationKeepsExistingRowsAndAddsColour(t *testing.T) {
	path := legacyDB(t)
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open (migrating): %v", err)
	}
	defer db.Close()

	var version int
	_ = db.QueryRow("PRAGMA user_version").Scan(&version)
	if version != schemaVersion {
		t.Errorf("user_version = %d, want %d", version, schemaVersion)
	}
	old, _ := db.GetOwnedPart("3001", NoColor, "")
	if old == nil || old.Qty != 7 || old.SyncedPartID != 42 || old.Name != "Brick 2x4" {
		t.Fatalf("the existing row must survive as an unknown-colour row: %+v", old)
	}
	// The old UNIQUE(part_num) is gone: a second colour of the same part now fits.
	if err := db.AddOwnedPart(OwnedPart{PartNum: "3001", Name: "Brick 2x4", ColorID: 4, ColorName: "Red", Qty: 1}); err != nil {
		t.Fatalf("second colour after migration: %v", err)
	}

	db.Close()
	again, err := Open(path) // a second open is a no-op
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if rows, _ := again.ListOwnedParts(); len(rows) != 2 {
		t.Errorf("reopening must not change the data, got %d rows", len(rows))
	}
}

// Every telnet/web session is its own process opening the file: several may
// try to migrate at once, and exactly one may do the rebuild.
func TestConcurrentOpensMigrateSafely(t *testing.T) {
	path := legacyDB(t)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			db, err := Open(path)
			if err != nil {
				errs <- err
				return
			}
			db.Close()
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent Open failed: %v", err)
	}
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if rows, _ := db.ListOwnedParts(); len(rows) != 1 || rows[0].Qty != 7 {
		t.Fatalf("data after concurrent migration: %+v", rows)
	}
}
