package dashboardapi

import (
	"net/http"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

func TestMissingReportCSVFormat(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()
	resp := getWithToken(t, srv.URL, "/dashboard/reports/missing?format=csv")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/csv; charset=utf-8" {
		t.Fatalf("content-type = %q", ct)
	}
}

func TestExtraReportCSVFormat(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()
	resp := getWithToken(t, srv.URL, "/dashboard/reports/extra?format=csv")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/csv; charset=utf-8" {
		t.Fatalf("content-type = %q", ct)
	}
}

// A stock sheet needs the offline catalog's own parts list (not just an
// owned-set row), which this package's plain testServer fixture doesn't
// seed -- this confirms the new ?format=csv branch still surfaces
// StockSheet's real error as JSON rather than emitting a broken CSV body.
func TestStockSheetCSVFormatSurfacesTheUnderlyingErrorAsJSON(t *testing.T) {
	db, srv := testServer(t, "secret")
	defer srv.Close()
	if err := db.UpsertSet(lego.Set{SetNum: "75192-1", Name: "Millennium Falcon", Theme: "Star Wars", Qty: 1}); err != nil {
		t.Fatal(err)
	}
	resp := getWithToken(t, srv.URL, "/dashboard/reports/stocktake/75192-1?format=csv")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (no catalog parts list seeded)", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q, want the plain JSON error path, not a CSV header", ct)
	}
}
