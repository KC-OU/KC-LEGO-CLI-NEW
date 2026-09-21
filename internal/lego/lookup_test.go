package lego

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func loadedCatalog(t *testing.T) *DB {
	t.Helper()
	db := openScratchDB(t)
	srv := (&catalogServer{}).start(t)
	if _, err := db.RefreshCatalog(context.Background(), CatalogOptions{BaseURL: srv.URL + "/", HTTP: srv.Client()}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestLookupUsesTheOfflineCatalogWithoutAnyKey(t *testing.T) {
	db := loadedCatalog(t)
	got := db.LookupPart(context.Background(), &Client{}, "3001") // no key at all
	if got == nil || got.Name != "Brick 2 x 4" || got.Category != "Bricks" || got.Source != "offline catalog" || len(got.Colors) != 2 {
		t.Fatalf("lookup = %+v", got)
	}
}

func TestLookupGoesLiveOnlyForWhatTheCatalogLacks(t *testing.T) {
	db := loadedCatalog(t)
	cs := &countingServer{}
	srv := cs.start(t, func(hit int, w http.ResponseWriter) { w.WriteHeader(200); _, _ = w.Write([]byte(`{"results":[]}`)) })
	rb := &Client{APIKey: "k", BaseURL: srv.URL, HTTP: srv.Client(), Store: db}
	if got := db.LookupPart(context.Background(), rb, "3001"); got == nil || cs.hits != 0 {
		t.Fatalf("a part the catalog fully knows must not touch the API (%d requests)", cs.hits)
	}

	// 970c00 is in the catalog but has no colours listed: only the colours come from the live API.
	fake := fakeAPI(t, map[string]string{
		"/parts/970c00/colors/": `{"results":[{"color_id":0,"color_name":"Black","num_sets":10},{"color_id":4,"color_name":"Red","num_sets":5}]}`,
	}, nil)
	fake.Store = db
	got := db.LookupPart(context.Background(), fake, "970c00")
	if got == nil || got.Name != "Minifig Legs" || len(got.Colors) != 2 || got.Colors[1].RGB != "C91A09" {
		t.Fatalf("catalog name + live colours (with swatch colours from the catalog): %+v", got)
	}
	if !strings.Contains(got.Source, "offline catalog") || !strings.Contains(got.Source, "live") {
		t.Errorf("source should say both, got %q", got.Source)
	}
}

func TestLookupLiveWhenTheCatalogIsEmpty(t *testing.T) {
	db := openScratchDB(t) // never downloaded
	rb := fakeAPI(t, map[string]string{
		"/parts/3001/":         `{"part_num":"3001","name":"Brick 2 x 4","part_cat_id":11,"external_ids":{"BrickLink":["3001"]}}`,
		"/part_categories/11/": `{"id":11,"name":"Bricks"}`,
		"/parts/3001/colors/":  `{"results":[{"color_id":4,"color_name":"Red","num_sets":1}]}`,
		"/colors/":             `{"results":[{"id":4,"name":"Red","rgb":"C91A09","is_trans":false}]}`,
	}, nil)
	got := db.LookupPart(context.Background(), rb, "3001")
	if got == nil || got.Name != "Brick 2 x 4" || got.Category != "Bricks" || got.BrickLinkID != "3001" || got.Source != "Rebrickable (live)" {
		t.Fatalf("lookup = %+v", got)
	}
	if len(got.Colors) != 1 || got.Colors[0].RGB != "C91A09" {
		t.Errorf("live colours should carry their swatch colour: %+v", got.Colors)
	}
}

func TestLookupKeepsWorkingWhenLiveFails(t *testing.T) {
	db := loadedCatalog(t)
	_, _ = db.Exec("DELETE FROM cat_elements WHERE part_num = '3023'") // colours missing -> would go live
	_, _ = db.Exec("DELETE FROM cat_inventory_parts WHERE part_num = '3023'")
	rb := fakeAPI(t, nil, map[string]int{"/parts/3023/colors/": http.StatusUnauthorized})
	got := db.LookupPart(context.Background(), rb, "3023")
	if got == nil || got.Name != "Plate 1 x 2" || len(got.Colors) != 0 {
		t.Fatalf("the catalog answer must survive a live failure: %+v", got)
	}
	if len(got.Notes) != 1 || !strings.Contains(got.Notes[0], "rejected the API key") {
		t.Errorf("the failure should be reported as a note: %v", got.Notes)
	}
}

func TestLookupFallsBackToYourCollectionThenNothing(t *testing.T) {
	db := openScratchDB(t)
	if got := db.LookupPart(context.Background(), &Client{}, "zz-1"); got != nil {
		t.Fatalf("nothing knows this part, want nil, got %+v", got)
	}
	_ = db.AddOwnedPart(OwnedPart{PartNum: "zz-1", Name: "Mystery part", Category: "Odd", ColorID: NoColor, ColorName: "Teal", Qty: 2})
	got := db.LookupPart(context.Background(), &Client{}, "zz-1")
	if got == nil || got.Name != "Mystery part" || got.Source != "your collection" {
		t.Fatalf("lookup = %+v", got)
	}
	if db.LookupPart(context.Background(), nil, "  ") != nil {
		t.Error("a blank number is never a part")
	}
}

func TestMatchColor(t *testing.T) {
	list := []Color{{ID: 4, Name: "Red"}, {ID: 1, Name: "Blue"}, {ID: 71, Name: "Light Bluish Gray"}, {ID: 72, Name: "Dark Bluish Gray"}}
	cases := map[string]int{"4": 4, "red": 4, "RED": 4, "blue": 1, "light": 71, "light bluish gray": 71}
	for in, want := range cases {
		if c, ok := MatchColor(list, in); !ok || c.ID != want {
			t.Errorf("MatchColor(%q) = %+v, %v; want id %d", in, c, ok, want)
		}
	}
	for _, in := range []string{"", "bluish", "purple", "99"} { // ambiguous, unknown, or not a listed id
		if _, ok := MatchColor(list, in); ok {
			t.Errorf("MatchColor(%q) should not match", in)
		}
	}
}

func TestCatalogPartNumsCompletesByPrefixAndEscapesWildcards(t *testing.T) {
	db := loadedCatalog(t)
	got, err := db.CatalogPartNums("300", 10)
	if err != nil || len(got) == 0 {
		t.Fatalf("prefix 300 = %v %v", got, err)
	}
	for _, n := range got {
		if !strings.HasPrefix(n, "300") {
			t.Errorf("%q does not start with 300", n)
		}
	}
	if got, _ := db.CatalogPartNums("%", 10); len(got) != 0 {
		t.Errorf("a literal %% must not act as a wildcard: %v", got)
	}
	if got, _ := db.CatalogPartNums("", 1); len(got) != 1 {
		t.Errorf("limit not applied: %v", got)
	}
}
