package uiapp

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

// An on-screen popup for things worth interrupting whatever screen you're on for —
// today just a set coming up short on a check (see finishCheck, set_check.go) — shown
// on top of the current screen until you press Q. Modelled directly on the F1 help
// overlay (help.go): same takeover-the-display-area mechanism, no new pattern.

type alertMsg struct{ Title, Body string }

// queueAlert adds an alert to the queue, shown as soon as any earlier one (and the
// help overlay, if open) is out of the way. Nothing is ever dropped or replaced.
func (a *App) queueAlert(title, body string) {
	a.popups = append(a.popups, alertMsg{Title: title, Body: body})
}

func (a *App) alertView() string {
	t := a.theme
	al := a.popups[0]
	body := t.Text.Render(al.Body) + "\n\n" + t.Muted.Render("Press Q to dismiss.")
	return ui.RenderPanelRounded(t, t.Danger, al.Title, body)
}

// closeAlertOn swallows Q while an alert is showing (and only Q — unlike the help
// overlay's "any key," since Q is specifically what dismisses this) and pops it off
// the queue. Like closeHelpOn, it only ever reacts to real input (a key or a click):
// every other message (a background job finishing, a tick, ...) must pass straight
// through untouched, or a queued alert would swallow those too.
func (a *App) closeAlertOn(msg tea.Msg) bool {
	if len(a.popups) == 0 || a.helpOpen {
		return false
	}
	switch m := msg.(type) {
	case tea.KeyMsg:
		if isKey(m, 'q') {
			a.popups = a.popups[1:]
		}
		return true // every other key is swallowed too, dismissed or not: the popup is modal
	case tea.MouseMsg:
		return true
	}
	return false
}
