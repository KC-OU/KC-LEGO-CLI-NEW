package uiapp

import (
	"strings"
	"testing"
	"time"
)

func TestRotaViewScreenShowsTheNextSevenDays(t *testing.T) {
	app := newTestApp(t)

	today := time.Now().Format("2006-01-02")
	tomorrow := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	if err := app.legoDB.SetRota("dave", today, "09:00", "17:00", "", "admin"); err != nil {
		t.Fatal(err)
	}
	if err := app.legoDB.QuickNSOverride("sam", tomorrow, "admin"); err != nil {
		t.Fatal(err)
	}

	scr := rotaViewScreen().(*tableScreen)
	rows, subtitle, err := scr.fetch(app)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(subtitle, "2 shift(s)") {
		t.Errorf("expected the subtitle to count both scheduled entries, got %q", subtitle)
	}

	var foundDave, foundSamOverride, foundEmptyDay bool
	for _, r := range rows {
		if len(r) < 5 {
			continue
		}
		if r[0] == today+" (today)" && r[1] == "dave" && r[2] == "09:00" && r[3] == "17:00" {
			foundDave = true
		}
		if r[1] == "sam" && strings.Contains(r[4], "emergency cover") {
			foundSamOverride = true
		}
		if r[1] == "—" {
			foundEmptyDay = true
		}
	}
	if !foundDave {
		t.Errorf("expected today's row for dave, got %+v", rows)
	}
	if !foundSamOverride {
		t.Errorf("expected tomorrow's emergency-cover row for sam, got %+v", rows)
	}
	if !foundEmptyDay {
		t.Errorf("expected at least one day with nobody scheduled to show a placeholder row, got %+v", rows)
	}
	if len(rows) != 7 { // today's dave row + tomorrow's sam row + 5 empty-day placeholders
		t.Errorf("expected 7 days' worth of rows (2 scheduled + 5 empty), got %d: %+v", len(rows), rows)
	}
}
