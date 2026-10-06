package lego

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

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

// TestClockInRefusesWithoutBeingOnTheRotaOnceTheGateIsOn is the direct test
// for "refuse clock-in until an admin pre-approves": off by default (gate
// unset), clocking in never checks the rota at all — only once
// config.RequireClockIn is on does an unscheduled clock-in get refused, and
// only a real rota entry or a quick NS override (SetRota/QuickNSOverride —
// IsScheduled covers both) unblocks it. No live "approve this attempt"
// exchange: the admin grants it in advance, same action either way.
func TestClockInRefusesWithoutBeingOnTheRotaOnceTheGateIsOn(t *testing.T) {
	db := attDB(t)

	// Gate off (the default): clock-in is never gated by the rota.
	if _, err := db.ClockIn("dave"); err != nil {
		t.Fatalf("gate off: ClockIn = %v, want nil regardless of the rota", err)
	}
	if err := db.ClockOut("dave"); err != nil {
		t.Fatal(err)
	}

	enableClockInGate(t)
	if _, err := db.ClockIn("sam"); !errors.Is(err, ErrNotScheduled) {
		t.Fatalf("gate on, not scheduled: ClockIn = %v, want ErrNotScheduled", err)
	}
	if in, _ := db.IsClockedIn("sam"); in {
		t.Error("a refused clock-in must not have created an open shift")
	}

	if err := db.SetRota("sam", today(), "09:00", "17:00", "", "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ClockIn("sam"); err != nil {
		t.Fatalf("scheduled: ClockIn = %v, want nil", err)
	}
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

// TestMigrationAddsClockBreaksTable confirms a pre-v4 lego.db (rota_entries/
// clock_events already present, no clock_breaks) picks it up on open.
func TestMigrationAddsClockBreaksTable(t *testing.T) {
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
		CREATE TABLE rota_entries (username TEXT NOT NULL, date TEXT NOT NULL, start_time TEXT NOT NULL DEFAULT '',
			end_time TEXT NOT NULL DEFAULT '', note TEXT NOT NULL DEFAULT '', emergency_override INTEGER NOT NULL DEFAULT 0,
			created_by TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, PRIMARY KEY (username, date));
		CREATE TABLE clock_events (id INTEGER PRIMARY KEY AUTOINCREMENT, username TEXT NOT NULL,
			clock_in_at TEXT NOT NULL, clock_out_at TEXT NOT NULL DEFAULT '');
		PRAGMA user_version = 3`)
	raw.Close()
	if err != nil {
		t.Fatal(err)
	}

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open (migrating v3 -> v4): %v", err)
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
	if err := db.StartBreak("dave"); err != nil {
		t.Fatalf("StartBreak after migration: %v", err)
	}
}

func TestRequireClockedInCoversBothGates(t *testing.T) {
	enableClockInGate(t)
	db := attDB(t)

	if err := db.RequireClockedIn("dave"); !errors.Is(err, ErrNotClockedIn) {
		t.Errorf("not clocked in at all = %v, want ErrNotClockedIn", err)
	}

	// ClockIn itself now also needs today's rota, once the gate is on.
	if _, err := db.ClockIn("dave"); !errors.Is(err, ErrNotScheduled) {
		t.Fatalf("clocking in while unscheduled = %v, want ErrNotScheduled", err)
	}
	if err := db.SetRota("dave", today(), "09:00", "17:00", "", "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ClockIn("dave"); err != nil {
		t.Fatal(err)
	}
	if err := db.RequireClockedIn("dave"); err != nil {
		t.Errorf("clocked in and scheduled today = %v, want nil", err)
	}

	// An admin revoking the schedule after dave's already clocked in still
	// refuses pick/check work — RequireClockedIn checks fresh every time,
	// not just at clock-in.
	if err := db.ClearRota("dave", today()); err != nil {
		t.Fatal(err)
	}
	if err := db.RequireClockedIn("dave"); !errors.Is(err, ErrNotScheduled) {
		t.Errorf("rota cleared after clocking in = %v, want ErrNotScheduled", err)
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

func TestBreakStartEndRoundTrip(t *testing.T) {
	db := attDB(t)

	if err := db.StartBreak("dave"); !errors.Is(err, ErrNotClockedIn) {
		t.Errorf("starting a break with no shift = %v, want ErrNotClockedIn", err)
	}
	if _, err := db.ClockIn("dave"); err != nil {
		t.Fatal(err)
	}
	if on, err := db.OnBreak("dave"); err != nil || on {
		t.Fatalf("not on a break yet: %v, %v", on, err)
	}
	if err := db.EndBreak("dave"); !errors.Is(err, ErrNotOnBreak) {
		t.Errorf("ending a break that was never started = %v, want ErrNotOnBreak", err)
	}

	if err := db.StartBreak("dave"); err != nil {
		t.Fatal(err)
	}
	if on, err := db.OnBreak("dave"); err != nil || !on {
		t.Fatalf("should be on a break now: %v, %v", on, err)
	}
	if err := db.StartBreak("dave"); !errors.Is(err, ErrAlreadyOnBreak) {
		t.Errorf("starting a second break = %v, want ErrAlreadyOnBreak", err)
	}

	if err := db.EndBreak("dave"); err != nil {
		t.Fatal(err)
	}
	if on, err := db.OnBreak("dave"); err != nil || on {
		t.Fatalf("break should be over: %v, %v", on, err)
	}

	// Still clocked in throughout — a break never touches the shift itself.
	if in, err := db.IsClockedIn("dave"); err != nil || !in {
		t.Fatalf("a break must not clock dave out: %v, %v", in, err)
	}
}

func TestRequireClockedInRefusesDuringABreak(t *testing.T) {
	enableClockInGate(t)
	db := attDB(t)
	if err := db.SetRota("dave", today(), "09:00", "17:00", "", "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ClockIn("dave"); err != nil {
		t.Fatal(err)
	}
	if err := db.RequireClockedIn("dave"); err != nil {
		t.Fatalf("before the break: %v, want nil", err)
	}
	if err := db.StartBreak("dave"); err != nil {
		t.Fatal(err)
	}
	if err := db.RequireClockedIn("dave"); !errors.Is(err, ErrOnBreak) {
		t.Errorf("during the break = %v, want ErrOnBreak", err)
	}
	if err := db.EndBreak("dave"); err != nil {
		t.Fatal(err)
	}
	if err := db.RequireClockedIn("dave"); err != nil {
		t.Errorf("after the break = %v, want nil", err)
	}
}

func TestNoShowCandidatesOnlyFlagsLateUnclockedScheduledStarts(t *testing.T) {
	enableClockInGate(t)
	db := attDB(t)

	// Not on the rota at all today: never a no-show candidate.
	if cands, err := db.NoShowCandidates(10); err != nil || len(cands) != 0 {
		t.Fatalf("nobody scheduled: %+v, %v, want none", cands, err)
	}

	past := time.Now().Add(-30 * time.Minute).Format("15:04")
	future := time.Now().Add(30 * time.Minute).Format("15:04")
	if err := db.SetRota("dave", today(), past, "", "", "admin"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetRota("sam", today(), future, "", "", "admin"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetRota("ann", today(), "", "", "", "admin"); err != nil { // no start time: never flagged
		t.Fatal(err)
	}

	cands, err := db.NoShowCandidates(10)
	if err != nil || len(cands) != 1 || cands[0].Username != "dave" {
		t.Fatalf("NoShowCandidates = %+v, %v, want just dave (late, unclocked)", cands, err)
	}

	// Once dave clocks in, he drops off the list.
	if _, err := db.ClockIn("dave"); err != nil {
		t.Fatal(err)
	}
	if cands, err := db.NoShowCandidates(10); err != nil || len(cands) != 0 {
		t.Fatalf("after clocking in: %+v, %v, want none", cands, err)
	}
}

func TestNoShowCandidatesIsOffUntilTheGateIsOn(t *testing.T) {
	db := attDB(t) // gate NOT enabled
	past := time.Now().Add(-30 * time.Minute).Format("15:04")
	if err := db.SetRota("dave", today(), past, "", "", "admin"); err != nil {
		t.Fatal(err)
	}
	if cands, err := db.NoShowCandidates(10); err != nil || len(cands) != 0 {
		t.Fatalf("gate off: %+v, %v, want none regardless of the rota", cands, err)
	}
}

func TestTeamSummaryHoursAndAccuracy(t *testing.T) {
	db := attDB(t)
	date := "2026-10-10"

	// A finished shift that day, and one accuracy event.
	in, _ := time.Parse(time.RFC3339, date+"T09:00:00Z")
	out, _ := time.Parse(time.RFC3339, date+"T13:00:00Z")
	if _, err := db.Exec(`INSERT INTO clock_events (username, clock_in_at, clock_out_at) VALUES (?, ?, ?)`,
		"dave", in.Format(time.RFC3339), out.Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO accuracy_log (username, role, day, kind, target, pieces, missing, delta, created_at)
		VALUES ('dave', ?, ?, 'set_check', '1-1', 10, 2, -3, ?)`, AccuracyChecker, date, in.Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}

	lines, err := db.TeamSummary(date)
	if err != nil || len(lines) != 1 {
		t.Fatalf("TeamSummary = %+v, %v, want 1 line", lines, err)
	}
	l := lines[0]
	if l.Username != "dave" || l.HoursWorked != 4 {
		t.Errorf("expected dave, 4 hours worked, got %+v", l)
	}
	if l.CheckerAccuracy == nil || *l.CheckerAccuracy != 97 {
		t.Errorf("expected checker accuracy 97%%, got %v", l.CheckerAccuracy)
	}
	if l.PickerAccuracy != nil {
		t.Errorf("no picker activity that day: expected nil, got %v", *l.PickerAccuracy)
	}

	if lines, err := db.TeamSummary("2026-10-11"); err != nil || len(lines) != 0 {
		t.Fatalf("a day with no activity: %+v, %v, want none", lines, err)
	}
}

func TestRequireClockedInAllowsAQuickNSOverride(t *testing.T) {
	enableClockInGate(t)
	db := attDB(t)

	// A quick NS override now unlocks clocking in itself, not just the
	// later pick/check gate — dave can't even clock in before it's granted.
	if _, err := db.ClockIn("dave"); !errors.Is(err, ErrNotScheduled) {
		t.Fatalf("clocking in before the override = %v, want ErrNotScheduled", err)
	}
	if err := db.QuickNSOverride("dave", today(), "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ClockIn("dave"); err != nil {
		t.Fatal(err)
	}
	if err := db.RequireClockedIn("dave"); err != nil {
		t.Errorf("after the override = %v, want nil", err)
	}
}
