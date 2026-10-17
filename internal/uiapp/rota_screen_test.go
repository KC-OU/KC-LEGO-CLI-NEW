package uiapp

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
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

// TestRotaExtraKeysStartTheRightFlow covers N/O/C's wiring on the rota
// screen itself — which flow each key starts, independent of the
// user-picker step (assignWhoPick's own shared, already-exercised list
// logic) both this and Assign Work build on.
func TestRotaExtraKeysStartTheRightFlow(t *testing.T) {
	app := newTestApp(t)
	scr := rotaViewScreen().(*tableScreen)

	for _, tc := range []struct {
		key  rune
		want string
	}{
		{'n', "Schedule a shift for:"},
		{'o', "Quick override (emergency cover) today for:"},
		{'c', "Clear whose schedule:"},
	} {
		scr.extra(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{tc.key}})
		if app.pick == nil || app.pick.Header != tc.want {
			t.Errorf("key %q: expected pick header %q, got %+v", tc.key, tc.want, app.pick)
		}
	}
}

func TestRotaAdminSetsAFullShiftThroughTheChainedPrompts(t *testing.T) {
	app := newTestApp(t)
	today := time.Now().Format("2006-01-02")

	rotaSetShiftDate(app, "dave")
	pickSubmit(app, today)
	pickSubmit(app, "09:00")
	pickSubmit(app, "17:00")
	pickSubmit(app, "covering lates")

	if app.messageErr {
		t.Fatalf("unexpected error message after setting the shift")
	}
	rota, err := app.legoDB.GetRota("dave", today)
	if err != nil {
		t.Fatal(err)
	}
	if rota == nil || rota.StartTime != "09:00" || rota.EndTime != "17:00" || rota.Note != "covering lates" {
		t.Errorf("expected dave's shift to be saved, got %+v", rota)
	}
	if app.cur != scrRotaView {
		t.Errorf("expected to land back on the rota view, got %q", app.cur)
	}
}

func TestRotaAdminSetShiftDefaultsBlankDateToToday(t *testing.T) {
	app := newTestApp(t)
	today := time.Now().Format("2006-01-02")

	rotaSetShiftDate(app, "dave")
	pickSubmit(app, "") // blank date -> today
	pickSubmit(app, "")
	pickSubmit(app, "")
	pickSubmit(app, "")

	rota, err := app.legoDB.GetRota("dave", today)
	if err != nil {
		t.Fatal(err)
	}
	if rota == nil {
		t.Errorf("expected a blank date to default to today")
	}
}

func TestRotaAdminQuickOverrideCoversTodayAsEmergencyCover(t *testing.T) {
	app := newTestApp(t)
	today := time.Now().Format("2006-01-02")

	rotaQuickOverride(app, "dave")

	rota, err := app.legoDB.GetRota("dave", today)
	if err != nil {
		t.Fatal(err)
	}
	if rota == nil || !rota.EmergencyOverride {
		t.Errorf("expected today's entry to be an emergency-cover override, got %+v", rota)
	}
	if app.cur != scrRotaView {
		t.Errorf("expected to land back on the rota view, got %q", app.cur)
	}
}

func TestRotaAdminClearsADayThroughThePrompt(t *testing.T) {
	app := newTestApp(t)
	today := time.Now().Format("2006-01-02")
	if err := app.legoDB.SetRota("dave", today, "09:00", "17:00", "", "admin"); err != nil {
		t.Fatal(err)
	}

	rotaClearDate(app, "dave")
	pickSubmit(app, today)

	rota, err := app.legoDB.GetRota("dave", today)
	if err != nil {
		t.Fatal(err)
	}
	if rota != nil {
		t.Errorf("expected dave's schedule to be cleared, still got %+v", rota)
	}
}
