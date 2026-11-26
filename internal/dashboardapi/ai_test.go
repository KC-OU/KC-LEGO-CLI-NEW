package dashboardapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ai"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

func TestAISearchRunsTheModelsFilterAgainstOwnedStockAndGuardsMisconfiguration(t *testing.T) {
	nim := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"choices":[{"message":{"content":"{\"words\":[\"brick\"],\"color\":\"red\",\"max_price\":0}"}}]}`))
	}))
	defer nim.Close()
	old := ai.BaseURL
	ai.BaseURL = nim.URL
	defer func() { ai.BaseURL = old }()
	db, srv := testServer(t, "secret")
	defer srv.Close()
	db.AddOwnedPart(lego.OwnedPart{PartNum: "3001", Name: "Brick 2 x 4", ColorID: 4, ColorName: "Red", Qty: 5})

	t.Setenv(config.NvidiaNIMKey, "")
	if r := postJSON(t, srv.URL, "/dashboard/ai/search", `{"q":"red bricks"}`); r.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("no key = %d, want 503", r.StatusCode)
	}
	t.Setenv(config.NvidiaNIMKey, "k")
	t.Setenv(config.NvidiaNIMModels, "m1")
	r := postJSON(t, srv.URL, "/dashboard/ai/search", `{"q":"red bricks"}`)
	var out struct{ Hits []lego.NLHit }
	json.NewDecoder(r.Body).Decode(&out)
	r.Body.Close()
	if r.StatusCode != http.StatusOK || len(out.Hits) != 1 || out.Hits[0].PartNum != "3001" {
		t.Fatalf("search = %d %+v", r.StatusCode, out)
	}
	if r := postJSON(t, srv.URL, "/dashboard/ai/search", `{"q":""}`); r.StatusCode != http.StatusBadRequest {
		t.Errorf("empty q = %d, want 400", r.StatusCode)
	}
	if r := postJSON(t, srv.URL, "/dashboard/ai/listing", `{"part_num":"nope"}`); r.StatusCode != http.StatusNotFound {
		t.Errorf("unknown part = %d, want 404", r.StatusCode)
	}
}
