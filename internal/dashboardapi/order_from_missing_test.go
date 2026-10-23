package dashboardapi

import (
	"net/http"
	"testing"
)

func TestCreateOrderFromMissingRequiresSetNumAndBy(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()
	resp := postJSON(t, srv.URL, "/dashboard/orders/from-missing", `{}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestCreateOrderFromMissingReportsASetWithNoCheck(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()
	resp := postJSON(t, srv.URL, "/dashboard/orders/from-missing", `{"set_num":"99999-1","by":"admin"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for a set that's never been checked", resp.StatusCode)
	}
}
