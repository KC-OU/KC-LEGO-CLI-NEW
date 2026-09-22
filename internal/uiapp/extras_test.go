package uiapp

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

func TestLegoHubNotesFitATelnetScreen(t *testing.T) {
	app := newTestApp(t)
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 25})
	app.dayPick = &dayPick{day: app.now().Format("2006-01-02"), r: &lego.BuildResult{SetNum: "10696-1", Name: "Medium Creative Brick Box", Percent: 94, Missing: 12}}
	_ = app.legoDB.AddOwnedPart(ownedFor("3001", "Brick", "Bricks"))
	_ = app.legoDB.SetMinQty("3001", 4, "Red", 99)
	app.cur = scrHub
	app.goTo(scrLegoHub)
	v := plain(app.View())
	if rows := strings.Count(v, "\n") + 1; rows > 25 {
		t.Errorf("hub is %d rows tall at 80x25:\n%s", rows, v)
	}
	if !strings.Contains(v, "TODAY") || !strings.Contains(v, "LOW 1") {
		t.Errorf("both notes should show:\n%s", v)
	}
	press(app, "t")
	if app.cur != scrLegoMissing && app.message == "" {
		t.Errorf("T should open the day's set or say why not, on %q", app.cur)
	}
}

func TestAchievementsScreen(t *testing.T) {
	app := newTestApp(t)
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 25})
	_ = app.legoDB.AddOwnedPart(ownedFor("3001", "Brick", "Bricks"))
	app.cur = scrHub
	app.goTo(scrLegoHub)
	press(app, "7")
	press(app, "a")
	v := plain(app.View())
	if app.cur != scrLegoAchievements || !strings.Contains(v, "First Brick") || !strings.Contains(v, "★") {
		t.Errorf("achievements screen:\n%s", v)
	}
	if rows := strings.Count(v, "\n") + 1; rows > 25 {
		t.Errorf("achievements is %d rows tall", rows)
	}
}
