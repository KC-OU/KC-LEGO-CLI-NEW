package dashboardapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui/img"
)

func TestExportSetAsSortingSheetAndJSON(t *testing.T) {
	prev := newExportImages
	newExportImages = func() *img.Fetcher { return &img.Fetcher{Dir: t.TempDir(), Offline: true} } // no network, no live cache
	t.Cleanup(func() { newExportImages = prev })
	db, srv := testServer(t, "secret")
	defer srv.Close()
	for _, q := range []string{
		`INSERT INTO cat_sets (set_num, name, year, theme_id, num_parts, img_url) VALUES ('75192-1','Millennium Falcon',2017,0,7541,'')`,
		`INSERT INTO cat_inventories (id, set_num, version) VALUES (1,'75192-1',1)`,
		`INSERT INTO cat_inventory_parts (inventory_id, part_num, color_id, quantity) VALUES (1,'3001',0,10)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}

	resp := getWithToken(t, srv.URL, "/dashboard/export/set?num=75192-1&format=sorting-html")
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("status %d, type %q, body %.200s", resp.StatusCode, resp.Header.Get("Content-Type"), body)
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, "inline") || !strings.Contains(cd, "set-75192-1-sorting-html.html") {
		t.Errorf("disposition %q", cd)
	}
	if !strings.Contains(string(body), `class="mk"`) || !strings.Contains(string(body), "placeholder=\"miss\"") {
		t.Errorf("the sheet is missing its tick/cross/? boxes or missing/extra column")
	}
	if !strings.Contains(string(body), "Sorting sheet") || !strings.Contains(string(body), "3001") {
		t.Errorf("not a sorting sheet with the part: %.300s", body)
	}

	// the downloader's numeric ID pre-fills the sign-off boxes; a name or username never does
	withID := getWithToken(t, srv.URL, "/dashboard/export/set?num=75192-1&format=sorting-html&by=4821")
	idBody, _ := io.ReadAll(withID.Body)
	withID.Body.Close()
	if !strings.Contains(string(idBody), `id="hs" inputmode="numeric" maxlength="6" value="4821"`) {
		t.Errorf("the sheet should pre-fill the numeric ID 4821")
	}
	withName := getWithToken(t, srv.URL, "/dashboard/export/set?num=75192-1&format=sorting-html&by=bob2")
	nameBody, _ := io.ReadAll(withName.Body)
	withName.Body.Close()
	if strings.Contains(string(nameBody), `value="2"`) || strings.Contains(string(nameBody), "bob") {
		t.Errorf("a username must not reach the sheet, not even its digits")
	}

	resp2 := getWithToken(t, srv.URL, "/dashboard/export/set?num=75192-1&format=json")
	defer resp2.Body.Close()
	var out struct{ Title string }
	if err := json.NewDecoder(resp2.Body).Decode(&out); err != nil || !strings.Contains(out.Title, "Millennium Falcon") {
		t.Fatalf("json export title %q, err %v", out.Title, err)
	}
	if cd := resp2.Header.Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment") {
		t.Errorf("json should download, got %q", cd)
	}
}

func TestExportRejectsWhatTheTerminalWouldNotOffer(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()
	for path, want := range map[string]int{
		"/dashboard/export/owned?format=sorting-html":    http.StatusBadRequest, // sorting sheet is for one set
		"/dashboard/export/set?format=json":              http.StatusBadRequest, // no set number
		"/dashboard/export/set?num=../x&format=json":     http.StatusBadRequest,
		"/dashboard/export/nonsense?format=json":         http.StatusBadRequest,
		"/dashboard/export/set?num=99999-1&format=json":  http.StatusNotFound,
		"/dashboard/export/owned?format=rebrickable-csv": http.StatusOK,
		"/dashboard/export/missing?format=csv":           http.StatusOK,
		"/dashboard/export/build?format=xlsx":            http.StatusConflict, // empty offline catalog
	} {
		resp := getWithToken(t, srv.URL, path)
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("%s = %d, want %d", path, resp.StatusCode, want)
		}
	}
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/dashboard/export/owned?format=json", nil)
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no token: %v %v", resp, err)
	}
}
