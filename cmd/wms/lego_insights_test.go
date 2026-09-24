package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

// legoEnv points the CLI at scratch files and returns the open scratch store.
func legoEnv(t *testing.T) *lego.DB {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("LEGO_DB_PATH", filepath.Join(dir, "lego.db"))
	t.Setenv("WMS_SETTINGS_FILE", filepath.Join(dir, "settings.json"))
	t.Setenv("REBRICKABLE_API_KEY", "")
	t.Setenv("REBRICKABLE_BASE_URL", "http://127.0.0.1:1") // nothing may reach the real API from a test
	db, err := lego.Open("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func seedCatalog(t *testing.T, db *lego.DB) {
	t.Helper()
	for _, q := range []string{
		`INSERT INTO cat_categories (id, name) VALUES (11, 'Bricks'), (14, 'Plates')`,
		`INSERT INTO cat_parts (part_num, name, part_cat_id) VALUES ('3001', 'Brick 2 x 4', 11), ('3023', 'Plate 1 x 2', 14)`,
		`INSERT INTO cat_colors (id, name, rgb, is_trans) VALUES (4, 'Red', 'C91A09', 0), (1, 'Blue', '0055BF', 0)`,
		`INSERT INTO cat_elements (part_num, color_id) VALUES ('3001', 4), ('3001', 1), ('3023', 1)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLowStockRoundTripThroughTheCLI(t *testing.T) {
	db := legoEnv(t)
	_ = db.AddOwnedPart(lego.OwnedPart{PartNum: "3001", Name: "Brick 2 x 4", Category: "Bricks", ColorID: 4, ColorName: "Red", Qty: 3})
	_ = db.AddOwnedPart(lego.OwnedPart{PartNum: "3001", Name: "Brick 2 x 4", Category: "Bricks", ColorID: 1, ColorName: "Blue", Qty: 50})

	if _, code := run(t, "lego", "set-min", "3001", "--min", "5"); code != exitUsage {
		t.Errorf("two colours held and no --color must be a usage error, got %d", code)
	}
	if _, code := run(t, "lego", "set-min", "3001", "--color", "red"); code != exitUsage {
		t.Errorf("--min is required, got %d", code)
	}
	if _, code := run(t, "lego", "set-min", "9999", "--min", "1"); code != exitNotFound {
		t.Errorf("an unheld part = %d, want %d", code, exitNotFound)
	}
	if stdout, code := run(t, "lego", "set-min", "3001", "--color", "red", "--min", "5"); code != 0 || !strings.Contains(stdout, "warn below 5") {
		t.Fatalf("set-min: code=%d out=%q", code, stdout)
	}

	stdout, code := run(t, "lego", "low", "--json", "--fail")
	var doc struct {
		Low []map[string]any `json:"low"`
	}
	// stdout and the JSON error on stderr share the capture: read just the first document.
	if code != exitFailure || json.NewDecoder(strings.NewReader(stdout)).Decode(&doc) != nil || len(doc.Low) != 1 || doc.Low[0]["short"] != float64(2) {
		t.Fatalf("low --fail: code=%d out=%q", code, stdout)
	}
	_ = db.AddOwnedPart(lego.OwnedPart{PartNum: "3001", Name: "Brick 2 x 4", Category: "Bricks", ColorID: 4, ColorName: "Red", Qty: 9})
	if _, code := run(t, "lego", "low", "--fail", "--quiet"); code != 0 {
		t.Errorf("nothing low = exit %d", code)
	}
	if stdout, _ := run(t, "lego", "stats", "--json"); !strings.Contains(stdout, `"LoosePieces": 59`) {
		t.Errorf("stats --json: %s", stdout)
	}
}

func TestOptionalTogglesAndReportsState(t *testing.T) {
	db := legoEnv(t)
	seedCatalog(t, db)
	if stdout, code := run(t, "lego", "optional", "3001"); code != 0 || !strings.Contains(stdout, "currently required") {
		t.Fatalf("default state: code=%d out=%q", code, stdout)
	}
	if stdout, code := run(t, "lego", "optional", "3001", "on"); code != 0 || !strings.Contains(stdout, "marked optional") {
		t.Fatalf("on: code=%d out=%q", code, stdout)
	}
	if stdout, code := run(t, "lego", "optional", "3001"); code != 0 || !strings.Contains(stdout, "currently optional") {
		t.Fatalf("after on: code=%d out=%q", code, stdout)
	}
	if stdout, code := run(t, "lego", "optional", "3001", "off"); code != 0 || !strings.Contains(stdout, "marked required") {
		t.Fatalf("off: code=%d out=%q", code, stdout)
	}
	if _, code := run(t, "lego", "optional", "3001", "sideways"); code != exitUsage {
		t.Errorf("a bad word must be a usage error, got %d", code)
	}
}

func TestImportPartsPlansFirstThenWritesOnce(t *testing.T) {
	db := legoEnv(t)
	seedCatalog(t, db)
	file := filepath.Join(t.TempDir(), "parts.csv")
	os.WriteFile(file, []byte("Part,Color,Quantity\n3001,Red,10\n3001,4,5\n3023,Blue,6\nzz9,Blue,1\n"), 0600)

	stdout, code := run(t, "lego", "import-parts", file, "--dry-run", "--json")
	var plan map[string]any
	if code != 0 || json.Unmarshal([]byte(stdout), &plan) != nil || plan["dry_run"] != true || plan["new"] != float64(3) || plan["unknown_parts"] != float64(1) {
		t.Fatalf("dry run: code=%d out=%q", code, stdout)
	}
	if got, _ := db.GetOwnedPart("3001", 4, "Red"); got != nil {
		t.Fatal("--dry-run must not write anything")
	}
	if _, code := run(t, "lego", "import-parts", file); code != exitUsage {
		t.Errorf("without a terminal and without --yes it must refuse, got %d", code)
	}
	if _, code := run(t, "lego", "import-parts", file, "--yes"); code != 0 {
		t.Fatalf("import failed: %d", code)
	}
	got, _ := db.GetOwnedPart("3001", 4, "Red")
	if got == nil || got.Qty != 15 || got.Name != "Brick 2 x 4" || got.Category != "Bricks" {
		t.Fatalf("rows for the same part+colour are summed and named from the catalog: %+v", got)
	}
	run(t, "lego", "import-parts", file, "--yes", "--mode", "set")
	if got, _ := db.GetOwnedPart("3001", 4, "Red"); got.Qty != 15 {
		t.Errorf("--mode set replaces instead of adding, got %d", got.Qty)
	}
	run(t, "lego", "import-parts", file, "--yes")
	if got, _ := db.GetOwnedPart("3001", 4, "Red"); got.Qty != 30 {
		t.Errorf("--mode add (the default) adds, got %d", got.Qty)
	}
	if _, code := run(t, "lego", "import-parts", filepath.Join(t.TempDir(), "nope.csv")); code != exitNotFound {
		t.Errorf("a missing file = %d", code)
	}
	bad := filepath.Join(t.TempDir(), "bad.csv")
	os.WriteFile(bad, []byte("name,price\nx,1\n"), 0600)
	if _, code := run(t, "lego", "import-parts", bad, "--dry-run"); code != exitUsage {
		t.Errorf("a file that is not a parts list = %d", code)
	}
	if _, code := run(t, "lego", "import-parts", file, "--mode", "merge", "--dry-run"); code != exitUsage {
		t.Errorf("a bad mode = %d", code)
	}
}

func TestImportPartsBrickLinkNeedsAKeyForColours(t *testing.T) {
	legoEnv(t)
	file := filepath.Join(t.TempDir(), "inv.xml")
	os.WriteFile(file, []byte(`<INVENTORY><ITEM><ITEMTYPE>P</ITEMTYPE><ITEMID>3001</ITEMID><COLOR>5</COLOR><QTY>2</QTY></ITEM></INVENTORY>`), 0600)
	if _, code := run(t, "lego", "import-parts", file, "--dry-run"); code != exitAuth {
		t.Errorf("BrickLink colours are translated through Rebrickable: no key = exit %d, want %d", code, exitAuth)
	}
}

func fakeRebrickable(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sets/75192-1/parts/":
			fmt.Fprint(w, `{"next":null,"results":[
			 {"part":{"part_num":"3001","name":"Brick 2 x 4","external_ids":{"BrickLink":["3001"]}},"color":{"id":4,"name":"Red","rgb":"C91A09","external_ids":{"BrickLink":{"ext_ids":[5]}}},"quantity":10,"is_spare":false}]}`)
		case "/colors/":
			fmt.Fprint(w, `{"results":[{"id":4,"name":"Red","rgb":"C91A09","is_trans":false,"external_ids":{"BrickLink":{"ext_ids":[5]}}}]}`)
		case "/parts/3001/":
			fmt.Fprint(w, `{"part_num":"3001","name":"Brick 2 x 4","part_cat_id":11,"external_ids":{"BrickLink":["3001"]}}`)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("REBRICKABLE_BASE_URL", srv.URL)
	t.Setenv("REBRICKABLE_API_KEY", "k")
}

func TestWantedListForASetIsBrickLinkXML(t *testing.T) {
	db := legoEnv(t)
	fakeRebrickable(t)
	_ = db.AddOwnedPart(lego.OwnedPart{PartNum: "3001", Name: "Brick 2 x 4", Category: "Bricks", ColorID: 4, ColorName: "Red", Qty: 6})
	file := filepath.Join(t.TempDir(), "falcon.xml")

	if _, code := run(t, "lego", "wanted"); code != exitUsage {
		t.Errorf("neither --set nor --low = %d", code)
	}
	if _, code := run(t, "lego", "wanted", "--set", "75192", "--json"); code != exitUsage {
		t.Errorf("--json without -o = %d (the XML would corrupt the JSON)", code)
	}
	stdout, code := run(t, "lego", "wanted", "--set", "75192", "-o", file, "--json")
	if code != 0 {
		t.Fatalf("wanted: code=%d out=%q", code, stdout)
	}
	xmlBytes, _ := os.ReadFile(file)
	for _, want := range []string{"<ITEMID>3001</ITEMID>", "<COLOR>5</COLOR>", "<MINQTY>4</MINQTY>"} { // need 10, hold 6
		if !strings.Contains(string(xmlBytes), want) {
			t.Errorf("XML lacks %s:\n%s", want, xmlBytes)
		}
	}
	if _, code := run(t, "lego", "wanted", "--set", "75192", "-o", file); code != exitUsage {
		t.Errorf("an existing file must not be overwritten silently, got %d", code)
	}
	if _, code := run(t, "lego", "wanted", "--set", "75192", "-o", file, "--force"); code != 0 {
		t.Errorf("--force replaces it, got %d", code)
	}
	if _, code := run(t, "lego", "wanted", "--set", "00000", "-o", filepath.Join(t.TempDir(), "x.xml")); code != exitNotFound {
		t.Errorf("an unknown set = %d, want %d", code, exitNotFound)
	}
}

func TestWantedLowWithoutAKeyFallsBackAndSaysSo(t *testing.T) {
	db := legoEnv(t)
	_ = db.AddOwnedPart(lego.OwnedPart{PartNum: "3001", Name: "Brick", Category: "Bricks", ColorID: 4, ColorName: "Red", Qty: 1})
	_ = db.SetMinQty("3001", 4, "Red", 10)
	file := filepath.Join(t.TempDir(), "low.xml")
	stdout, code := run(t, "lego", "wanted", "--low", "-o", file)
	if code != 0 || !strings.Contains(stdout, "no BrickLink part number") || !strings.Contains(stdout, "colour is left out") {
		t.Fatalf("code=%d out=%q", code, stdout)
	}
	if b, _ := os.ReadFile(file); !strings.Contains(string(b), "<MINQTY>9</MINQTY>") || strings.Contains(string(b), "<COLOR>") {
		t.Errorf("XML:\n%s", b)
	}
	if _, code := run(t, "lego", "wanted", "--set", "75192", "-o", filepath.Join(t.TempDir(), "n.xml")); code != exitAuth {
		t.Errorf("a set's parts list needs the key: %d", code)
	}
}

func TestHistorySnapshotsAndRestoreThroughTheCLI(t *testing.T) {
	db := legoEnv(t)
	t.Setenv("WMS_USER", "alex")
	_ = db.AddOwnedPart(lego.OwnedPart{PartNum: "3001", Name: "Brick", Category: "Bricks", ColorID: 4, ColorName: "Red", Qty: 10})
	if _, code := run(t, "lego", "snapshots", "take", "--label", "good state"); code != 0 {
		t.Fatalf("snapshot take failed: %d", code)
	}
	_ = db.AddOwnedPart(lego.OwnedPart{PartNum: "3001", Name: "Brick", Category: "Bricks", ColorID: 4, ColorName: "Red", Qty: 1})
	_ = db.AddOwnedPart(lego.OwnedPart{PartNum: "9999", Name: "Oops", ColorID: -1, ColorName: "Odd", Qty: 50})

	stdout, code := run(t, "lego", "history", "--part", "3001")
	if code != 0 || !strings.Contains(stdout, "10 -> 1") || !strings.Contains(stdout, "change") {
		t.Fatalf("history: code=%d %q", code, stdout)
	}
	stdout, code = run(t, "lego", "snapshots")
	if code != 0 || !strings.Contains(stdout, "good state") || !strings.Contains(stdout, "Pieces over time") {
		t.Fatalf("snapshots: code=%d %q", code, stdout)
	}
	snaps, _ := db.Snapshots(10)
	var good int64
	for _, s := range snaps {
		if s.Label == "good state" {
			good = s.ID
		}
	}
	ref := strconv.FormatInt(good, 10)

	stdout, code = run(t, "lego", "restore", ref, "--dry-run")
	if code != 0 || !strings.Contains(stdout, "1 removed") || !strings.Contains(stdout, "1 changed") {
		t.Fatalf("dry run: code=%d %q", code, stdout)
	}
	if got, _ := db.GetOwnedPart("9999", -1, "Odd"); got == nil {
		t.Fatal("--dry-run must not change anything")
	}
	if _, code := run(t, "lego", "restore", ref); code != exitUsage {
		t.Errorf("no terminal and no --yes must refuse, got %d", code)
	}
	if _, code := run(t, "lego", "restore", ref, "--yes"); code != 0 {
		t.Fatalf("restore failed: %d", code)
	}
	if got, _ := db.GetOwnedPart("3001", 4, "Red"); got.Qty != 10 {
		t.Errorf("3001 = %d, want 10", got.Qty)
	}
	if got, _ := db.GetOwnedPart("9999", -1, "Odd"); got != nil {
		t.Errorf("the part added after the snapshot is gone: %+v", got)
	}
	if stdout, _ := run(t, "lego", "restore", ref, "--yes"); !strings.Contains(stdout, "nothing to do") {
		t.Errorf("restoring twice is a no-op: %q", stdout)
	}
	if _, code := run(t, "lego", "restore", "2001-01-01"); code != exitNotFound {
		t.Errorf("unknown snapshot = %d", code)
	}
	h, _ := db.History("", 50)
	sawActor := false
	for _, r := range h {
		if r.Actor == "cli:alex" || r.Actor == "system" {
			sawActor = true
		}
	}
	if !sawActor {
		t.Errorf("journal entries name who acted: %+v", h)
	}
}

func TestExportCommandFormatsFilesAndRoundTrip(t *testing.T) {
	db := legoEnv(t)
	seedCatalog(t, db)
	_ = db.AddOwnedPart(lego.OwnedPart{PartNum: "3001", Name: "Brick 2 x 4", Category: "Bricks", ColorID: 4, ColorName: "Red", Qty: 10})
	_ = db.AddOwnedPart(lego.OwnedPart{PartNum: "3023", Name: "Plate 1 x 2", Category: "Plates", ColorID: 1, ColorName: "Blue", Qty: 6})
	dir := t.TempDir()
	file := filepath.Join(dir, "parts.csv")

	if _, code := run(t, "lego", "export"); code != exitUsage {
		t.Errorf("--format is required: %d", code)
	}
	if _, code := run(t, "lego", "export", "--format", "pdf"); code != exitUsage {
		t.Errorf("unknown format: %d", code)
	}
	if _, code := run(t, "lego", "export", "--format", "csv", "--json"); code != exitUsage {
		t.Errorf("--json needs -o: %d", code)
	}
	if _, code := run(t, "lego", "export", "--format", "csv", "--set", "75192"); code != exitUsage {
		t.Errorf("--set needs --missing: %d", code)
	}
	if stdout, code := run(t, "lego", "export", "--format", "rebrickable-csv", "-o", file); code != 0 || !strings.Contains(stdout, "2 part line(s)") {
		t.Fatalf("export: code=%d %q", code, stdout)
	}
	if _, code := run(t, "lego", "export", "--format", "rebrickable-csv", "-o", file); code != exitUsage {
		t.Errorf("an existing file is not overwritten: %d", code)
	}
	if _, code := run(t, "lego", "export", "--format", "rebrickable-csv", "-o", file, "--force"); code != 0 {
		t.Errorf("--force replaces it: %d", code)
	}
	b, _ := os.ReadFile(file)
	if string(b) != "Part,Color,Quantity\n3001,4,10\n3023,1,6\n" {
		t.Errorf("file = %q", b)
	}

	// what one exports, another instance imports: wipe and re-import
	db.Exec(`DELETE FROM owned_parts`)
	if _, code := run(t, "lego", "import-parts", file, "--yes"); code != 0 {
		t.Fatal("import-parts of the export failed")
	}
	if got, _ := db.GetOwnedPart("3001", 4, "Red"); got == nil || got.Qty != 10 {
		t.Errorf("round trip: %+v", got)
	}
	stdout, code := run(t, "lego", "export", "--format", "html")
	if code != 0 || !strings.Contains(stdout, "<!doctype html>") || !strings.Contains(stdout, "Brick 2 x 4") {
		t.Errorf("html to stdout: code=%d %.80q", code, stdout)
	}
	// missing parts for a set from the offline catalog
	db.Exec(`INSERT INTO cat_inventories (id, version, set_num) VALUES (1,1,'75192-1'); INSERT INTO cat_inventory_parts (inventory_id, part_num, color_id, quantity) VALUES (1,'3001',4,25)`)
	db.Exec(`INSERT INTO cat_sets (set_num, name, year, theme_id, num_parts) VALUES ('75192-1','Falcon',2017,0,25)`)
	stdout, code = run(t, "lego", "export", "--format", "rebrickable-csv", "--set", "75192", "--missing")
	if code != 0 || !strings.Contains(stdout, "3001,4,15") {
		t.Errorf("missing export: code=%d %q", code, stdout)
	}
}

func TestAddPartAcceptsAnElementIDAndRefusesJunk(t *testing.T) {
	db := legoEnv(t)
	seedCatalog(t, db)
	if _, err := db.Exec(`INSERT INTO cat_element_ids (element_id, part_num, color_id) VALUES ('300121', '3001', 4)`); err != nil {
		t.Fatal(err)
	}
	stdout, code := run(t, "lego", "add-part", "300121", "--qty", "2", "--dry-run")
	if code != 0 || !strings.Contains(stdout, "Element 300121 is part 3001 in Red") || !strings.Contains(stdout, "IPN 3001-4") {
		t.Fatalf("an element ID is one part in one colour: code=%d out=%q", code, stdout)
	}
	if _, code := run(t, "lego", "add-part", "3001,x", "--qty", "1", "--color", "red", "--dry-run"); code != exitUsage {
		t.Errorf("junk as a part number is a usage error, got %d", code)
	}
}

func TestDoctorWarnsWhenACollectionHasNoBackupAndAcceptsARecentOne(t *testing.T) {
	db := legoEnv(t)
	dir := t.TempDir()
	t.Setenv("MODERNWMS_BACKUP_DIR", dir)
	if c := checkLegoBackup(time.Now()); c.Status != stOK {
		t.Errorf("an empty collection needs no backup: %+v", c)
	}
	_ = db.AddOwnedPart(lego.OwnedPart{PartNum: "3001", Name: "Brick 2 x 4", Category: "Bricks", ColorID: 4, ColorName: "Red", Qty: 3})
	if c := checkLegoBackup(time.Now()); c.Status != stWarn || !strings.Contains(c.Detail, "no backup") {
		t.Errorf("a collection with data and no backup must warn: %+v", c)
	}
	if err := os.MkdirAll(filepath.Join(dir, "lego"), 0o700); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(dir, "lego", "lego_db_20260921_000000.db")
	os.WriteFile(f, []byte("x"), 0o600)
	if c := checkLegoBackup(time.Now()); c.Status != stOK {
		t.Errorf("a fresh backup is fine: %+v", c)
	}
	old := time.Now().Add(-40 * 24 * time.Hour)
	os.Chtimes(f, old, old)
	if c := checkLegoBackup(time.Now()); c.Status != stWarn || !strings.Contains(c.Detail, "40 days") {
		t.Errorf("an old backup warns: %+v", c)
	}
}
