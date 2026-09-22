package uiapp

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

func TestSplashPlaysAndSkips(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	for _, theme := range []string{"half-life", "lego", "matrix", "green"} {
		app := newTestApp(t)
		t.Setenv(config.TUISplash, "1")
		t.Setenv(config.TUITheme, theme)
		app.Update(tea.WindowSizeMsg{Width: 80, Height: 25})
		app.enterHub()
		if app.splashFrame == 0 || app.pendingCmd == nil {
			t.Fatalf("%s: splash did not start", theme)
		}
		for i := 0; i < splashFrames; i++ {
			v := plain(app.View())
			if rows := strings.Count(v, "\n") + 1; rows > 25 {
				t.Errorf("%s frame %d: %d rows", theme, app.splashFrame, rows)
			}
			app.Update(splashTickMsg{})
		}
		if app.splashFrame != 0 || app.cur != scrHub {
			t.Errorf("%s: splash should end on the hub (frame %d, on %q)", theme, app.splashFrame, app.cur)
		}
	}
	app := newTestApp(t)
	t.Setenv(config.TUISplash, "1")
	app.enterHub()
	press(app, "7") // skips, and is not taken as a menu choice
	if app.splashFrame != 0 || app.cur != scrHub {
		t.Errorf("a key should skip the splash and stay on the hub, on %q", app.cur)
	}
	t.Setenv(config.TUISplash, "0")
	app.enterHub()
	if app.splashFrame != 0 {
		t.Error("MODERNWMS_TUI_SPLASH=0 turns it off")
	}
}
