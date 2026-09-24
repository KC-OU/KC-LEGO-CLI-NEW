package uiapp

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/auth"
)

func TestFirstLoginGoesThroughTourThenSkipsNextTime(t *testing.T) {
	app := newTestApp(t)
	session := &auth.Session{Source: "partdb", Username: "newuser", Role: "viewer"}

	app.session = session
	proceedPastAuth(app, session)
	if app.cur != scrTour {
		t.Fatalf("a first-ever login should land on the tour, got %q", app.cur)
	}

	// finish it (as if Enter was pressed through every page)
	for i := 0; i < len(tourPages); i++ {
		app.screens[scrTour].HandleKey(app, tea.KeyMsg{Type: tea.KeyEnter})
	}
	if app.cur != scrHub {
		t.Fatalf("finishing the tour should land on the hub, got %q", app.cur)
	}
	if !hasSeenTour("newuser") {
		t.Fatal("finishing the tour should mark it seen")
	}

	// a second sign-in skips straight past it
	app.cur, app.tourPage = scrLogin, 0
	proceedPastAuth(app, session)
	if app.cur != scrHub {
		t.Fatalf("a returning user should skip the tour, got %q", app.cur)
	}
}

func TestSkippingTheTourAlsoMarksItSeen(t *testing.T) {
	app := newTestApp(t)
	session := &auth.Session{Source: "partdb", Username: "skipper", Role: "viewer"}
	app.session = session
	proceedPastAuth(app, session)
	if app.cur != scrTour {
		t.Fatalf("expected the tour, got %q", app.cur)
	}
	app.screens[scrTour].HandleKey(app, tea.KeyMsg{Type: tea.KeyEsc})
	if app.cur != scrHub || !hasSeenTour("skipper") {
		t.Fatalf("Esc should skip straight to the hub and still mark it seen: cur=%q seen=%v", app.cur, hasSeenTour("skipper"))
	}
}

func TestMarkTourSeenPreservesAnExistingThemeChoice(t *testing.T) {
	newTestApp(t) // sets up config.UserPrefsFile in a scratch dir
	if err := savePref("alex", "amber"); err != nil {
		t.Fatal(err)
	}
	if err := markTourSeen("alex"); err != nil {
		t.Fatal(err)
	}
	p := loadPrefs()["alex"]
	if !p.Tour || p.Theme != "amber" {
		t.Fatalf("marking the tour seen must not lose the theme choice: %+v", p)
	}
}
