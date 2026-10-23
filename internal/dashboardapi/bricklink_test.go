package dashboardapi

import (
	"net/http"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/bricklink"
)

func TestParseBLItemType(t *testing.T) {
	cases := map[string]bricklink.ItemType{"part": bricklink.Part, "P": bricklink.Part, "set": bricklink.Set, "minifig": bricklink.Minifig, "m": bricklink.Minifig}
	for in, want := range cases {
		got, ok := parseBLItemType(in)
		if !ok || got != want {
			t.Errorf("parseBLItemType(%q) = %q, %v; want %q, true", in, got, ok, want)
		}
	}
	if _, ok := parseBLItemType("widget"); ok {
		t.Error(`parseBLItemType("widget") should reject an unknown type`)
	}
}

func TestBricklinkRoutesWithNoClientAre503(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()
	cases := []struct {
		method, path, body string
	}{
		{http.MethodGet, "/dashboard/bricklink/item/part/3001", ""},
		{http.MethodGet, "/dashboard/bricklink/price/part/3001", ""},
		{http.MethodGet, "/dashboard/bricklink/where-used/3001", ""},
		{http.MethodPost, "/dashboard/admin/bricklink/colors/sync", `{"by":"admin"}`},
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
