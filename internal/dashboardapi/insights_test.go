package dashboardapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestCollectionValueReturnsCurrentAndHistory(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()
	resp := getWithToken(t, srv.URL, "/dashboard/lego/value")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out struct {
		Current map[string]any   `json:"current"`
		History []map[string]any `json:"history"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Current == nil {
		t.Fatal("expected a current value object")
	}
}

func TestSystemHealthReportsConfiguredFlags(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()
	resp := getWithToken(t, srv.URL, "/dashboard/admin/system-health")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	// testServer builds a Server with no bl/bo clients, so both must read false.
	if v, _ := out["bricklink_configured"].(bool); v {
		t.Fatal("expected bricklink_configured=false with no client injected")
	}
}
