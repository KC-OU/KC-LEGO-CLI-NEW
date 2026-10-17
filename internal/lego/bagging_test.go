package lego

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

// TestMigrationAddsBaggingTables confirms a pre-v5 lego.db (rota/clock
// tables already present, no part_bag_size/check_bags/bag_kinds) picks them
// all up, all the way to the current schema.
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
		CREATE TABLE set_checks (id INTEGER PRIMARY KEY AUTOINCREMENT, set_num TEXT NOT NULL, kind TEXT NOT NULL, status TEXT NOT NULL,
			checked_by TEXT NOT NULL DEFAULT '', started_at TEXT NOT NULL, finished_at TEXT NOT NULL DEFAULT '',
			lines INTEGER NOT NULL DEFAULT 0, pieces INTEGER NOT NULL DEFAULT 0, have INTEGER NOT NULL DEFAULT 0,
			missing INTEGER NOT NULL DEFAULT 0, extra INTEGER NOT NULL DEFAULT 0);
		PRAGMA user_version = 4`)
	raw.Close()
	if err != nil {
		t.Fatal(err)
	}

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open (migrating v4 -> v%d): %v", schemaVersion, err)
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
	if _, err := db.EnsureCheckBag(1, "75192-1", BagSmall, "dave"); err != nil {
		t.Fatalf("EnsureCheckBag after migration: %v", err)
	}
}

// TestMigrationRebuildsAnExistingSingleBagPerCheck confirms a v8-shaped
// check_bags row (one per check, the pre-this-feature schema) survives the
// v9 rebuild as that check's small-bag instance, already scanned (it was
// already confirmed, under the old meaning) — no data silently dropped.
func TestMigrationRebuildsAnExistingSingleBagPerCheck(t *testing.T) {
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
		CREATE TABLE check_bags (check_id INTEGER PRIMARY KEY, bag_code TEXT NOT NULL, created_by TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL);
		INSERT INTO check_bags (check_id, bag_code, created_by, created_at) VALUES (42, 'BAG-0042', 'dave', '2026-01-01T00:00:00');
		PRAGMA user_version = 5`)
	raw.Close()
	if err != nil {
		t.Fatal(err)
	}

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open (migrating v5 -> v%d): %v", schemaVersion, err)
	}
	defer db.Close()

	bags, err := db.CheckBags(42)
	if err != nil {
		t.Fatal(err)
	}
	if len(bags) != 1 {
		t.Fatalf("CheckBags(42) = %+v, want exactly the migrated row", bags)
	}
	b := bags[0]
	if b.Code != "BAG-0042" || b.BagKindKey != BagSmall || b.InstanceLabel != "A" || b.ScannedAt.IsZero() {
		t.Errorf("migrated bag = %+v, want code BAG-0042, kind small, label A, already scanned", b)
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
		t.Error("an unregistered bag kind must be rejected")
	}
	if err := db.SetBagSize("3001", BagExtra); err == nil {
		t.Error("a part can't be assigned the extra bag directly")
	}
}

func TestAddBagKindThenUseItForAPart(t *testing.T) {
	db := bagDB(t)
	if _, err := db.AddBagKind("wheels", "Wheels", "dave"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetBagSize("3001", "wheels"); err != nil {
		t.Fatalf("a newly registered kind should be usable: %v", err)
	}
	if size, _ := db.PartBagSize("3001"); size != "wheels" {
		t.Errorf("PartBagSize = %q, want wheels", size)
	}
}

func TestEnsureCheckBagIsIdempotentAndCodesAreDeterministic(t *testing.T) {
	db := bagDB(t)
	first, err := db.EnsureCheckBag(7, "75192-1", BagSmall, "dave")
	if err != nil {
		t.Fatal(err)
	}
	if first.Code != "75192-1-A" {
		t.Errorf("code = %q, want 75192-1-A", first.Code)
	}
	if first.ScannedAt.IsZero() == false {
		t.Error("EnsureCheckBag registers a bag, it doesn't scan it in")
	}
	second, err := db.EnsureCheckBag(7, "75192-1", BagSmall, "dave")
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID {
		t.Errorf("a second EnsureCheckBag call should return the same instance, got %+v vs %+v", second, first)
	}

	main, err := db.EnsureCheckBag(7, "75192-1", BagMain, "dave")
	if err != nil {
		t.Fatal(err)
	}
	if main.Code != "75192-1-B" {
		t.Errorf("main bag code = %q, want 75192-1-B", main.Code)
	}
	extra, err := db.EnsureCheckBag(7, "75192-1", BagExtra, "dave")
	if err != nil {
		t.Fatal(err)
	}
	if extra.Code != "75192-1-E" {
		t.Errorf("extra bag code = %q, want 75192-1-E (E is reserved, not just the next letter)", extra.Code)
	}
}

func TestScanBagFindsRegistersAndScansIn(t *testing.T) {
	db := bagDB(t)
	bag, err := db.EnsureCheckBag(7, "75192-1", BagSmall, "dave")
	if err != nil {
		t.Fatal(err)
	}
	if !bag.ScannedAt.IsZero() {
		t.Fatal("not scanned yet")
	}

	res, err := db.ScanBag(7, "75192-1", bag.Code)
	if err != nil {
		t.Fatal(err)
	}
	if res.Bag == nil || res.Bag.ScannedAt.IsZero() {
		t.Fatalf("ScanBag result = %+v, want a scanned bag", res)
	}

	// A fresh code for the main bag, never registered before: ScanBag
	// registers it on the fly, same check.
	res2, err := db.ScanBag(7, "75192-1", "75192-1-B")
	if err != nil {
		t.Fatal(err)
	}
	if res2.Bag == nil || res2.Bag.BagKindKey != BagMain {
		t.Fatalf("ScanBag for an unregistered-but-valid code = %+v, want a new main-bag instance", res2)
	}
}

func TestScanBagRecognisesACodeForADifferentSet(t *testing.T) {
	db := bagDB(t)
	// Set 75192's small bag already exists and belongs to check 7.
	if _, err := db.EnsureCheckBag(7, "75192-1", BagSmall, "dave"); err != nil {
		t.Fatal(err)
	}
	// Checking a DIFFERENT set (10696) and scanning 75192's bag by mistake.
	res, err := db.ScanBag(99, "10696-1", "75192-1-A")
	if err != nil {
		t.Fatal(err)
	}
	if res.OtherSet != "75192-1" {
		t.Fatalf("ScanBag across sets = %+v, want OtherSet 75192-1", res)
	}

	// A code that merely LOOKS like it's for a different set (never
	// registered at all) is still recognised from its own shape alone.
	res2, err := db.ScanBag(99, "10696-1", "75192-1-A2")
	if err != nil {
		t.Fatal(err)
	}
	if res2.OtherSet != "75192-1" {
		t.Fatalf("ScanBag for an unregistered but differently-set-prefixed code = %+v, want OtherSet 75192-1", res2)
	}
}

func TestScanBagRejectsGarbage(t *testing.T) {
	db := bagDB(t)
	for _, code := range []string{"", "not-a-real-code-at-all-zzz", "75192-1-Z9"} {
		res, err := db.ScanBag(7, "75192-1", code)
		if err != nil {
			t.Fatal(err)
		}
		if !res.Unknown {
			t.Errorf("ScanBag(%q) = %+v, want Unknown", code, res)
		}
	}
}

func TestAddBagInstanceEnforcesMaxBagsAndRaisingItUnblocks(t *testing.T) {
	db := bagDB(t)
	if _, err := db.EnsureCheckBag(7, "75192-1", BagSmall, "dave"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.EnsureCheckBag(7, "75192-1", BagMain, "dave"); err != nil {
		t.Fatal(err)
	}
	// Default max_bags is 2, and both slots are taken.
	if _, err := db.AddBagInstance(7, "75192-1", BagSmall, "dave"); err != ErrBagCapReached {
		t.Fatalf("AddBagInstance at the cap = %v, want ErrBagCapReached", err)
	}
	if err := db.SetBagPolicy("75192-1", 3, ""); err != nil {
		t.Fatal(err)
	}
	second, err := db.AddBagInstance(7, "75192-1", BagSmall, "dave")
	if err != nil {
		t.Fatalf("AddBagInstance after raising max_bags: %v", err)
	}
	if second.InstanceLabel != "A2" {
		t.Errorf("second small-bag label = %q, want A2", second.InstanceLabel)
	}
}

func TestRequiredBagKindsOnlyIncludesExtraWhenSomethingWasActuallyFoundExtra(t *testing.T) {
	d := buildDB(t)
	if err := d.SetBagSize("3023", BagSmall); err != nil {
		t.Fatal(err)
	}
	c, err := d.NewCheck(context.Background(), nil, "1-1", CheckIntake, "kc")
	if err != nil {
		t.Fatal(err)
	}
	required, err := d.RequiredBagKinds(c)
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]bool{}
	for _, k := range required {
		keys[k.Key] = true
	}
	if !keys[BagSmall] || !keys[BagMain] || keys[BagExtra] {
		t.Fatalf("required kinds before any extra = %+v, want small+main only", required)
	}

	for i := range c.Lines {
		if c.Lines[i].PartNum == "3001" {
			c.Lines[i].Extra = 2
		}
	}
	required, err = d.RequiredBagKinds(c)
	if err != nil {
		t.Fatal(err)
	}
	keys = map[string]bool{}
	for _, k := range required {
		keys[k.Key] = true
	}
	if !keys[BagExtra] {
		t.Fatalf("required kinds after marking extra = %+v, want extra included", required)
	}
}

func TestUnscannedRequiredBagsNarrowsAsBagsAreScanned(t *testing.T) {
	d := buildDB(t)
	if err := d.SetBagSize("3023", BagSmall); err != nil {
		t.Fatal(err)
	}
	c, err := d.NewCheck(context.Background(), nil, "1-1", CheckIntake, "kc")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SaveCheck(c); err != nil {
		t.Fatal(err)
	}

	missing, err := d.UnscannedRequiredBags(c)
	if err != nil || len(missing) != 2 {
		t.Fatalf("UnscannedRequiredBags before any scan = %+v, err %v, want 2 (small+main)", missing, err)
	}

	if _, err := d.ScanBag(c.ID, c.SetNum, BagCode(c.SetNum, BagSmall)); err != nil {
		t.Fatal(err)
	}
	missing, err = d.UnscannedRequiredBags(c)
	if err != nil || len(missing) != 1 || missing[0].Key != BagMain {
		t.Fatalf("UnscannedRequiredBags after scanning small = %+v, err %v, want just main", missing, err)
	}

	if _, err := d.ScanBag(c.ID, c.SetNum, BagCode(c.SetNum, BagMain)); err != nil {
		t.Fatal(err)
	}
	missing, err = d.UnscannedRequiredBags(c)
	if err != nil || len(missing) != 0 {
		t.Fatalf("UnscannedRequiredBags after scanning both = %+v, err %v, want none left", missing, err)
	}
}

// TestAssignPoolBagScansLikeAnyOtherBagAndIsIdempotent confirms a reusable
// pool bag needs no special-casing anywhere else: once assigned, it's just
// another check_bags row with that exact code, so ScanBag/finish-gate logic
// treats it identically to a fresh one-off code.
func TestAssignPoolBagScansLikeAnyOtherBagAndIsIdempotent(t *testing.T) {
	db := bagDB(t)
	if _, err := db.AddPoolBag("BIN-07", BagSmall, "admin"); err != nil {
		t.Fatal(err)
	}
	b, err := db.AssignPoolBag(7, "admin", "BIN-07")
	if err != nil {
		t.Fatal(err)
	}
	if b.Code != "BIN-07" || b.BagKindKey != BagSmall {
		t.Fatalf("AssignPoolBag = %+v, want code BIN-07, kind small", b)
	}
	pb, err := db.PoolBagByCode("BIN-07")
	if err != nil || pb.Status != "assigned" || pb.CurrentCheckID != 7 {
		t.Fatalf("PoolBagByCode after assign = %+v, %v, want assigned to check 7", pb, err)
	}

	// Idempotent for the same check.
	again, err := db.AssignPoolBag(7, "admin", "BIN-07")
	if err != nil || again.ID != b.ID {
		t.Fatalf("second AssignPoolBag for the same check = %+v, %v, want the same row", again, err)
	}

	// A different check can't take it while it's out.
	if _, err := db.AssignPoolBag(8, "admin", "BIN-07"); err != ErrPoolBagUnavailable {
		t.Fatalf("AssignPoolBag for a different check while assigned = %v, want ErrPoolBagUnavailable", err)
	}

	// Scans exactly like any other registered bag -- no ScanBag changes needed.
	res, err := db.ScanBag(7, "75192-1", "BIN-07")
	if err != nil || res.Bag == nil || res.Bag.Code != "BIN-07" || res.Bag.ScannedAt.IsZero() {
		t.Fatalf("ScanBag on a pool-assigned code = %+v, %v, want it scanned in", res, err)
	}
}

// TestFinishCheckFreesItsPoolBagsBackToAvailable confirms a reusable bag is
// only ever "out" for the lifetime of the check that borrowed it.
func TestFinishCheckFreesItsPoolBagsBackToAvailable(t *testing.T) {
	d := buildDB(t)
	if _, err := d.AddPoolBag("BIN-09", BagMain, "admin"); err != nil {
		t.Fatal(err)
	}
	c, err := d.NewCheck(context.Background(), nil, "1-1", CheckIntake, "kc")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SaveCheck(c); err != nil {
		t.Fatal(err)
	}
	if _, err := d.AssignPoolBag(c.ID, "admin", "BIN-09"); err != nil {
		t.Fatal(err)
	}
	if pb, _ := d.PoolBagByCode("BIN-09"); pb.Status != "assigned" {
		t.Fatalf("expected BIN-09 assigned before finish, got %+v", pb)
	}
	for i := range c.Lines {
		c.Lines[i].Have = c.Lines[i].Need
	}
	if _, err := d.FinishCheck(c); err != nil {
		t.Fatal(err)
	}
	pb, err := d.PoolBagByCode("BIN-09")
	if err != nil || pb.Status != "available" || pb.CurrentCheckID != 0 {
		t.Fatalf("PoolBagByCode after finish = %+v, %v, want freed back to available", pb, err)
	}
}

// TestMissingByBagSplitsTheShortfallByEachPartsOwnBagKind confirms the
// completion dashboard/missing-parts report's per-bag breakdown reflects
// each short part's ordinary small/main placement, not a blanket total.
func TestMissingByBagSplitsTheShortfallByEachPartsOwnBagKind(t *testing.T) {
	d := buildDB(t)
	if err := d.SetBagSize("3023", BagSmall); err != nil {
		t.Fatal(err)
	}
	c, err := d.NewCheck(context.Background(), nil, "1-1", CheckIntake, "kc")
	if err != nil {
		t.Fatal(err)
	}
	for i := range c.Lines {
		switch c.Lines[i].PartNum {
		case "3001": // main bag, 60 needed
			c.Lines[i].Have -= 5
		case "3023": // small bag, 40 needed
			c.Lines[i].Have -= 3
		}
	}
	bm, err := d.MissingByBag(c)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, b := range bm {
		got[b.Key] = b.Missing
	}
	if got[BagMain] != 5 || got[BagSmall] != 3 {
		t.Fatalf("MissingByBag = %+v, want main:5 small:3", got)
	}
}

// TestFindBagCoversAPoolBagARegisteredInstanceAndAFreshUnregisteredCode
// exercises every one of FindBag's branches -- the lost-bag lookup's whole
// point is working whether or not the bag was ever actually scanned.
func TestFindBagCoversAPoolBagARegisteredInstanceAndAFreshUnregisteredCode(t *testing.T) {
	d := buildDB(t)

	if r, err := d.FindBag("NO-SUCH-CODE"); err != nil || r.Found {
		t.Fatalf("FindBag for an unknown code = %+v, %v, want not found", r, err)
	}

	// A pool bag, never assigned to anything yet.
	if _, err := d.AddPoolBag("BIN-11", BagMain, "admin"); err != nil {
		t.Fatal(err)
	}
	r, err := d.FindBag("BIN-11")
	if err != nil || !r.Found || !r.Pool || r.PoolStatus != "available" || r.BagKindKey != BagMain {
		t.Fatalf("FindBag on an unassigned pool bag = %+v, %v", r, err)
	}

	// A registered, scanned-in instance -- needs a real set_checks row so
	// FindBag's GetCheck lookup can name the set it belongs to.
	c, err := d.NewCheck(context.Background(), nil, "1-1", CheckIntake, "kc")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SaveCheck(c); err != nil {
		t.Fatal(err)
	}
	bag, err := d.EnsureCheckBag(c.ID, c.SetNum, BagSmall, "dave")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.ScanBag(c.ID, c.SetNum, bag.Code); err != nil {
		t.Fatal(err)
	}
	r, err = d.FindBag(bag.Code)
	if err != nil || !r.Found || r.Pool || r.SetNum != c.SetNum || r.BagKindKey != BagSmall || r.ScannedAt.IsZero() {
		t.Fatalf("FindBag on a scanned-in instance = %+v, %v", r, err)
	}

	// Printed but never scanned into anything -- still resolves from its
	// own "<set>-<label>" shape alone, same parse ScanBag uses.
	r, err = d.FindBag(c.SetNum + "-B")
	if err != nil || !r.Found || r.SetNum != c.SetNum || r.BagKindKey != BagMain || !r.ScannedAt.IsZero() {
		t.Fatalf("FindBag on a fresh unregistered code = %+v, %v", r, err)
	}
}

// TestFindBagIncludesProvenanceAndTheTiedCheckOutcome is the "who made it and
// what mess-ups did it make" admin lookup: FindBag surfaces who registered
// the bag and when, plus the result of the check it's tied to once that
// check is finished -- reusing the check's own existing record (Totals,
// Disputed/DisputeNote) rather than a separate incident log.
func TestFindBagIncludesProvenanceAndTheTiedCheckOutcome(t *testing.T) {
	d := buildDB(t)
	c, err := d.NewCheck(context.Background(), nil, "1-1", CheckIntake, "alex")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SaveCheck(c); err != nil {
		t.Fatal(err)
	}
	bag, err := d.EnsureCheckBag(c.ID, c.SetNum, BagSmall, "dave")
	if err != nil {
		t.Fatal(err)
	}

	// Still in progress: provenance is already there, but no outcome yet.
	r, err := d.FindBag(bag.Code)
	if err != nil || !r.Found || r.CreatedBy != "dave" || r.CreatedAt.IsZero() {
		t.Fatalf("FindBag before finishing = %+v, %v, want dave's provenance", r, err)
	}
	if r.CheckDone || r.Missing != 0 || r.Extra != 0 {
		t.Errorf("an in-progress check should report no outcome yet: %+v", r)
	}

	for i := range c.Lines {
		if c.Lines[i].PartNum == "3001" {
			c.Lines[i].Have -= 2
			c.Lines[i].Extra = 1
		}
	}
	if _, err := d.FinishCheck(c); err != nil {
		t.Fatal(err)
	}
	if err := d.FlagDispute(c.ID, "checker says the shelf count was off"); err != nil {
		t.Fatal(err)
	}

	r, err = d.FindBag(bag.Code)
	if err != nil || !r.Found || !r.CheckDone {
		t.Fatalf("FindBag after finishing = %+v, %v", r, err)
	}
	if r.Missing != 2 || r.Extra != 1 {
		t.Errorf("Missing/Extra = %d/%d, want 2/1", r.Missing, r.Extra)
	}
	if !r.Disputed || r.DisputeNote != "checker says the shelf count was off" {
		t.Errorf("Disputed/DisputeNote = %v/%q", r.Disputed, r.DisputeNote)
	}
}

// TestPrintableBagsCoversEveryKindOfBagWithItsTrueCode is the regression test
// for the reprint bug: a pool-assigned bag's real (admin-chosen) code, a
// second same-kind instance's real "<set>-A2" code, and a custom kind beyond
// small/main/extra must all come back as themselves -- never recomputed via
// BagCode, which is only ever right for a kind's very first instance.
func TestPrintableBagsCoversEveryKindOfBagWithItsTrueCode(t *testing.T) {
	d := buildDB(t)

	// A set never checked at all: just the two deterministic, not-yet-
	// printed codes for small and main.
	bags, err := d.PrintableBags("9999-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(bags) != 2 || bags[0].Code != "9999-1-A" || bags[1].Code != "9999-1-B" {
		t.Fatalf("PrintableBags for a never-checked set = %+v, want just the virtual small+main codes", bags)
	}

	c, err := d.NewCheck(context.Background(), nil, "1-1", CheckIntake, "kc")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SaveCheck(c); err != nil {
		t.Fatal(err)
	}
	if err := d.SetBagPolicy(c.SetNum, 3, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := d.AddBagKind("wheels", "Wheels", "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.AddPoolBag("BIN-07", BagSmall, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.AssignPoolBag(c.ID, "admin", "BIN-07"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.EnsureCheckBag(c.ID, c.SetNum, BagMain, "admin"); err != nil { // "<set>-B", the first instance
		t.Fatal(err)
	}
	if _, err := d.AddBagInstance(c.ID, c.SetNum, BagMain, "admin"); err != nil { // "<set>-B2", the second
		t.Fatal(err)
	}
	if _, err := d.EnsureCheckBag(c.ID, c.SetNum, "wheels", "admin"); err != nil {
		t.Fatal(err)
	}

	bags, err = d.PrintableBags(c.SetNum)
	if err != nil {
		t.Fatal(err)
	}
	byCode := map[string]CheckBag{}
	for _, b := range bags {
		byCode[b.Code] = b
	}
	if len(bags) != 4 { // small (pool), main's 1st instance, main's 2nd instance, wheels -- no duplicate small
		t.Fatalf("PrintableBags = %+v, want 4 distinct bags", bags)
	}
	if b, ok := byCode["BIN-07"]; !ok || b.BagKindKey != BagSmall {
		t.Errorf("the pool-assigned bag should keep its own code, got %+v", byCode)
	}
	if b, ok := byCode[c.SetNum+"-B"]; !ok || b.BagKindKey != BagMain {
		t.Errorf("main's first instance should still be listed, got %+v", byCode)
	}
	if b, ok := byCode[c.SetNum+"-B2"]; !ok || b.BagKindKey != BagMain {
		t.Errorf("the second main instance should show its real \"-B2\" code, not the first instance's, got %+v", byCode)
	}
	if b, ok := byCode[c.SetNum+"-W"]; !ok || b.BagKindKey != "wheels" {
		t.Errorf("a custom bag kind should be included too, got %+v", byCode)
	}
	if _, ok := byCode[c.SetNum+"-A"]; ok {
		t.Errorf("the small bag's deterministic code should NOT also be listed once a real (pool) instance exists: %+v", byCode)
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
