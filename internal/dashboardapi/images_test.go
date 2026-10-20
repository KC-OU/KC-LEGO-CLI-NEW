package dashboardapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

func TestSetImageOverrideIsPickedUpByCatalogSet(t *testing.T) {
	db, srv := testServer(t, "secret")
	defer srv.Close()

	for _, q := range []string{
		`INSERT INTO cat_themes (id, name, parent_id) VALUES (1, 'Star Wars', 0)`,
		`INSERT INTO cat_sets (set_num, name, year, theme_id, num_parts, img_url) VALUES ('75192-1', 'Falcon', 2017, 1, 7541, 'https://original.example/falcon.jpg')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}

	body := `{"set_num":"75192-1","image_url":"https://override.example/falcon.jpg","by":"admin"}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/dashboard/admin/sets/image", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	hit, err := db.CatalogSet("75192-1")
	if err != nil || hit == nil || hit.ImgURL != "https://override.example/falcon.jpg" {
		t.Fatalf("expected the override to apply, got %+v (err %v)", hit, err)
	}
}

func TestSetImageRequiresSetNumAndBy(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	body := `{"image_url":"https://example.com/x.jpg"}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/dashboard/admin/sets/image", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestSetImageRequiresTheSharedToken(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/dashboard/admin/sets/image", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestSetDetailIncludesTheResolvedImageURL(t *testing.T) {
	db, srv := testServer(t, "secret")
	defer srv.Close()

	for _, q := range []string{
		`INSERT INTO cat_themes (id, name, parent_id) VALUES (1, 'Star Wars', 0)`,
		`INSERT INTO cat_sets (set_num, name, year, theme_id, num_parts, img_url) VALUES ('75192-1', 'Falcon', 2017, 1, 7541, 'https://original.example/falcon.jpg')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.UpsertSet(lego.Set{SetNum: "75192-1", Name: "Falcon", Qty: 1, PartsQty: 7541}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetImageOverride("75192-1", "https://override.example/falcon.jpg"); err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/dashboard/sets/75192-1", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var body struct {
		ImageURL string `json:"image_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.ImageURL != "https://override.example/falcon.jpg" {
		t.Fatalf("expected the overridden image url, got %q", body.ImageURL)
	}
}
