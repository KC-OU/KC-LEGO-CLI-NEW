package dashboardapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
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

func TestCatalogSetLookupSortsInventoryByTheSortParam(t *testing.T) {
	db, srv := testServer(t, "secret")
	defer srv.Close()

	for _, q := range []string{
		`INSERT INTO cat_sets (set_num, name, year, theme_id, num_parts, img_url) VALUES ('75192-1','Millennium Falcon',2017,0,7541,'')`,
		`INSERT INTO cat_inventories (id, set_num, version) VALUES (1,'75192-1',1)`,
		`INSERT INTO cat_colors (id, name, rgb) VALUES (0,'Black',''), (4,'Red','')`,
		`INSERT INTO cat_inventory_parts (inventory_id, part_num, color_id, quantity) VALUES (1,'3023',4,2), (1,'3001',0,10)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}

	get := func(sort string) []string {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/dashboard/catalog/sets/75192?sort="+sort, nil)
		req.Header.Set("Authorization", "Bearer secret")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var body struct {
			Inventory []struct {
				PartNum string `json:"PartNum"`
			} `json:"inventory"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		out := make([]string, len(body.Inventory))
		for i, it := range body.Inventory {
			out[i] = it.PartNum
		}
		return out
	}

	if got := get("part_id"); len(got) != 2 || got[0] != "3001" || got[1] != "3023" {
		t.Fatalf("sort=part_id -> %v, want [3001 3023]", got)
	}
	if got := get("qty"); len(got) != 2 || got[0] != "3001" || got[1] != "3023" {
		t.Fatalf("sort=qty -> %v, want [3001 3023] (3001 has the higher quantity)", got)
	}
}

func TestCatalogSetLookupIncludesElementIDAndImageURLWhenKnown(t *testing.T) {
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
	if err := db.SetElementID("3001", 0, "300126", "admin"); err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/dashboard/catalog/sets/75192", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var body struct {
		Inventory []struct {
			ElementID string `json:"ElementID"`
			ImageURL  string `json:"ImageURL"`
		} `json:"inventory"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Inventory) != 1 || body.Inventory[0].ElementID != "300126" {
		t.Fatalf("expected element id 300126, got %+v", body.Inventory)
	}
	if body.Inventory[0].ImageURL != "https://cdn.rebrickable.com/media/parts/elements/300126.jpg" {
		t.Fatalf("unexpected image url: %q", body.Inventory[0].ImageURL)
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

func TestCatalogSetSearchByThemeAndBulkListWithSort(t *testing.T) {
	db, srv := testServer(t, "secret")
	defer srv.Close()
	for _, q := range []string{
		`INSERT INTO cat_themes (id, name, parent_id) VALUES (1, 'Star Wars', 0)`,
		`INSERT INTO cat_sets (set_num, name, year, theme_id, num_parts, img_url) VALUES ('75192-1','Millennium Falcon',2017,1,7541,''), ('75257-1','Speeder',2019,1,1329,''), ('3866-1','Other',2008,0,10,'')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.UpsertSet(lego.Set{SetNum: "75192", Name: "Millennium Falcon", Qty: 2}); err != nil {
		t.Fatal(err)
	}
	get := func(query string) (nums []string, owned map[string]int) {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/dashboard/catalog/sets?"+query, nil)
		req.Header.Set("Authorization", "Bearer secret")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var body struct {
			Sets []struct {
				Num   string `json:"Num"`
				Owned int    `json:"owned"`
			} `json:"sets"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		owned = map[string]int{}
		for _, s := range body.Sets {
			nums = append(nums, s.Num)
			owned[s.Num] = s.Owned
		}
		return
	}
	nums, owned := get("q=star+wars&sort=year_asc")
	if len(nums) != 2 || nums[0] != "75192-1" || nums[1] != "75257-1" || owned["75192-1"] != 2 || owned["75257-1"] != 0 {
		t.Errorf("theme search, oldest first, with owned counts: %v %v", nums, owned)
	}
	if nums, _ := get("q=3866,75192&sort=num_desc"); len(nums) != 2 || nums[0] != "75192-1" || nums[1] != "3866-1" {
		t.Errorf("bulk list, set number descending: %v", nums)
	}
}

func TestCatalogSetLookupShowsAFixedPartImage(t *testing.T) {
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
	if err := db.SetElementID("3001", 0, "300126", "admin"); err != nil {
		t.Fatal(err)
	}
	const fixed = "https://www.lego.com/cdn/product-assets/element.img.lod5photo.192x192/4113917.jpg?format=webply&fit=bounds&quality=75&width=150&height=120&dpr=1"
	if err := db.SetPartImageOverride("3001", 0, fixed, "admin"); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/dashboard/catalog/sets/75192", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		Inventory []struct {
			ImageURL string `json:"ImageURL"`
		} `json:"inventory"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Inventory) != 1 || body.Inventory[0].ImageURL != fixed {
		t.Fatalf("the fixed image should win over the element photo, got %+v", body.Inventory)
	}
}
