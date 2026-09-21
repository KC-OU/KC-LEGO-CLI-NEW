package lego

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestRealCatalogLoad loads Rebrickable's real files (set WMS_REAL_CSV_DIR to a
// folder holding sets/themes/minifigs/inventories/inventory_parts/inventory_sets/
// inventory_minifigs/part_relationships .csv.gz; the four older files come from
// the small fixtures). It checks scale: time, database size, search speed.
func TestRealCatalogLoad(t *testing.T) {
	dir := os.Getenv("WMS_REAL_CSV_DIR")
	if dir == "" {
		t.Skip("set WMS_REAL_CSV_DIR to run the real-data load test")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Path[1:]
		if b, err := os.ReadFile(filepath.Join(dir, name)); err == nil {
			w.Header().Set("Last-Modified", "Sat, 19 Sep 2026 03:00:00 GMT")
			w.Write(b)
			return
		}
		key := name[:len(name)-len(".csv.gz")]
		if s, ok := catalogCSV[key]; ok {
			w.Write(gz(s))
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()

	dbPath := filepath.Join(t.TempDir(), "lego.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	start := time.Now()
	res, err := db.RefreshCatalog(context.Background(), CatalogOptions{BaseURL: srv.URL + "/", HTTP: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("loaded in %v: %v", time.Since(start).Round(time.Millisecond), res.Rows)
	fi, _ := os.Stat(dbPath)
	t.Logf("database size: %.1f MB", float64(fi.Size())/1e6)

	for _, term := range []string{"millennium falcon", "lloyd", "75192", "ninjago"} {
		st := time.Now()
		hits, err := db.SearchCatalogSets(term, 25)
		if err != nil || len(hits) == 0 {
			t.Errorf("set search %q = %d hits, %v", term, len(hits), err)
		}
		if d := time.Since(st); d > 200*time.Millisecond {
			t.Errorf("set search %q took %v", term, d)
		}
		if len(hits) > 0 {
			t.Logf("%-18s -> %d hits, first: %s %s [%s] (%v)", term, len(hits), hits[0].Num, hits[0].Name, hits[0].Theme, time.Since(st).Round(time.Microsecond))
		}
	}
	if s, _ := db.CatalogSet("75192"); s == nil || s.Pieces < 7000 {
		t.Errorf("75192 = %+v", s)
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM cat_inventory_parts`).Scan(&n)
	if n < 1000000 {
		t.Errorf("inventory_parts rows = %d", n)
	}
}
