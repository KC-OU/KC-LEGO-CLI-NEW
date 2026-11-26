package dashboardapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

func TestRetiredPageDataImportAndRefreshGuards(t *testing.T) {
	t.Setenv(config.BricksetAPIKey, "")
	_, srv := testServer(t, "secret")
	defer srv.Close()
	if r := getWithToken(t, srv.URL, "/dashboard/retired"); r.StatusCode != http.StatusOK {
		t.Errorf("retired = %d", r.StatusCode)
	}
	if r := postJSON(t, srv.URL, "/dashboard/retired/refresh", `{}`); r.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("refresh without a Brickset key = %d, want 503", r.StatusCode)
	}
	req, _ := http.NewRequest("POST", srv.URL+"/dashboard/retired/import?name=x.xlsx", strings.NewReader("not a spreadsheet"))
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("garbage import = %d, want 400", resp.StatusCode)
	}
}
