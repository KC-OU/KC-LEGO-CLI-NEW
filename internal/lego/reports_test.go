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

// TestMissingPartsReportTagsEachRowWithItsBagKind confirms the per-bag
// breakdown: a missing row is tagged with the bag its part would go in once
// restocked (its ordinary small/main placement), shown as its own column in
// the HTML report only when at least one row actually carries one.
func TestMissingPartsReportTagsEachRowWithItsBagKind(t *testing.T) {
	d := buildDB(t)
	if err := d.SetBagSize("3023", BagSmall); err != nil {
		t.Fatal(err)
	}
	checkedFireStationWithExtra(t, d, 0) // missing nothing here; just need the small-bag part present
	c, err := d.NewCheck(context.Background(), nil, "1-1", CheckRecount, "kc")
	if err != nil {
		t.Fatal(err)
	}
	for i := range c.Lines {
		if c.Lines[i].PartNum == "3023" {
			c.Lines[i].Have -= 3
		}
	}
	if _, err := d.FinishCheck(c); err != nil {
		t.Fatal(err)
	}
	data, err := d.MissingPartsReport([]string{"1-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Rows) != 1 || data.Rows[0].Bag != "Small Parts" {
		t.Fatalf("rows = %+v, want one row tagged Small Parts", data.Rows)
	}
	html, err := ReportHTML(data)
	if err != nil || !strings.Contains(string(html), "<th>Bag</th>") || !strings.Contains(string(html), "Small Parts") {
		t.Fatalf("report html should show a Bag column: %v\n%s", err, html)
	}
}

// TestReportHTMLOmitsTheBagColumnWhenNoRowHasOne keeps every other report
// (CollectionReport, SetPartsReport, ...) unchanged — the shared template
// only grows a Bag column when a report actually populates one.
func TestReportHTMLOmitsTheBagColumnWhenNoRowHasOne(t *testing.T) {
	d := buildDB(t)
	own(t, d, "3001", 4, "Red", 12)
	data, err := d.CollectionReport()
	if err != nil {
		t.Fatal(err)
	}
	html, err := ReportHTML(data)
	if err != nil || strings.Contains(string(html), "<th>Bag</th>") {
		t.Fatalf("report html should have no Bag column here: %v\n%s", err, html)
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

// checkedFireStationWithExtra finishes a check that records extra (spare) 3001 reds,
// so part_origins has something for ExtraPartsReport to find.
func checkedFireStationWithExtra(t *testing.T, d *DB, extraRed int) {
	t.Helper()
	c, err := d.NewCheck(context.Background(), nil, "1-1", CheckIntake, "alex")
	if err != nil {
		t.Fatal(err)
	}
	for i := range c.Lines {
		if c.Lines[i].PartNum == "3001" {
			c.Lines[i].Extra = extraRed
		}
	}
	if _, err := d.FinishCheck(c); err != nil {
		t.Fatal(err)
	}
}

func TestSetsListReportListsSetsNotLooseParts(t *testing.T) {
	d := buildDB(t)
	if err := d.UpsertSet(Set{SetNum: "1-1", Name: "Fire Station", Theme: "City", Year: 2020, Qty: 1, PartsQty: 100}); err != nil {
		t.Fatal(err)
	}
	data, err := d.SetsListReport()
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Rows) != 0 {
		t.Errorf("a sets list should carry no part rows: %+v", data.Rows)
	}
	if len(data.Sets) == 0 {
		t.Fatal("expected sets from the buildDB fixture")
	}
	found := false
	for _, s := range data.Sets {
		if s.SetNum == "1-1" && s.Name == "Fire Station" {
			found = true
		}
	}
	if !found {
		t.Errorf("Fire Station missing from %+v", data.Sets)
	}
	html, err := SetsListHTML(data)
	if err != nil || !strings.Contains(string(html), "Fire Station") {
		t.Fatalf("html: %v\n%s", err, html)
	}
}

func TestExtraPartsReportGroupsByOriginSet(t *testing.T) {
	d := buildDB(t)
	checkedFireStationWithExtra(t, d, 4) // 4 spare red 3001, from 1-1
	data, err := d.ExtraPartsReport(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Rows) != 1 || data.Rows[0].PartNum != "3001" || data.Rows[0].Qty != 4 {
		t.Fatalf("rows = %+v", data.Rows)
	}
	if !strings.Contains(data.Rows[0].SetNum, "Fire Station") {
		t.Errorf("row should carry the origin set's title, got %q", data.Rows[0].SetNum)
	}
	// filtered to a different set: nothing matches
	filtered, err := d.ExtraPartsReport([]string{"2-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered.Rows) != 0 {
		t.Errorf("filtering to an unrelated set should find nothing: %+v", filtered.Rows)
	}
}

// TestExtraPartsReportTagsRowsWithTheExtraBagKind: every row here came from
// a check's Extra lines, which always land in the dedicated extra bag (see
// RequiredBagKind) -- so every row carries that kind's name, uniformly.
func TestExtraPartsReportTagsRowsWithTheExtraBagKind(t *testing.T) {
	d := buildDB(t)
	checkedFireStationWithExtra(t, d, 4)
	data, err := d.ExtraPartsReport(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Rows) != 1 || data.Rows[0].Bag != "Extra" {
		t.Fatalf("rows = %+v, want one row tagged Extra", data.Rows)
	}
}

func TestOrderListReportGroupsByOrderWithTotals(t *testing.T) {
	d := buildDB(t)
	o := &Order{Supplier: "BrickOwl", Status: "ordered", Currency: "GBP", Shipping: 2.5}
	if err := d.SaveOrder(o); err != nil {
		t.Fatal(err)
	}
	if err := d.AddOrderLine(o.ID, OrderLine{PartNum: "3001", ColorName: "Red", PartName: "Brick 2x4", Qty: 10, UnitPrice: 0.10}); err != nil {
		t.Fatal(err)
	}
	data, err := d.OrderListReport("")
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Rows) != 1 || data.Rows[0].PartNum != "3001" || data.Rows[0].Qty != 10 {
		t.Fatalf("rows = %+v", data.Rows)
	}
	if !strings.Contains(data.Rows[0].SetNum, "BrickOwl") {
		t.Errorf("row should be grouped under the order label, got %q", data.Rows[0].SetNum)
	}
	joined := ""
	for _, f := range data.Facts {
		joined += f[0] + ":" + f[1] + "\n"
	}
	if !strings.Contains(joined, "Total spent:3.50") { // 10*0.10 + 2.50 shipping
		t.Errorf("facts should total spend:\n%s", joined)
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
	hist, err := d.CheckHistory("1-1", "", 10)
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
	all, err := d.CheckHistory("", "", 10)
	if err != nil || len(all) != 2 {
		t.Fatalf("history across all sets: %v %d", err, len(all))
	}
	html, err := CheckHistoryHTML("All checks", hist)
	if err != nil || !strings.Contains(string(html), "sam") || !strings.Contains(string(html), "COMPLETE") {
		t.Fatalf("history html: %v", err)
	}
}

// TestCheckHistoryFiltersByChecker is the "track down a check" search: a
// partial, case-insensitive match on who did it, same LIKE convention as
// SearchSets, so tracking someone down doesn't need their exact username.
func TestCheckHistoryFiltersByChecker(t *testing.T) {
	d := buildDB(t)
	checkedFireStation(t, d, 3) // checked_by "alex"
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

	byAlex, err := d.CheckHistory("", "alex", 10)
	if err != nil || len(byAlex) != 1 || byAlex[0].CheckedBy != "alex" {
		t.Fatalf("filtering by checker 'alex': %v %+v", err, byAlex)
	}
	byPartial, err := d.CheckHistory("", "AL", 10) // case-insensitive, partial
	if err != nil || len(byPartial) != 1 || byPartial[0].CheckedBy != "alex" {
		t.Fatalf("filtering by partial checker 'AL': %v %+v", err, byPartial)
	}
	byNobody, err := d.CheckHistory("", "nobody", 10)
	if err != nil || len(byNobody) != 0 {
		t.Fatalf("filtering by a checker with no checks: %v %+v", err, byNobody)
	}
}
