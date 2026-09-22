package uiapp

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/auth"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/wmsdb"
)

func TestThemePickerIsPerUser(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	app := newTestApp(t)
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 25})
	app.cur = scrHub
	app.goTo(scrMyTheme)
	for app.screens[scrMyTheme].(*themeScreen).sel != indexOf(ui.Themes, "half-life") {
		app.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	if v := plain(app.View()); !strings.Contains(v, "HALF-LIFE — preview") {
		t.Fatalf("preview should follow the selection:\n%s", v)
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if app.theme.Name != "half-life" || loadPrefs()["admin"].Theme != "half-life" {
		t.Fatalf("theme %q, prefs %+v", app.theme.Name, loadPrefs())
	}
	if config.Get(config.TUITheme) == "half-life" {
		t.Error("a personal choice must not change everyone's default")
	}

	// Another user signs on with the default; the first gets theirs back.
	app.resetSession()
	app.session = &auth.Session{Username: "bob", Role: "operator", Permissions: &wmsdb.Permissions{CanWrite: true}}
	app.enterHub()
	if app.theme.Name != "green" {
		t.Errorf("bob should get the default theme, got %q", app.theme.Name)
	}
	app.resetSession()
	app.session = &auth.Session{Username: "admin", Role: "admin", Permissions: &wmsdb.Permissions{IsAdmin: true}}
	app.enterHub()
	if app.theme.Name != "half-life" {
		t.Errorf("admin's own theme should come back at sign-on, got %q", app.theme.Name)
	}
}

func TestEveryThemeScreenFits(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	for _, name := range ui.Themes {
		app := newTestApp(t)
		app.Update(tea.WindowSizeMsg{Width: 80, Height: 25})
		app.applyTheme(name)
		for _, id := range []string{scrMyTheme, scrLegoHub, scrLegoAchievements, scrExport} {
			app.cur, app.stack = scrHub, nil
			app.exportJob = ownedExportJob()
			app.goTo(id)
			v := plain(app.View())
			if rows := strings.Count(v, "\n") + 1; rows > 25 {
				t.Errorf("%s/%s: %d rows", name, id, rows)
			}
			for i, l := range strings.Split(v, "\n") {
				if w := len([]rune(l)); w > 80 {
					t.Errorf("%s/%s: row %d is %d wide", name, id, i, w)
				}
			}
		}
	}
}

func indexOf(xs []string, x string) int {
	for i, v := range xs {
		if v == x {
			return i
		}
	}
	return -1
}
