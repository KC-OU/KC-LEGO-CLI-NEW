package uiapp

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

// Achievements (collection milestones) and Set of the Day (a set your loose parts
// nearly cover, the same for everyone on a given day) — both worked out from the
// collection as it is, so nothing new is stored.

const scrLegoAchievements = "lego_achievements"

type achievementsScreen struct{ base }

func (s *achievementsScreen) PanelID() string                    { return "LEGACH" }
func (s *achievementsScreen) Title() string                      { return "Achievements" }
func (s *achievementsScreen) OnEnter(app *App)                   {}
func (s *achievementsScreen) HandleKey(app *App, msg tea.KeyMsg) {}

func (s *achievementsScreen) Body(app *App) string {
	t := app.theme
	list, err := app.legoDB.Achievements(strings.ToUpper(config.Get(config.BricklinkCondition)))
	if err != nil {
		return ui.Status(t, false, err.Error())
	}
	got := 0
	lines := make([]string, 0, len(list)+2)
	for _, a := range list {
		if a.Got() {
			got++
		}
		lines = append(lines, ansi.Truncate(ui.AchievementLine(t, a.Name, a.Desc, a.Have, a.Goal), t.W(), "…"))
	}
	head := t.Strong.Render(fmt.Sprintf("%d of %d unlocked", got, len(list))) + t.Muted.Render("  — milestones from your collection as it is now")
	return head + "\n\n" + strings.Join(lines, "\n")
}

// setOfTheDay is cached per day: the pick is fixed for the day, and working it out
// scans the catalog.
type dayPick struct {
	day string
	r   *lego.BuildResult
}

func (app *App) setOfTheDay() *lego.BuildResult {
	today := app.now().Format("2006-01-02")
	if app.dayPick == nil || app.dayPick.day != today {
		r, _ := app.legoDB.SetOfTheDay(app.now())
		app.dayPick = &dayPick{day: today, r: r}
	}
	return app.dayPick.r
}

func setOfTheDayNote(app *App) string {
	r := app.setOfTheDay()
	if r == nil {
		return ""
	}
	return app.theme.Accent.Render(" TODAY ") + app.theme.Text.Render(fmt.Sprintf(" %s %s — you hold %d%%, %d piece(s) short. Press T", r.SetNum, r.Name, r.Percent, r.Missing))
}

// legoHubIntro is the LEGO hub's notes: low stock, then the day's build challenge.
// A short window (a 24- or 25-row telnet screen) gets them as one compact line, so
// the menu still fits.
func legoHubIntro(app *App) string {
	low, today := lowStockNote(app), setOfTheDayNote(app)
	if low == "" || today == "" {
		return low + today
	}
	if app.height == 0 || app.height >= 27 {
		return low + "\n" + today
	}
	t := app.theme
	n, _ := app.legoDB.LowStock()
	r := app.setOfTheDay()
	return t.Warning.Render(fmt.Sprintf(" LOW %d ", len(n))) + t.Muted.Render(" (6, then / low)  ") +
		t.Accent.Render(" TODAY ") + t.Text.Render(fmt.Sprintf(" %s %d%%, %d short — T", r.SetNum, r.Percent, r.Missing))
}

func openSetOfTheDay(app *App) {
	if r := app.setOfTheDay(); r != nil {
		showMissing(app, r.SetNum)
		return
	}
	app.setMsg("No set is 90% covered by your loose parts yet — B shows the closest.", false)
}
