package dashboardapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

func TestCycleCountEndpointsRoundTrip(t *testing.T) {
	db, srv := testServer(t, "secret")
	defer srv.Close()
	db.AddOwnedPart(lego.OwnedPart{PartNum: "3001", ColorID: 4, ColorName: "Red", Qty: 10})
	resp := getWithToken(t, srv.URL, "/dashboard/counts/due")
	var due []lego.CountDue
	json.NewDecoder(resp.Body).Decode(&due)
	resp.Body.Close()
	if len(due) != 1 || due[0].PartNum != "3001" {
		t.Fatalf("due = %+v", due)
	}
	if r := postJSON(t, srv.URL, "/dashboard/counts", `{"part_num":"3001","color_id":4,"color_name":"Red","counted":8}`); r.StatusCode != http.StatusBadRequest {
		t.Errorf("missing 'by' = %d, want 400", r.StatusCode)
	}
	r := postJSON(t, srv.URL, "/dashboard/counts", `{"part_num":"3001","color_id":4,"color_name":"Red","counted":8,"by":"alice"}`)
	var out map[string]int
	json.NewDecoder(r.Body).Decode(&out)
	r.Body.Close()
	if out["variance"] != -2 {
		t.Errorf("variance = %v, want -2", out)
	}
	if q := getWithToken(t, srv.URL, "/dashboard/quality"); q.StatusCode != http.StatusOK {
		t.Errorf("quality = %d", q.StatusCode)
	}
}
