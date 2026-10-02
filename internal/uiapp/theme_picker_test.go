package uiapp

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/auth"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
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

func TestThemeRequestLogsAnAdminEventAndNotifies(t *testing.T) {
	app := newTestApp(t)
	app.goTo(scrThemeRequest)
	app.screens[scrThemeRequest].(*formScreen).submit(app, []string{"Solarized", "https://ethanschoonover.com/solarized/"})

	evs, err := app.legoDB.AdminEvents(10)
	if err != nil || len(evs) != 1 || evs[0].Kind != lego.EventThemeRequest {
		t.Fatalf("AdminEvents = %+v, %v, want one theme_request event", evs, err)
	}
	if !strings.Contains(evs[0].Detail, "Solarized") || !strings.Contains(evs[0].Detail, "ethanschoonover") {
		t.Errorf("detail = %q, want the name and link", evs[0].Detail)
	}
	if evs[0].Actor != "admin" {
		t.Errorf("actor = %q, want the signed-in user", evs[0].Actor)
	}
	if !auditHas(t, "THEME_REQUESTED") {
		t.Error("expected an audit log entry")
	}
}

func TestThemeRequestRequiresAName(t *testing.T) {
	app := newTestApp(t)
	app.goTo(scrThemeRequest)
	app.screens[scrThemeRequest].(*formScreen).submit(app, []string{"", "https://example.com"})
	if !app.messageErr {
		t.Error("a blank name should be refused")
	}
	if evs, _ := app.legoDB.AdminEvents(10); len(evs) != 0 {
		t.Errorf("nothing should be logged on refusal, got %+v", evs)
	}
}
