package uiapp

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// A short first-run tour: the first time an account ever signs in, 3 static pages
// before the hub instead of dropping straight in. Skippable at any point (Esc/Q),
// which counts the same as finishing it — the point is orientation, not a quiz, and
// it should never nag a second time.

type tourPage struct{ Title, Body string }

var tourPages = []tourPage{
	{
		Title: "Welcome",
		Body: "This is the hub — every area of the app is one number away.\n\n" +
			"Type a number to go there. 0 or Q goes back a screen (or signs out at the hub).",
	},
	{
		Title: "Getting around",
		Body: "F1 or ?     this screen's help, anywhere\n" +
			"Ctrl-K / F2 command palette — type to jump straight to any screen, part or set\n" +
			"F9 or U     undo the last change you made this session\n" +
			"F10 or L    lock the session",
	},
	{
		Title: "Where to start",
		Body: "LEGO Collection has everything for your sets and parts.\n" +
			"Inside it, Set Workshop is where stock checks, missing parts, orders and printable\n" +
			"reports all live.\n\n" +
			"Press Enter to go to the hub.",
	},
}

func tourScreen() screenModel { return &tourScreenImpl{} }

type tourScreenImpl struct{ base }

func (s *tourScreenImpl) PanelID() string { return "TOUR" }
func (s *tourScreenImpl) Title() string   { return tourPages[0].Title } // overridden per-page in Body
func (s *tourScreenImpl) OnEnter(app *App) {
	app.tourPage = 0
}

func (s *tourScreenImpl) FKeys() [][2]string {
	return [][2]string{{"Enter", "Next"}, {"Esc", "Skip"}}
}

func (s *tourScreenImpl) HandleKey(app *App, msg tea.KeyMsg) {
	switch {
	case msg.Type == tea.KeyEsc, isKey(msg, 'q'):
		finishTour(app)
	case msg.Type == tea.KeyEnter:
		if app.tourPage >= len(tourPages)-1 {
			finishTour(app)
			return
		}
		app.tourPage++
	}
}

func (s *tourScreenImpl) Body(app *App) string {
	t := app.theme
	p := tourPages[app.tourPage]
	header := t.Strong.Render(fmt.Sprintf("%s (%d/%d)", p.Title, app.tourPage+1, len(tourPages)))
	return header + "\n\n" + t.Text.Render(p.Body)
}

func finishTour(app *App) {
	if app.session != nil {
		_ = markTourSeen(app.session.Username)
	}
	app.enterHub()
}
