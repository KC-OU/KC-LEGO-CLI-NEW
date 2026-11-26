package dashboardapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

func TestSetCheckReturnsTheMostRecentlyFinishedChecksLines(t *testing.T) {
	db, srv := testServer(t, "secret")
	defer srv.Close()

	for _, q := range []string{
		`INSERT INTO cat_sets (set_num, name, year, theme_id, num_parts, img_url) VALUES ('75192-1','Millennium Falcon',2017,0,2,'')`,
		`INSERT INTO cat_inventories (id, set_num, version) VALUES (1,'75192-1',1)`,
		`INSERT INTO cat_colors (id, name, rgb) VALUES (0,'Black','')`,
		`INSERT INTO cat_inventory_parts (inventory_id, part_num, color_id, quantity) VALUES (1,'3001',0,4)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.SetElementID("3001", 0, "300126", "admin"); err != nil {
		t.Fatal(err)
	}

	c, err := db.NewCheck(context.Background(), nil, "75192-1", lego.CheckIntake, "kc")
	if err != nil {
		t.Fatal(err)
	}
	c.CheckedBy = "kc"
	c.Lines[0].Have = 3 // 1 short of Need=4
	if _, err := db.FinishCheck(c); err != nil {
		t.Fatal(err)
	}

	resp := getWithToken(t, srv.URL, "/dashboard/checks/75192-1")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var body checkView
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.SetNum != "75192-1" || body.Status != lego.StatusDone {
		t.Fatalf("got %+v", body)
	}
	if len(body.Lines) != 1 || body.Lines[0].PartNum != "3001" || body.Lines[0].Missing != 1 {
		t.Fatalf("unexpected lines: %+v", body.Lines)
	}
	if body.Lines[0].ElementID != "300126" || body.Lines[0].ImageURL == "" {
		t.Fatalf("expected element id/image to be enriched, got %+v", body.Lines[0])
	}
	if body.Missing != 1 {
		t.Fatalf("totals.missing = %d, want 1", body.Missing)
	}
}

func TestSetCheckIs404WhenNothingHasBeenCheckedYet(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()
	resp := getWithToken(t, srv.URL, "/dashboard/checks/00000-9")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}
