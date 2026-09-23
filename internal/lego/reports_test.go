package lego

import (
	"context"
	"strings"
	"testing"
)

func checkedFireStation(t *testing.T, d *DB, missingRed int) {
	t.Helper()
	c, err := d.NewCheck(context.Background(), nil, "1-1", CheckIntake, "alex")
	if err != nil {
		t.Fatal(err)
	}
	for i := range c.Lines {
		if c.Lines[i].PartNum == "3001" && missingRed > 0 {
			c.Lines[i].Have -= missingRed
		}
	}
	if _, err := d.FinishCheck(c); err != nil {
		t.Fatal(err)
	}
}

func TestMissingPartsReportCoversIncompleteSetsWithTotals(t *testing.T) {
	d := buildDB(t)
	checkedFireStation(t, d, 5) // 5 red 3001 short
	data, err := d.MissingPartsReport(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Rows) != 1 || data.Rows[0].PartNum != "3001" || data.Rows[0].Qty != 5 {
		t.Fatalf("rows = %+v", data.Rows)
	}
	if !strings.Contains(data.Rows[0].SetNum, "Fire Station") {
		t.Errorf("row should carry the set title, got %q", data.Rows[0].SetNum)
	}
	joined := ""
	for _, f := range data.Facts {
		joined += f[0] + ":" + f[1] + "\n"
	}
	if !strings.Contains(joined, "Total missing:5") {
		t.Errorf("facts should total the missing count:\n%s", joined)
	}
	html, err := ReportHTML(data)
	if err != nil || !strings.Contains(string(html), "Fire Station") || !strings.Contains(string(html), "3001") {
		t.Fatalf("report html: %v", err)
	}
}

func TestMissingPartsReportExplicitSetsIgnoresComplete(t *testing.T) {
	d := buildDB(t)
	checkedFireStation(t, d, 0) // complete: nothing missing
	data, err := d.MissingPartsReport([]string{"1-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Rows) != 0 {
		t.Errorf("a complete set should add no missing rows: %+v", data.Rows)
	}
}

func TestCollectionReportHasSummaryAndRows(t *testing.T) {
	d := buildDB(t)
	own(t, d, "3001", 4, "Red", 12)
	data, err := d.CollectionReport()
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Rows) != 1 || data.Rows[0].Qty != 12 {
		t.Fatalf("rows = %+v", data.Rows)
	}
	found := false
	for _, f := range data.Facts {
		if f[0] == "Loose parts" {
			found = true
		}
	}
	if !found {
		t.Errorf("facts missing loose-parts summary: %+v", data.Facts)
	}
}

func TestSetPartsReportMultipleSets(t *testing.T) {
	d := buildDB(t)
	data, err := d.SetPartsReport([]string{"1-1", "3-1"})
	if err != nil {
		t.Fatal(err)
	}
	order, groups := bySet(data.Rows)
	if len(order) != 2 {
		t.Fatalf("expected 2 set groups, got %v", order)
	}
	total := 0
	for _, rows := range groups {
		total += len(rows)
	}
	if total != len(data.Rows) || total == 0 {
		t.Fatalf("rows split oddly: %d total, %d grouped", len(data.Rows), total)
	}
	html, err := ReportHTML(data)
	if err != nil || !strings.Contains(string(html), "Big Castle") {
		t.Fatalf("set parts report html: %v", err)
	}
}

func TestWishlistHTMLShowsSetTitleAndPartLabel(t *testing.T) {
	d := buildDB(t)
	if err := d.AddWatch(Watch{ItemType: "SET", ItemNo: "1-1", ColorID: -1, MaxPrice: 120}); err != nil {
		t.Fatal(err)
	}
	if err := d.AddWatch(Watch{ItemType: "PART", ItemNo: "3001", ColorID: 4, ColorName: "Red", MaxPrice: 0.05}); err != nil {
		t.Fatal(err)
	}
	ws, err := d.ListWatches()
	if err != nil {
		t.Fatal(err)
	}
	html, err := WishlistHTML(d, "My wishlist", ws)
	if err != nil {
		t.Fatal(err)
	}
	s := string(html)
	if !strings.Contains(s, "Fire Station") { // the set's catalog title, not just "1-1"
		t.Errorf("missing set title:\n%s", s)
	}
	if !strings.Contains(s, "3001") || !strings.Contains(s, "Red") {
		t.Errorf("missing part label:\n%s", s)
	}
}

func TestWishlistHTMLEmpty(t *testing.T) {
	d := buildDB(t)
	html, err := WishlistHTML(d, "My wishlist", nil)
	if err != nil || !strings.Contains(string(html), "Nothing on the list") {
		t.Fatalf("html: %v\n%s", err, html)
	}
}

func TestCheckHistoryNewestFirstAndFiltersByStatus(t *testing.T) {
	d := buildDB(t)
	checkedFireStation(t, d, 3)
	// a second, later check (recount) for the same set — this time it's found complete
	c, err := d.NewCheck(context.Background(), nil, "1-1", CheckRecount, "sam")
	if err != nil {
		t.Fatal(err)
	}
	for i := range c.Lines {
		c.Lines[i].Have = c.Lines[i].Need
	}
	if _, err := d.FinishCheck(c); err != nil {
		t.Fatal(err)
	}
	hist, err := d.CheckHistory("1-1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 2 {
		t.Fatalf("expected 2 finished checks, got %d", len(hist))
	}
	if hist[0].CheckedBy != "sam" || hist[0].Kind != CheckRecount {
		t.Errorf("newest first: %+v", hist[0])
	}
	if hist[1].CheckedBy != "alex" || hist[1].Missing != 3 {
		t.Errorf("oldest last: %+v", hist[1])
	}
	all, err := d.CheckHistory("", 10)
	if err != nil || len(all) != 2 {
		t.Fatalf("history across all sets: %v %d", err, len(all))
	}
	html, err := CheckHistoryHTML("All checks", hist)
	if err != nil || !strings.Contains(string(html), "sam") || !strings.Contains(string(html), "COMPLETE") {
		t.Fatalf("history html: %v", err)
	}
}
