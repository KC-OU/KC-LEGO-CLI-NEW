package lego

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

// enableClockInGate isolates WMS_SETTINGS_FILE (the Get()'s override-file
// precedence, see other packages' same pattern) and turns on the gate —
// off by default, so every other test in this file (and RequireClockedIn's
// own callers elsewhere) sees today's unchanged, ungated behaviour.
func enableClockInGate(t *testing.T) {
	t.Helper()
	t.Setenv(config.SettingsFile, filepath.Join(t.TempDir(), "settings.json"))
	t.Setenv(config.RequireClockIn, "1")
}

func attDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "lego.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestClockInOutRoundTrip(t *testing.T) {
	db := attDB(t)

	if in, err := db.IsClockedIn("dave"); err != nil || in {
		t.Fatalf("dave shouldn't be clocked in yet: %v, %v", in, err)
	}
	if err := db.ClockOut("dave"); !errors.Is(err, ErrNotClockedIn) {
		t.Errorf("clocking out with no open shift = %v, want ErrNotClockedIn", err)
	}

	ev, err := db.ClockIn("dave")
	if err != nil {
		t.Fatal(err)
	}
	if !ev.Open() {
		t.Error("a freshly clocked-in event should be open")
	}
	if in, err := db.IsClockedIn("dave"); err != nil || !in {
		t.Fatalf("dave should be clocked in now: %v, %v", in, err)
	}
	if _, err := db.ClockIn("dave"); !errors.Is(err, ErrAlreadyClockedIn) {
		t.Errorf("clocking in twice = %v, want ErrAlreadyClockedIn", err)
	}

	cur, err := db.CurrentShift("dave")
	if err != nil || cur == nil || cur.ID != ev.ID {
		t.Fatalf("CurrentShift = %+v, %v, want the open event", cur, err)
	}

	if err := db.ClockOut("dave"); err != nil {
		t.Fatal(err)
	}
	if in, err := db.IsClockedIn("dave"); err != nil || in {
		t.Fatalf("dave should be clocked out now: %v, %v", in, err)
	}
	if err := db.ClockOut("dave"); !errors.Is(err, ErrNotClockedIn) {
		t.Errorf("clocking out again = %v, want ErrNotClockedIn", err)
	}
	if cur, err := db.CurrentShift("dave"); err != nil || cur != nil {
		t.Fatalf("CurrentShift after clock-out = %+v, %v, want nil", cur, err)
	}

	// Clocking in again starts a brand new event.
	ev2, err := db.ClockIn("dave")
	if err != nil {
		t.Fatal(err)
	}
	if ev2.ID == ev.ID {
		t.Error("a second clock-in should be a new row, not reopen the old one")
	}
}

func TestClockOutOnlyEverClosesTheCallersOwnOpenShift(t *testing.T) {
	db := attDB(t)
	if _, err := db.ClockIn("dave"); err != nil {
		t.Fatal(err)
	}
	if err := db.ClockOut("sam"); !errors.Is(err, ErrNotClockedIn) {
		t.Errorf("sam clocking out with no shift of their own = %v, want ErrNotClockedIn", err)
	}
	if in, err := db.IsClockedIn("dave"); err != nil || !in {
		t.Fatalf("dave's shift must be untouched by sam's failed clock-out: %v, %v", in, err)
	}
}

func TestRotaSetClearAndIsScheduled(t *testing.T) {
	db := attDB(t)

	if sched, err := db.IsScheduled("dave", "2026-10-10"); err != nil || sched {
		t.Fatalf("no rota entry yet: IsScheduled = %v, %v", sched, err)
	}
	if e, err := db.GetRota("dave", "2026-10-10"); err != nil || e != nil {
		t.Fatalf("GetRota with nothing set = %+v, %v, want nil", e, err)
	}

	if err := db.SetRota("dave", "2026-10-10", "09:00", "17:00", "", "admin"); err != nil {
		t.Fatal(err)
	}
	if sched, err := db.IsScheduled("dave", "2026-10-10"); err != nil || !sched {
		t.Fatalf("IsScheduled after SetRota = %v, %v, want true", sched, err)
	}
	e, err := db.GetRota("dave", "2026-10-10")
	if err != nil || e == nil || e.StartTime != "09:00" || e.EndTime != "17:00" || e.EmergencyOverride {
		t.Fatalf("GetRota = %+v, %v", e, err)
	}

	// Upsert: setting it again for the same day replaces, not duplicates.
	if err := db.SetRota("dave", "2026-10-10", "10:00", "18:00", "covering lates", "admin"); err != nil {
		t.Fatal(err)
	}
	e, _ = db.GetRota("dave", "2026-10-10")
	if e.StartTime != "10:00" || e.Note != "covering lates" {
		t.Fatalf("expected the second SetRota to replace the first, got %+v", e)
	}

	if err := db.ClearRota("dave", "2026-10-10"); err != nil {
		t.Fatal(err)
	}
	if sched, err := db.IsScheduled("dave", "2026-10-10"); err != nil || sched {
		t.Fatalf("IsScheduled after ClearRota = %v, %v, want false", sched, err)
	}
}

func TestQuickNSOverrideSchedulesWithoutClobberingARealShift(t *testing.T) {
	db := attDB(t)

	// No rota entry yet: the override creates one, flagged as emergency.
	if err := db.QuickNSOverride("dave", "2026-10-11", "admin"); err != nil {
		t.Fatal(err)
	}
	e, err := db.GetRota("dave", "2026-10-11")
	if err != nil || e == nil || !e.EmergencyOverride {
		t.Fatalf("GetRota after QuickNSOverride = %+v, %v, want EmergencyOverride=true", e, err)
	}
	if sched, _ := db.IsScheduled("dave", "2026-10-11"); !sched {
		t.Error("an emergency override must count as scheduled")
	}

	// A real planned shift already on the rota is left alone by a later override call.
	if err := db.SetRota("sam", "2026-10-12", "08:00", "16:00", "", "admin"); err != nil {
		t.Fatal(err)
	}
	if err := db.QuickNSOverride("sam", "2026-10-12", "admin"); err != nil {
		t.Fatal(err)
	}
	e, _ = db.GetRota("sam", "2026-10-12")
	if e.EmergencyOverride || e.StartTime != "08:00" {
		t.Errorf("a quick override must not overwrite an existing planned shift, got %+v", e)
	}
}

func TestRotaForDateListsEveryoneScheduledThatDay(t *testing.T) {
	db := attDB(t)
	if err := db.SetRota("dave", "2026-10-10", "09:00", "17:00", "", "admin"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetRota("sam", "2026-10-10", "08:00", "16:00", "", "admin"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetRota("ann", "2026-10-11", "09:00", "17:00", "", "admin"); err != nil {
		t.Fatal(err)
	}

	entries, err := db.RotaForDate("2026-10-10")
	if err != nil || len(entries) != 2 {
		t.Fatalf("RotaForDate = %d entries, %v, want 2", len(entries), err)
	}
	if entries[0].Username != "sam" || entries[1].Username != "dave" {
		t.Errorf("expected sam (08:00) before dave (09:00), got %s then %s", entries[0].Username, entries[1].Username)
	}
}

func TestRequireClockedInCoversBothGates(t *testing.T) {
	enableClockInGate(t)
	db := attDB(t)

	if err := db.RequireClockedIn("dave"); !errors.Is(err, ErrNotClockedIn) {
		t.Errorf("not clocked in at all = %v, want ErrNotClockedIn", err)
	}

	if _, err := db.ClockIn("dave"); err != nil {
		t.Fatal(err)
	}
	if err := db.RequireClockedIn("dave"); !errors.Is(err, ErrNotScheduled) {
		t.Errorf("clocked in but not on today's rota = %v, want ErrNotScheduled", err)
	}

	if err := db.SetRota("dave", today(), "09:00", "17:00", "", "admin"); err != nil {
		t.Fatal(err)
	}
	if err := db.RequireClockedIn("dave"); err != nil {
		t.Errorf("clocked in and scheduled today = %v, want nil", err)
	}
}

// TestMigrationAddsRotaAndClockTables confirms a pre-v3 lego.db (no
// rota_entries/clock_events at all) picks them up on open, same pattern as
// TestMigrationAddsImageURLToSetState for v1 -> v2 (internal/lego/parts_test.go).
func TestMigrationAddsRotaAndClockTables(t *testing.T) {
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
		PRAGMA user_version = 2`)
	raw.Close()
	if err != nil {
		t.Fatal(err)
	}

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open (migrating v2 -> v3): %v", err)
	}
	defer db.Close()

	var version int
	_ = db.QueryRow("PRAGMA user_version").Scan(&version)
	if version != schemaVersion {
		t.Errorf("user_version = %d, want %d", version, schemaVersion)
	}
	if _, err := db.ClockIn("dave"); err != nil {
		t.Fatalf("ClockIn after migration: %v", err)
	}
	if err := db.SetRota("dave", "2026-10-10", "09:00", "17:00", "", "admin"); err != nil {
		t.Fatalf("SetRota after migration: %v", err)
	}
}

func TestRequireClockedInAllowsAQuickNSOverride(t *testing.T) {
	enableClockInGate(t)
	db := attDB(t)
	if _, err := db.ClockIn("dave"); err != nil {
		t.Fatal(err)
	}
	if err := db.RequireClockedIn("dave"); !errors.Is(err, ErrNotScheduled) {
		t.Fatalf("before the override = %v, want ErrNotScheduled", err)
	}
	if err := db.QuickNSOverride("dave", today(), "admin"); err != nil {
		t.Fatal(err)
	}
	if err := db.RequireClockedIn("dave"); err != nil {
		t.Errorf("after the override = %v, want nil", err)
	}
}
