package dashboardapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

func TestPrintableBagsListsTheDeterministicSmallAndMainLabelsBeforeAnyCheck(t *testing.T) {
	db, srv := testServer(t, "secret")
	defer srv.Close()
	if err := db.UpsertSet(lego.Set{SetNum: "75192-1", Name: "Millennium Falcon", Theme: "Star Wars", Qty: 1}); err != nil {
		t.Fatal(err)
	}

	resp := getWithToken(t, srv.URL, "/dashboard/bags/75192-1")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out struct {
		Bags []checkBagView `json:"bags"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Bags) < 2 {
		t.Fatalf("expected at least small+main bags, got %d", len(out.Bags))
	}
	kinds := map[string]bool{}
	for _, b := range out.Bags {
		kinds[b.BagKindKey] = true
		if b.Code == "" {
			t.Errorf("bag %+v has no code", b)
		}
	}
	if !kinds["small"] || !kinds["main"] {
		t.Fatalf("expected both small and main kinds, got %v", kinds)
	}
}

func TestBagLabelReturnsAPDFForAKnownCode(t *testing.T) {
	db, srv := testServer(t, "secret")
	defer srv.Close()
	if err := db.UpsertSet(lego.Set{SetNum: "75192-1", Name: "Millennium Falcon", Theme: "Star Wars", Qty: 1}); err != nil {
		t.Fatal(err)
	}
	bags, err := db.PrintableBags("75192-1")
	if err != nil || len(bags) == 0 {
		t.Fatalf("PrintableBags: %v, %d bags", err, len(bags))
	}
	resp := getWithToken(t, srv.URL, "/dashboard/labels/bags/75192-1/"+bags[0].Code)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/pdf" {
		t.Fatalf("content-type = %q", ct)
	}
}

func TestBagLabelReportsAnUnknownCode(t *testing.T) {
	db, srv := testServer(t, "secret")
	defer srv.Close()
	if err := db.UpsertSet(lego.Set{SetNum: "75192-1", Name: "Millennium Falcon", Theme: "Star Wars", Qty: 1}); err != nil {
		t.Fatal(err)
	}
	resp := getWithToken(t, srv.URL, "/dashboard/labels/bags/75192-1/no-such-code")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}
