package dashboardapi

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

func TestPartDBRoutesWithNoServiceAre503(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()
	cases := []struct {
		method, path, body string
	}{
		{http.MethodGet, "/dashboard/partdb/parts", ""},
		{http.MethodGet, "/dashboard/partdb/parts/1", ""},
		{http.MethodPost, "/dashboard/partdb/parts", `{"name":"x","ipn":"x","by":"admin"}`},
		{http.MethodPost, "/dashboard/partdb/parts/1/stock", `{"delta":1,"by":"admin"}`},
	}
	for _, c := range cases {
		var resp *http.Response
		if c.method == http.MethodGet {
			resp = getWithToken(t, srv.URL, c.path)
		} else {
			resp = postJSON(t, srv.URL, c.path, c.body)
		}
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Errorf("%s %s: status = %d, want 503", c.method, c.path, resp.StatusCode)
		}
		resp.Body.Close()
	}
}

func TestOpsRoutesWithNoWMSClientAre503(t *testing.T) {
	// testServer passes nil for wms, same "nil disables" convention as
	// usersSvc/pdb -- these routes should refuse cleanly rather than panic.
	_, srv := testServer(t, "secret")
	defer srv.Close()
	cases := []struct {
		method, path, body string
	}{
		{http.MethodGet, "/dashboard/ops/inventory", ""},
		{http.MethodGet, "/dashboard/ops/masterdata/spus", ""},
		{http.MethodGet, "/dashboard/ops/masterdata/suppliers", ""},
		{http.MethodGet, "/dashboard/ops/masterdata/customers", ""},
		{http.MethodGet, "/dashboard/ops/dispatch", ""},
		{http.MethodPost, "/dashboard/ops/receive", `{"identifier":"x","qty":1,"by":"admin"}`},
	}
	for _, c := range cases {
		var resp *http.Response
		if c.method == http.MethodGet {
			resp = getWithToken(t, srv.URL, c.path)
		} else {
			resp = postJSON(t, srv.URL, c.path, c.body)
		}
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Errorf("%s %s: status = %d, want 503", c.method, c.path, resp.StatusCode)
		}
		resp.Body.Close()
	}
}

func TestSettingsGetMasksSecretsAndSetChangesOnlyNamedFields(t *testing.T) {
	t.Setenv(config.SettingsFile, filepath.Join(t.TempDir(), "settings.json"))
	_, srv := testServer(t, "secret")
	defer srv.Close()

	set := postJSON(t, srv.URL, "/dashboard/admin/settings", `{"values":{"rebrickable_api_key":"abcd1234efgh5678","tui_theme":"dark"},"by":"admin"}`)
	defer set.Body.Close()
	if set.StatusCode != http.StatusOK {
		t.Fatalf("set status = %d", set.StatusCode)
	}

	got := getWithToken(t, srv.URL, "/dashboard/admin/settings")
	defer got.Body.Close()
	var out map[string]string
	if err := json.NewDecoder(got.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out["rebrickable_api_key"] != "************5678" {
		t.Fatalf("expected the key masked to its last 4 chars, got %q", out["rebrickable_api_key"])
	}
	if out["tui_theme"] != "dark" {
		t.Fatalf("expected the plaintext theme to round-trip, got %q", out["tui_theme"])
	}
	if out["bricklink_consumer_key"] != "" {
		t.Fatalf("expected an unset secret to stay empty, got %q", out["bricklink_consumer_key"])
	}
}

func TestSetSettingsRejectsAnUnknownField(t *testing.T) {
	t.Setenv(config.SettingsFile, filepath.Join(t.TempDir(), "settings.json"))
	_, srv := testServer(t, "secret")
	defer srv.Close()
	resp := postJSON(t, srv.URL, "/dashboard/admin/settings", `{"values":{"not_a_real_setting":"x"},"by":"admin"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestSetSettingsRequiresBy(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()
	resp := postJSON(t, srv.URL, "/dashboard/admin/settings", `{"values":{"tui_theme":"dark"}}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestSetSyncAdminRequiresEveryField(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()
	resp := postJSON(t, srv.URL, "/dashboard/admin/settings/sync-admin", `{"username":"kc"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}
