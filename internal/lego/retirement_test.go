package lego

import (
	"strings"
	"testing"
	"time"
)

const sampleRetirementCSV = `Theme,Subtheme,Set #,Set Name,Age,Piece Count,Retirement Date,Notes: (Exclusivity, release, etc.),LEGO.com Link
Icons,Modular,10297,Boutique Hotel,18+,3066,Dec 31 2026,—,https://lego.com/10297
Star Wars,Ships,75192,Millennium Falcon,16+,7541,—,—,https://lego.com/75192
,,,,,,,,
Technic,,42151,McLaren Formula 1,18+,1432,"Jan 5, 2027",US/CA only,https://lego.com/42151
`

func TestParseRetirementCSVReadsByHeaderName(t *testing.T) {
	rows, err := ParseRetirementCSV(strings.NewReader(sampleRetirementCSV))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows (the blank row skipped), got %d: %+v", len(rows), rows)
	}
	if rows[0].SetNum != "10297" || rows[0].Theme != "Icons" || rows[0].RetiresAt.IsZero() {
		t.Errorf("row 0 = %+v", rows[0])
	}
	if !rows[1].RetiresAt.IsZero() {
		t.Errorf("row 1 (—) should have an unknown date, got %v", rows[1].RetiresAt)
	}
	if rows[2].RetiresAt.Format("2006-01-02") != "2027-01-05" {
		t.Errorf("row 2 date = %v, want 2027-01-05", rows[2].RetiresAt)
	}
}

// TestParseRetirementCSVSkipsLeadingJunkRows matches the live Brick Tap export's actual
// shape: a blank row, then title/attribution rows, before the real header — row 1 is not
// reliably the header.
func TestParseRetirementCSVSkipsLeadingJunkRows(t *testing.T) {
	body := ",,,,,,\n" +
		"Last update: September 21, 2026,,,,,,\n" +
		"Sheet maintained by someone,,,,,,\n" +
		"Theme,Subtheme,Set #,Set Name,Age,Piece Count,Retirement Date\n" +
		"Icons,Modular,10297,Boutique Hotel,18+,3066,\"Dec 31, 2026\"\n"
	rows, err := ParseRetirementCSV(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].SetNum != "10297" || rows[0].Name != "Boutique Hotel" {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestParseRetirementCSVMissingSetColumnErrors(t *testing.T) {
	if _, err := ParseRetirementCSV(strings.NewReader("Theme,Notes\nIcons,x\n")); err == nil {
		t.Error("a header with no Set # column should error, not silently import nothing useful")
	}
}

func TestParseRetirementCSVEmptyInput(t *testing.T) {
	rows, err := ParseRetirementCSV(strings.NewReader(""))
	if err != nil || rows != nil {
		t.Errorf("empty input: rows=%v err=%v", rows, err)
	}
}

func TestImportAndRetirementFor(t *testing.T) {
	d := openScratchDB(t)
	rows, err := ParseRetirementCSV(strings.NewReader(sampleRetirementCSV))
	if err != nil {
		t.Fatal(err)
	}
	n, err := d.ImportRetirements(rows)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("imported %d, want 3", n)
	}
	r, err := d.RetirementFor("10297")
	if err != nil || r == nil || r.Name != "Boutique Hotel" {
		t.Fatalf("RetirementFor(10297) = %+v, %v", r, err)
	}
	if r, err := d.RetirementFor("99999"); err != nil || r != nil {
		t.Fatalf("unknown set should return nil, nil: %+v, %v", r, err)
	}
	// a second import fully replaces the first (the sheet is the source of truth)
	if _, err := d.ImportRetirements([]RetirementInfo{{SetNum: "10297", Name: "Renamed"}}); err != nil {
		t.Fatal(err)
	}
	r, _ = d.RetirementFor("10297")
	if r.Name != "Renamed" {
		t.Errorf("after re-import: %+v", r)
	}
	if r, _ := d.RetirementFor("75192"); r != nil {
		t.Error("75192 should be gone after the re-import replaced the whole table")
	}
}

// TestImportToleratesDuplicateSetNumbers matches the real Brick Tap sheet, which lists
// the same set more than once (one row per region/exclusivity note) — the import must
// not fail on that, and the last row for a set should be what's stored.
func TestImportToleratesDuplicateSetNumbers(t *testing.T) {
	d := openScratchDB(t)
	rows := []RetirementInfo{
		{SetNum: "10297", Name: "Boutique Hotel (US)"},
		{SetNum: "10297", Name: "Boutique Hotel (EU)"},
	}
	n, err := d.ImportRetirements(rows)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("distinct count = %d, want 1", n)
	}
	r, err := d.RetirementFor("10297")
	if err != nil || r == nil || r.Name != "Boutique Hotel (EU)" {
		t.Fatalf("RetirementFor(10297) = %+v, %v", r, err)
	}
}

func TestRetiringSoonFiltersAndFlagsOwnedAndWatched(t *testing.T) {
	d := openScratchDB(t)
	soon := time.Now().AddDate(0, 0, 10).Format("Jan 2 2006")
	far := time.Now().AddDate(2, 0, 0).Format("Jan 2 2006")
	csv := "Set #,Set Name,Retirement Date\n" +
		"10297,Boutique Hotel," + soon + "\n" +
		"75192,Millennium Falcon," + far + "\n" +
		"99999,No Date,—\n"
	rows, err := ParseRetirementCSV(strings.NewReader(csv))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.ImportRetirements(rows); err != nil {
		t.Fatal(err)
	}
	if err := d.UpsertSet(Set{SetNum: "10297", Name: "Boutique Hotel", Qty: 1}); err != nil {
		t.Fatal(err)
	}
	if err := d.AddWatch(Watch{ItemType: "SET", ItemNo: "75192", ColorID: -1, MaxPrice: 500}); err != nil {
		t.Fatal(err)
	}

	list, err := d.RetiringSoon(30 * 24 * time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].SetNum != "10297" {
		t.Fatalf("expected only 10297 within 30 days (75192 is 2 years out, 99999 undated), got %+v", list)
	}
	if !list[0].Owned || list[0].Watching {
		t.Errorf("10297 should be flagged owned, not watching: %+v", list[0])
	}

	longList, err := d.RetiringSoon(3 * 365 * 24 * time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(longList) != 2 {
		t.Fatalf("expected 2 dated sets within 3 years (99999 stays excluded, undated), got %+v", longList)
	}
	var falcon *RetiringSet
	for i := range longList {
		if longList[i].SetNum == "75192" {
			falcon = &longList[i]
		}
	}
	if falcon == nil || !falcon.Watching || falcon.Owned {
		t.Errorf("75192 should be flagged watching, not owned: %+v", falcon)
	}
}
