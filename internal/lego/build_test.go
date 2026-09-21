package lego

import (
	"fmt"
	"testing"
	"time"
)

func buildDB(t *testing.T) *DB {
	d := openScratchDB(t)
	for _, q := range []string{
		`INSERT INTO cat_themes (id, name, parent_id) VALUES (1, 'City', 0)`,
		`INSERT INTO cat_sets (set_num, name, year, theme_id, num_parts, img_url) VALUES
			('1-1','Fire Station',2020,1,100,''), ('2-1','Tiny Bag',2021,1,10,''), ('3-1','Big Castle',2019,1,1000,''), ('4-1','Old Set',2000,1,50,'')`,
		// set 1: 3001 red x60, 3023 blue x40  (100 pieces)
		`INSERT INTO cat_inventories (id, version, set_num) VALUES (11,1,'1-1'), (12,1,'2-1'), (13,1,'3-1'), (14,1,'4-1'), (15,2,'4-1')`,
		`INSERT INTO cat_inventory_parts (inventory_id, part_num, color_id, quantity) VALUES
			(11,'3001',4,60),(11,'3023',1,40),
			(12,'3001',4,10),
			(13,'3001',4,500),(13,'3023',1,500),
			(14,'3001',4,50),
			(15,'3001',4,25),(15,'3023',1,25),(15,'3001',4,0)`,
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatalf("%v: %s", err, q)
		}
	}
	return d
}

func TestCanBuildRanksByCoverageAndAppliesFilters(t *testing.T) {
	d := buildDB(t)
	own(t, d, "3001", 4, "Red", 60)
	own(t, d, "3023", 1, "Blue", 20)
	own(t, d, "9999", -1, "Sparkly", 500) // a free-text colour can't match anything

	got, err := d.CanBuild(BuildOptions{MinPercent: 10})
	if err != nil {
		t.Fatal(err)
	}
	byNum := map[string]BuildResult{}
	for _, r := range got {
		byNum[r.SetNum] = r
	}
	if r, ok := byNum["1-1"]; !ok || r.Have != 80 || r.Total != 100 || r.Percent != 80 || r.Missing != 20 || r.Theme != "City" || r.Name != "Fire Station" {
		t.Errorf("Fire Station = %+v", r)
	}
	if _, ok := byNum["2-1"]; ok {
		t.Error("a 10-piece set is below the 20-piece minimum, however complete")
	}
	// set 4-1 has two inventory versions; only the newest (25+25, and you hold 25 red of 25 red + 20 blue of 25) counts
	if r, ok := byNum["4-1"]; !ok || r.Total != 50 || r.Have != 45 {
		t.Errorf("the newest inventory version is used: %+v", r)
	}
	if r := byNum["3-1"]; r.SetNum != "" {
		t.Errorf("Big Castle: 60+20 of 1000 is 8%%, below the 10%% asked: %+v", r)
	}
	if got[0].SetNum != "4-1" && got[0].SetNum != "1-1" {
		t.Errorf("best coverage first: %+v", got)
	}
	if got[0].Percent < got[1].Percent {
		t.Errorf("sorted by coverage: %+v", got)
	}
}

func TestCanBuildFiltersAndMessages(t *testing.T) {
	d := buildDB(t)
	own(t, d, "3001", 4, "Red", 60)
	own(t, d, "3023", 1, "Blue", 20)
	if got, _ := d.CanBuild(BuildOptions{MinPercent: 91}); len(got) != 0 {
		t.Errorf("nothing is 91%% covered: %+v", got)
	}
	if got, _ := d.CanBuild(BuildOptions{MinPercent: 50, MaxMissing: 10}); len(got) != 1 || got[0].SetNum != "4-1" { // 5 missing
		t.Errorf("max missing 10: %+v", got)
	}
	if got, _ := d.CanBuild(BuildOptions{MinPercent: 10, Limit: 1}); len(got) != 1 {
		t.Errorf("limit: %+v", got)
	}
	if got, err := openScratchDB(t).CanBuild(BuildOptions{}); err == nil || got != nil {
		t.Errorf("no catalog contents must say how to fix it: %v %v", got, err)
	}
	empty := buildDB(t)
	if got, err := empty.CanBuild(BuildOptions{}); err != nil || len(got) != 0 {
		t.Errorf("owning nothing builds nothing: %v %v", got, err)
	}
}

// The query must stay fast on a big collection against a big catalog.
func TestCanBuildIsFastAtScale(t *testing.T) {
	d := openScratchDB(t)
	tx, _ := d.Begin()
	for s := 1; s <= 3000; s++ {
		tx.Exec(`INSERT INTO cat_sets (set_num, name, year, theme_id, num_parts) VALUES (?,?,?,0,60)`, fmt.Sprintf("%d-1", s), "Set", 2000+s%25)
		tx.Exec(`INSERT INTO cat_inventories (id, version, set_num) VALUES (?,1,?)`, s, fmt.Sprintf("%d-1", s))
		for p := 0; p < 60; p++ { // 180,000 inventory rows
			tx.Exec(`INSERT INTO cat_inventory_parts (inventory_id, part_num, color_id, quantity) VALUES (?,?,?,1)`, s, fmt.Sprintf("p%d", (s*7+p)%400), p%12)
		}
	}
	tx.Commit()
	for p := 0; p < 400; p++ {
		own(t, d, fmt.Sprintf("p%d", p), p%12, "c", 5)
	}
	start := time.Now()
	got, err := d.CanBuild(BuildOptions{MinPercent: 20})
	if err != nil || len(got) == 0 {
		t.Fatalf("%v %v", got, err)
	}
	if el := time.Since(start); el > 3*time.Second {
		t.Errorf("took %v for 3,000 sets x 60 parts and 400 owned lines", el)
	}
}
