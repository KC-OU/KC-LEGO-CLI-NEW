package lego

import (
	"context"
	"strings"
	"testing"
)

func TestFTSQueryIsSafeAndPrefixed(t *testing.T) {
	cases := map[string]string{
		"brick 2 x 4":       `"brick"* "2"* "x"* "4"*`,
		"  3001  ":          `"3001"*`,
		`he said "hi" OR *`: `"he"* "said"* "hi"* "OR"*`, // operators become plain words
		"a-b_c":             `"a"* "b"* "c"*`,
		"2x4":               `"2"* "x"* "4"*`,
		"Brick 1X2X3":       `"Brick"* "1"* "X"* "2"* "X"* "3"*`,
		"3001a":             `"3001a"*`,
		"":                  "",
		"!!! ---":           "",
		"Ünïcode café":      `"Ünïcode"* "café"*`,
		`") OR name:*`:      `"OR"* "name"*`,
	}
	for in, want := range cases {
		if got := ftsQuery(in); got != want {
			t.Errorf("ftsQuery(%q) = %q, want %q", in, got, want)
		}
	}
	if got := ftsQuery(strings.Repeat("word ", 50)); strings.Count(got, `"*`) != 8 {
		t.Errorf("a pasted paragraph is capped at 8 words: %q", got)
	}
}

func TestOfflineSearchNeedsNoKeyAndNoNetwork(t *testing.T) {
	db := loadedCatalog(t)

	parts, err := db.SearchCatalogParts("bri 2x4", 10)
	if err != nil || len(parts) != 1 || parts[0].Num != "3001" {
		t.Fatalf("bri 2x4 = %+v %v", parts, err)
	}
	parts, err = db.SearchCatalogParts("brick 2 x 4", 10)
	if err != nil || len(parts) != 1 || parts[0].Num != "3001" || parts[0].Category != "Bricks" {
		t.Fatalf("brick 2 x 4 = %+v %v", parts, err)
	}
	if got, _ := db.SearchCatalogParts("3001", 10); len(got) != 1 || got[0].Num != "3001" {
		t.Errorf("a part number finds the part: %+v", got)
	}
	if got, _ := db.SearchCatalogParts("plat", 10); len(got) != 1 || got[0].Num != "3023" {
		t.Errorf("a prefix finds Plate: %+v", got)
	}
	if got, _ := db.SearchCatalogParts("", 10); got != nil {
		t.Errorf("an empty search is empty: %+v", got)
	}
	for _, evil := range []string{`"`, `*`, `) OR (`, `name:x`, "NEAR(a b)", `'; DROP TABLE cat_parts; --`} {
		if _, err := db.SearchCatalogParts(evil, 5); err != nil {
			t.Errorf("hostile input %q must not break the query: %v", evil, err)
		}
	}
	if n := db.count("cat_parts"); n != 3 {
		t.Fatalf("cat_parts was damaged: %d", n)
	}

	sets, err := db.SearchCatalogSets("falcon", 10)
	if err != nil || len(sets) != 1 || sets[0].Num != "75192-1" || sets[0].Pieces != 7541 || sets[0].Year != 2017 {
		t.Fatalf("falcon = %+v %v", sets, err)
	}
	if sets[0].Theme != "Star Wars - Ultimate Collector Series" {
		t.Errorf("theme path = %q", sets[0].Theme)
	}
	if got, _ := db.SearchCatalogSets("75192", 10); len(got) != 1 || got[0].Num != "75192-1" {
		t.Errorf("a set number finds the set: %+v", got)
	}
	if got, _ := db.SearchCatalogSets("lloyd's ninja", 10); len(got) != 1 || got[0].Num != "71788-1" {
		t.Errorf("apostrophes in the name: %+v", got)
	}
	figs, _ := db.SearchCatalogMinifigs("toy store", 10)
	if len(figs) != 1 || figs[0].Num != "fig-000001" {
		t.Errorf("minifigs = %+v", figs)
	}
}

func TestCatalogSetAndElementLookup(t *testing.T) {
	db := loadedCatalog(t)
	for _, in := range []string{"75192", "75192-1", " 75192-1 "} {
		if s, err := db.CatalogSet(in); err != nil || s == nil || s.Name != "Millennium Falcon" {
			t.Errorf("CatalogSet(%q) = %+v %v", in, s, err)
		}
	}
	if s, _ := db.CatalogSet("nope"); s != nil {
		t.Error("an unknown set is nil")
	}
	if part, color, ok := db.ElementPart(" 300121 "); !ok || part != "3001" || color != 4 {
		t.Errorf("element 300121 = %s %d %v", part, color, ok)
	}
	if _, _, ok := db.ElementPart("999999"); ok {
		t.Error("an unknown element is not found")
	}
	if got := db.ThemePath(171); got != "Star Wars - Ultimate Collector Series" {
		t.Errorf("theme path = %q", got)
	}
	if got := db.ThemePath(0); got != "" {
		t.Errorf("no theme = %q", got)
	}
}

func TestThemePathSurvivesACycle(t *testing.T) {
	db := openScratchDB(t)
	db.Exec(`INSERT INTO cat_themes (id, name, parent_id) VALUES (1,'A',2),(2,'B',1)`)
	if got := db.ThemePath(1); got == "" || strings.Count(got, " - ") > 3 {
		t.Errorf("a malformed cycle must stop after four levels: %q", got)
	}
}

func TestLookupSetChainFallsThroughEachSource(t *testing.T) {
	ctx := context.Background()

	// 1. the offline catalog answers first, with no key at all
	db := loadedCatalog(t)
	got := db.LookupSet(ctx, &Client{}, "75192")
	if !got.Found() || got.Source != "offline catalog" || got.Set.Name != "Millennium Falcon" || got.Set.Theme == "" {
		t.Fatalf("offline = %+v", got)
	}

	// 2. the catalog lacks it, live Rebrickable answers
	live := fakeAPI(t, map[string]string{
		"/sets/99999-1/": `{"set_num":"99999-1","name":"Live Only","year":2030,"theme_id":158,"num_parts":10}`,
		"/themes/158/":   `{"id":158,"parent_id":null,"name":"Star Wars"}`,
	}, nil)
	got = db.LookupSet(ctx, live, "99999")
	if !got.Found() || got.Source != "Rebrickable (live)" || got.Set.Name != "Live Only" || got.Set.Theme != "Star Wars" {
		t.Fatalf("live = %+v", got)
	}

	// 3. the API is down: the old imported reference table answers
	empty := openScratchDB(t)
	if _, err := empty.Exec(`INSERT INTO ref_sets (set_num, name, year, theme, total_pieces) VALUES ('88888-1','Legacy Set','1999','Town','120')`); err != nil {
		t.Fatal(err)
	}
	down := fakeAPI(t, nil, map[string]int{"/sets/88888-1/": 500})
	got = empty.LookupSet(ctx, down, "88888")
	if !got.Found() || got.Source != "legacy import" || got.Set.Year != 1999 || got.Set.Pieces != 120 || len(got.Notes) == 0 {
		t.Fatalf("legacy = %+v (the API failure should still be noted)", got)
	}

	// 4. nothing knows it: not found, with a reason when the catalog is empty
	got = openScratchDB(t).LookupSet(ctx, &Client{}, "12345")
	if got.Found() || len(got.Notes) == 0 || !strings.Contains(got.Notes[0], "catalog refresh") {
		t.Fatalf("nothing = %+v", got)
	}
	if got := db.LookupSet(ctx, &Client{}, "12345"); got.Found() {
		t.Errorf("an unknown set with a loaded catalog: %+v", got)
	}
	if got := db.LookupSet(ctx, nil, "  "); got.Found() {
		t.Error("a blank number finds nothing")
	}
}

func TestFindSetsAndPartsChain(t *testing.T) {
	ctx := context.Background()
	db := loadedCatalog(t)

	// offline answers first, even with a key configured and the API dead
	dead := fakeAPI(t, nil, map[string]int{"/sets/": 500, "/parts/": 500})
	dead.APIKey = "k"
	res := db.FindSets(ctx, dead, "falcon")
	if len(res.Hits) != 1 || !strings.HasPrefix(res.Source, "offline catalog, refreshed ") || len(res.Notes) != 0 {
		t.Fatalf("offline sets = %+v", res)
	}
	if pr := db.FindParts(ctx, dead, "plate"); len(pr.Hits) != 1 || !strings.HasPrefix(pr.Source, "offline catalog") {
		t.Fatalf("offline parts = %+v", pr)
	}

	// nothing offline: live fills in (a brand-new set)
	live := fakeAPI(t, map[string]string{
		"/sets/": `{"results":[{"set_num":"99999-1","name":"Brand New","year":2030,"num_parts":10}]}`,
	}, nil)
	if res := db.FindSets(ctx, live, "brand new"); len(res.Hits) != 1 || res.Source != "live — Rebrickable" {
		t.Fatalf("live fallback = %+v", res)
	}

	// nothing offline, API down: the legacy tables answer and the outage is noted
	legacy := openScratchDB(t)
	legacy.Exec(`INSERT INTO ref_sets (set_num, name, year, theme, total_pieces) VALUES ('88888-1','Legacy Set','1999','Town','120')`)
	legacy.Exec(`INSERT INTO ref_parts (part_num, name, category) VALUES ('9999','Legacy Part','Bricks')`)
	res = legacy.FindSets(ctx, dead, "legacy")
	if len(res.Hits) != 1 || res.Source != "local — imported lookup file" || len(res.Notes) != 1 || !strings.Contains(res.Notes[0], "unreachable") {
		t.Fatalf("legacy sets = %+v", res)
	}
	if pr := legacy.FindParts(ctx, dead, "legacy"); len(pr.Hits) != 1 || pr.Hits[0].Num != "9999" {
		t.Fatalf("legacy parts = %+v", pr)
	}

	// nothing anywhere: say how to fix it
	res = openScratchDB(t).FindSets(ctx, &Client{}, "anything")
	if len(res.Hits) != 0 || res.Source != "no match" || len(res.Notes) != 1 || !strings.Contains(res.Notes[0], "catalog refresh") {
		t.Fatalf("empty = %+v", res)
	}
	if res := db.FindSets(ctx, &Client{}, "zzzzzz"); len(res.Notes) != 0 || res.Source != "no match" {
		t.Errorf("a loaded catalog with no match needs no hint: %+v", res)
	}
}

func TestSearchRanksExactAndPrefixMatchesFirst(t *testing.T) {
	db := openScratchDB(t)
	for _, q := range []string{
		`INSERT INTO cat_parts (part_num, name, part_cat_id) VALUES
			('2577','Brick Round Corner 4 x 4 Full Brick',11), ('48201','Quatro Brick 2 x 4',11), ('3001','Brick 2 x 4',11), ('3001b','Brick 2 x 4 with Hole',11)`,
		`INSERT INTO fts_parts(fts_parts) VALUES('rebuild')`,
		`INSERT INTO cat_sets (set_num, name, year, theme_id, num_parts, img_url) VALUES
			('1-1','Falcon Mini Millennium Falcon Pack',2030,0,1,''), ('75257-1','Millennium Falcon',2019,0,1328,''), ('75426-1','Millennium Falcon',2026,0,885,'')`,
		`INSERT INTO fts_sets(fts_sets) VALUES('rebuild')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	parts, _ := db.SearchCatalogParts("brick 2 x 4", 10)
	if len(parts) < 3 || parts[0].Num != "3001" || parts[1].Num != "3001b" {
		t.Errorf("the part literally named Brick 2 x 4 comes first, then names starting with it: %+v", parts)
	}
	sets, _ := db.SearchCatalogSets("millennium falcon", 10)
	if len(sets) != 3 || sets[0].Num != "75426-1" || sets[1].Num != "75257-1" {
		t.Errorf("exact names first, newest first among equals: %+v", sets)
	}
	if p, _ := db.SearchCatalogParts("100% _real", 5); len(p) != 0 {
		t.Errorf("LIKE wildcards in the term are literal: %+v", p)
	}
}

func TestColoursComeFromSetContentsWhenTheElementListLacksThePart(t *testing.T) {
	db := openScratchDB(t)
	db.Exec(`INSERT INTO cat_colors (id, name, rgb, is_trans) VALUES (4,'Red','C91A09',0),(1,'Blue','0055BF',0),(0,'Black','05131D',0)`)
	db.Exec(`INSERT INTO cat_elements (part_num, color_id) VALUES ('3001', 4)`)
	db.Exec(`INSERT INTO cat_inventory_parts (inventory_id, part_num, color_id, quantity) VALUES (1,'3001',4,2),(1,'3001',1,1),(2,'9999',0,5)`)
	cols, err := db.CatalogColorsFor("3001")
	if err != nil || len(cols) != 2 || cols[0].Name != "Blue" || cols[1].Name != "Red" {
		t.Errorf("union of elements and set contents, no duplicates: %+v %v", cols, err)
	}
	if cols, _ := db.CatalogColorsFor("9999"); len(cols) != 1 || cols[0].Name != "Black" {
		t.Errorf("a part known only from set contents: %+v", cols)
	}
}
