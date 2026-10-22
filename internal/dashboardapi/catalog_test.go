package dashboardapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestCatalogSetLookupFindsAnOfflineCatalogSet(t *testing.T) {
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

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/dashboard/catalog/sets/75192", nil)
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
		Set struct {
			Num  string `json:"Num"`
			Name string `json:"Name"`
		} `json:"set"`
		Inventory []struct {
			PartNum string `json:"PartNum"`
			Qty     int    `json:"Qty"`
		} `json:"inventory"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Set.Num != "75192-1" || body.Set.Name != "Millennium Falcon" {
		t.Fatalf("expected the seeded set, got %+v", body.Set)
	}
	if len(body.Inventory) != 1 || body.Inventory[0].PartNum != "3001" || body.Inventory[0].Qty != 10 {
		t.Fatalf("expected the one seeded part, got %+v", body.Inventory)
	}
}

func TestCatalogSetLookupOfANonexistentSetIs404(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/dashboard/catalog/sets/00000-9", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestCatalogSetLookupRequiresTheSharedToken(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/dashboard/catalog/sets/75192")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}
