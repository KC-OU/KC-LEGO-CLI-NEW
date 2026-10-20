package dashboardapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

func TestQueryPartsFindsAPartByNameOrNumber(t *testing.T) {
	db, srv := testServer(t, "secret")
	defer srv.Close()

	for _, q := range []string{
		`INSERT INTO cat_parts (part_num, name, part_cat_id) VALUES ('3001','Brick 2 x 4',11), ('3002','Brick 2 x 3',11)`,
		`INSERT INTO fts_parts(fts_parts) VALUES('rebuild')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/dashboard/queries/parts?q=brick+2+x+4", nil)
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
		Parts []lego.PartHit `json:"parts"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Parts) != 1 || body.Parts[0].Num != "3001" {
		t.Fatalf("expected only the exact-name match, got %+v", body.Parts)
	}
}

func TestQueryPartsWithNoTermIsEmptyNotAnError(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/dashboard/queries/parts", nil)
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
		Parts []lego.PartHit `json:"parts"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Parts) != 0 {
		t.Fatalf("expected no results for an empty query, got %+v", body.Parts)
	}
}

func TestQueryMinifigsFindsAMinifigByName(t *testing.T) {
	db, srv := testServer(t, "secret")
	defer srv.Close()

	for _, q := range []string{
		`INSERT INTO cat_minifigs (fig_num, name, num_parts, img_url) VALUES ('fig-000001','Luke Skywalker',4,'')`,
		`INSERT INTO fts_minifigs(fts_minifigs) VALUES('rebuild')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/dashboard/queries/minifigs?q=luke", nil)
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
		Minifigs []lego.MinifigHit `json:"minifigs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Minifigs) != 1 || body.Minifigs[0].Num != "fig-000001" {
		t.Fatalf("expected the one seeded minifig, got %+v", body.Minifigs)
	}
}

func TestQueryRoutesRequireTheSharedToken(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	for _, path := range []string{"/dashboard/queries/parts", "/dashboard/queries/minifigs"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s: status = %d, want 401", path, resp.StatusCode)
		}
	}
}
