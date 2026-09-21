package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/partdb"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/sync"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/wmsdb"
)

func testPartDB(t *testing.T) *partdb.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "app.db")
	sqlDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	schema := `
	CREATE TABLE categories (id INTEGER PRIMARY KEY, parent_id INTEGER, name TEXT);
	CREATE TABLE storelocations (id INTEGER PRIMARY KEY, name TEXT);
	CREATE TABLE parts (id INTEGER PRIMARY KEY, id_category INTEGER, datetime_added TEXT, name TEXT, last_modified TEXT,
		needs_review INTEGER, tags TEXT, description TEXT, comment TEXT, visible INTEGER, favorite INTEGER,
		minamount REAL, manufacturer_product_url TEXT, manufacturer_product_number TEXT, order_quantity INTEGER,
		manual_order INTEGER, ipn TEXT, gtin TEXT, mass REAL);
	CREATE TABLE part_lots (id INTEGER PRIMARY KEY, id_part INTEGER, id_store_location INTEGER, amount REAL,
		needs_refill INTEGER, vendor_barcode TEXT, datetime_added TEXT);
	INSERT INTO categories (id, parent_id, name) VALUES (1, NULL, 'Resistors');
	INSERT INTO parts (id, id_category, name, description, manufacturer_product_number, ipn, gtin, mass)
		VALUES (100, 1, 'Widget', 'A small widget', 'MFG-100', '', '', 0);
	INSERT INTO part_lots (id_part, id_store_location, amount) VALUES (100, 1, 12);
	`
	if _, err := sqlDB.Exec(schema); err != nil {
		t.Fatalf("seed schema: %v", err)
	}
	return &partdb.DB{DB: sqlDB}
}

func testServer(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	credsPath := filepath.Join(dir, "credentials.json")
	linksPath := filepath.Join(dir, "links.json")
	db := testPartDB(t)
	engine := sync.NewEngine(wmsdb.NewClient(), db)
	return NewServer(engine, db, credsPath, linksPath), credsPath
}

func TestHealth(t *testing.T) {
	s, _ := testServer(t)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ok" {
		t.Fatalf("body = %v", body)
	}
}

func TestStatusShape(t *testing.T) {
	s, _ := testServer(t)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"status", "last_sync_time", "stats", "history"} {
		if _, ok := body[key]; !ok {
			t.Errorf("missing key %q in /api/status response", key)
		}
	}
}

func TestPartsEndpoint(t *testing.T) {
	s, _ := testServer(t)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/parts", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		TotalParts int `json:"total_parts"`
		Parts      []struct {
			PartDBID      int     `json:"partdb_id"`
			SPUCode       string  `json:"spu_code"`
			TotalQuantity float64 `json:"total_quantity"`
		} `json:"parts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.TotalParts != 1 || body.Parts[0].SPUCode != "MFG-100" || body.Parts[0].TotalQuantity != 12 {
		t.Fatalf("unexpected parts body: %+v", body)
	}
}

func TestMetricsContainsAllNames(t *testing.T) {
	s, _ := testServer(t)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := rec.Body.String()
	for _, name := range []string{
		"partdb_sync_status", "partdb_synced_categories", "partdb_synced_parts",
		"partdb_synced_locations", "partdb_synced_stock_records", "partdb_total_stock_qty",
		"partdb_sync_duration_seconds", "partdb_sync_runs_total", "partdb_sync_errors_total",
		"partdb_api_requests_total", "partdb_sync_uptime_seconds",
	} {
		if !strings.Contains(body, name) {
			t.Errorf("metrics output missing %q", name)
		}
	}
}

func TestPostWithoutAuthRejected(t *testing.T) {
	s, _ := testServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/parts/update-links", bytes.NewReader([]byte(`{"part_id":100}`)))
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestPostWithBootstrappedAuthSucceeds(t *testing.T) {
	// SyncAdminPass has no hardcoded default (see credentialStore.get) —
	// bootstrap now requires the operator to set SYNC_ADMIN_PASS explicitly.
	t.Setenv("SYNC_ADMIN_PASS", "admin123")
	s, credsPath := testServer(t)
	_ = os.Remove(credsPath) // force bootstrap from SYNC_ADMIN_USER/PASS

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/parts/update-links", bytes.NewReader([]byte(`{"part_id":100,"partdb_link":"https://example.com/100"}`)))
	req.SetBasicAuth("admin", "admin123")
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec2 := httptest.NewRecorder()
	s.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/api/parts", nil))
	if !strings.Contains(rec2.Body.String(), "https://example.com/100") {
		t.Fatalf("override not reflected in /api/parts: %s", rec2.Body.String())
	}
}

// TestPostRefusesBootstrapWithoutAdminPassSet is the actual regression test
// for the SyncAdminPass hardcoded-default fix: with no credentials file and
// no SYNC_ADMIN_PASS set, the server must not silently stand up an
// "admin123" account — every authed request should fail closed.
func TestPostRefusesBootstrapWithoutAdminPassSet(t *testing.T) {
	t.Setenv("SYNC_ADMIN_PASS", "")
	s, credsPath := testServer(t)
	_ = os.Remove(credsPath)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/parts/update-links", bytes.NewReader([]byte(`{"part_id":100,"partdb_link":"https://example.com/100"}`)))
	req.SetBasicAuth("admin", "admin123")
	s.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("expected the legacy default password to be rejected when SYNC_ADMIN_PASS is unset")
	}
}
