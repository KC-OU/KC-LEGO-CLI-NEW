package dashboardapi

import (
	"net/http"
	"testing"
)

func TestPartOutNeedsASetAndReportsUnknownOnes(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()
	for path, want := range map[string]int{
		"/dashboard/lego/partout":            http.StatusBadRequest,
		"/dashboard/lego/partout?set=0000-0": http.StatusOK, // unknown set: empty inventory, nothing priced
	} {
		resp := getWithToken(t, srv.URL, path)
		if resp.StatusCode != want {
			t.Errorf("%s status = %d, want %d", path, resp.StatusCode, want)
		}
		resp.Body.Close()
	}
}
