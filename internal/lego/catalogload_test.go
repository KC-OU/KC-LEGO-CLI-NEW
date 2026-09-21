package lego

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func refreshWith(t *testing.T, db *DB, cs *catalogServer, force bool) (*CatalogResult, error) {
	t.Helper()
	srv := cs.start(t)
	return db.RefreshCatalog(context.Background(), CatalogOptions{BaseURL: srv.URL + "/", HTTP: srv.Client(), Force: force})
}

func setName(db *DB, num string) string {
	var n string
	_ = db.QueryRow(`SELECT name FROM cat_sets WHERE set_num = ?`, num).Scan(&n)
	return n
}

func TestRefreshLoadsEveryTableAndTheSearchIndexes(t *testing.T) {
	db := openScratchDB(t)
	var steps []string
	srv := (&catalogServer{}).start(t)
	res, err := db.RefreshCatalog(context.Background(), CatalogOptions{BaseURL: srv.URL + "/", HTTP: srv.Client(), Progress: func(s string) { steps = append(steps, s) }})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"colors": 5, "part_categories": 3, "parts": 3, "elements": 3, "themes": 2, "sets": 3, "minifigs": 1,
		"inventories": 3, "inventory_parts": 5, "inventory_sets": 1, "inventory_minifigs": 1, "part_relationships": 3}
	for f, n := range want {
		if res.Rows[f] != n {
			t.Errorf("%s: loaded %d rows, want %d", f, res.Rows[f], n)
		}
	}
	// spare parts (is_spare True) are not stored
	if n := db.count("cat_inventory_parts"); n != 4 {
		t.Errorf("cat_inventory_parts = %d, want 4 (the spare row skipped)", n)
	}
	if len(steps) < 2*len(catalogSpecs)-len(catalogSpecs)/2 || !strings.HasPrefix(steps[0], "downloading") {
		t.Errorf("progress = %v", steps)
	}
	tables := db.CatalogTables()
	if len(tables) != len(catalogSpecs) || tables[0].Rows == 0 || tables[0].Loaded.IsZero() {
		t.Errorf("CatalogTables = %+v", tables)
	}
	if hits, _ := db.SearchCatalogSets("falcon", 5); len(hits) != 1 {
		t.Errorf("the set index must be built by the refresh: %v", hits)
	}
}

func TestAFailedRefreshLeavesTheOldCatalogExactlyAsItWas(t *testing.T) {
	db := loadedCatalog(t)
	before := setName(db, "75192-1")

	broken := map[string]map[string][]byte{
		"header drift: a column was renamed": {"sets": gz("set_number,name,year,theme_id,num_parts,img_url\n75192-1,CHANGED,2017,171,7541,\n")},
		"truncated download":                 {"inventory_parts": gz(catalogCSV["inventory_parts"])[:40]},
		"not gzip at all":                    {"minifigs": []byte("<html>rate limited</html>")},
		"header only, no rows":               {"themes": gz("id,name,parent_id\n")},
	}
	for name, override := range broken {
		override := override
		cs := &catalogServer{override: override}
		// A good change to sets is in the same refresh: it must NOT land if any other file is bad.
		if _, ok := override["sets"]; !ok {
			override["sets"] = gz("set_num,name,year,theme_id,num_parts,img_url\n75192-1,CHANGED,2017,171,7541,\n")
		}
		if _, err := refreshWith(t, db, cs, true); err == nil {
			t.Errorf("%s: the refresh must fail", name)
			continue
		}
		if got := setName(db, "75192-1"); got != before {
			t.Errorf("%s: the catalog changed to %q although the refresh failed (all-or-nothing)", name, got)
		}
		if hits, _ := db.SearchCatalogSets("falcon", 5); len(hits) != 1 {
			t.Errorf("%s: search broke after a failed refresh: %v", name, hits)
		}
	}
}

func TestHeaderDriftNamesTheMissingColumn(t *testing.T) {
	db := openScratchDB(t)
	_, err := refreshWith(t, db, &catalogServer{override: map[string][]byte{"parts": gz("part_number,name,part_cat_id\n3001,Brick,11\n")}}, true)
	if err == nil || !strings.Contains(err.Error(), "parts") || !strings.Contains(err.Error(), `"part_num"`) || !strings.Contains(err.Error(), "format changed") {
		t.Fatalf("error should name the file and the missing column: %v", err)
	}
}

func TestARefreshThatWouldShrinkTheCatalogIsRefused(t *testing.T) {
	db := openScratchDB(t)
	var big strings.Builder
	big.WriteString("part_num,name,part_cat_id,part_material\n")
	for i := 0; i < 300; i++ {
		fmt.Fprintf(&big, "p%d,Part %d,11,Plastic\n", i, i)
	}
	if _, err := refreshWith(t, db, &catalogServer{override: map[string][]byte{"parts": gz(big.String())}}, true); err != nil {
		t.Fatal(err)
	}
	if n := db.count("cat_parts"); n != 300 {
		t.Fatalf("setup: %d parts", n)
	}
	_, err := refreshWith(t, db, &catalogServer{}, true) // the fixture has 3 parts
	if err == nil || !strings.Contains(err.Error(), "shrank") {
		t.Fatalf("a catalog that suddenly has 1%% of its parts is broken upstream, not news: %v", err)
	}
	if n := db.count("cat_parts"); n != 300 {
		t.Errorf("the old parts must survive: %d", n)
	}
}

func TestRefreshSkipsFilesUpstreamSaysAreUnchanged(t *testing.T) {
	db := openScratchDB(t)
	if _, err := refreshWith(t, db, &catalogServer{}, true); err != nil {
		t.Fatal(err)
	}
	cs := &catalogServer{notMod: true}
	res, err := refreshWith(t, db, cs, true)
	if err != nil || res.Updated != 0 || !strings.Contains(res.Message, "already current") {
		t.Fatalf("all-304 refresh = %+v %v", res, err)
	}
	if db.count("cat_sets") != 3 {
		t.Error("nothing may be reloaded on a 304")
	}
}

func TestSearchFollowsARefresh(t *testing.T) {
	db := loadedCatalog(t)
	renamed := gz("set_num,name,year,theme_id,num_parts,img_url\n75192-1,Renamed Ship,2017,171,7541,\n")
	if _, err := refreshWith(t, db, &catalogServer{override: map[string][]byte{"sets": renamed}}, true); err != nil {
		t.Fatal(err)
	}
	if hits, _ := db.SearchCatalogSets("renamed", 5); len(hits) != 1 {
		t.Errorf("the new name must be searchable: %v", hits)
	}
	if hits, _ := db.SearchCatalogSets("falcon", 5); len(hits) != 0 {
		t.Errorf("the old name must be gone from the index: %v", hits)
	}
}

func TestOldCatalogTablesAreUpgradedInPlace(t *testing.T) {
	// A lego.db from before the full catalog existed has only the four old tables;
	// opening it must add the rest without touching what is there.
	path := t.TempDir() + "/lego.db"
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	db.Exec(`INSERT INTO cat_parts (part_num, name, part_cat_id) VALUES ('3001', 'Kept', 11)`)
	for _, tbl := range []string{"cat_sets", "cat_themes", "cat_inventory_parts", "fts_sets"} {
		db.Exec("DROP TABLE " + tbl)
	}
	db.Close()
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if p, _ := db.CatalogPart("3001"); p == nil || p.Name != "Kept" {
		t.Error("existing catalog rows must survive an upgrade")
	}
	if _, err := db.SearchCatalogSets("x", 1); err != nil {
		t.Errorf("the new tables must exist after Open: %v", err)
	}
}

func TestCatalogCanBeLoadedFromFilesOnDisk(t *testing.T) {
	dir := t.TempDir()
	for name, body := range catalogCSV {
		if name == "elements" || name == "inventory_sets" { // not every file has to be there
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name+".csv.gz"), gz(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	db := openScratchDB(t)
	res, err := db.RefreshCatalog(context.Background(), CatalogOptions{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if res.Updated != len(catalogSpecs)-2 || db.count("cat_sets") != 3 || db.count("cat_element_ids") != 0 {
		t.Fatalf("from-dir = %+v sets=%d", res, db.count("cat_sets"))
	}
	if hits, _ := db.SearchCatalogSets("falcon", 5); len(hits) != 1 {
		t.Errorf("search works after a from-dir load: %v", hits)
	}
	// a second load the same day is fine (the once-a-day rule is about downloads) and reloads nothing unchanged
	res, err = db.RefreshCatalog(context.Background(), CatalogOptions{Dir: dir})
	if err != nil || res.Updated != 0 {
		t.Errorf("unchanged files are skipped: %+v %v", res, err)
	}
	if _, err := db.RefreshCatalog(context.Background(), CatalogOptions{Dir: t.TempDir()}); err == nil {
		t.Error("an empty folder must be an error")
	}
	// a from-dir load never touches the network: a dead CDN URL is irrelevant
	if _, err := db.RefreshCatalog(context.Background(), CatalogOptions{Dir: dir, BaseURL: "http://127.0.0.1:1/", Force: true}); err != nil {
		t.Errorf("--from-dir must not download: %v", err)
	}
}

func TestCapReaderStopsARunawayStream(t *testing.T) {
	c := &capReader{r: strings.NewReader(strings.Repeat("x", 100)), left: 10}
	if _, err := io.Copy(io.Discard, c); err == nil {
		t.Fatal("a stream over the cap must fail, not be read to the end")
	}
	ok := &capReader{r: strings.NewReader("abc"), left: 10}
	if b, err := io.ReadAll(ok); err != nil || string(b) != "abc" {
		t.Fatalf("under the cap must read normally: %q %v", b, err)
	}
}

// A catalog refresh is one long write transaction. Other processes (every telnet session and
// the CLI) must still be able to open the database and read while it runs.
func TestReadersAreNotBlockedWhileALongWriteIsInProgress(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lego.db")
	writer, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if _, err := writer.Exec(`INSERT INTO cat_colors (id, name, rgb, is_trans) VALUES (4, 'Red', 'C91A09', 0)`); err != nil {
		t.Fatal(err)
	}
	tx, err := writer.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO cat_colors (id, name, rgb, is_trans) VALUES (5, 'Blue', '0055BF', 0)`); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	reader, err := Open(path) // a second "process"
	if err != nil {
		t.Fatalf("opening while a write is in progress: %v", err)
	}
	defer reader.Close()
	var n int
	if err := reader.QueryRow(`SELECT COUNT(*) FROM cat_colors`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("a reader sees the committed data: n=%d err=%v", n, err)
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("readers must not wait for the writer: %v", time.Since(start))
	}
}
