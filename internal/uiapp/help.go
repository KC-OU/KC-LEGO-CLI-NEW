package uiapp

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

// The key cheat sheet: F1 anywhere once signed in, or ? on a screen that is
// not a form (on a form a ? is just text). Any key closes it, and that key is
// swallowed, so it can never trigger something by accident.

func (a *App) toggleHelp() {
	if a.authed {
		a.helpOpen = !a.helpOpen
	}
}

// helpKeys is what the current screen's own keys do, beyond the global ones.
func helpKeys(scr screenModel) [][2]string {
	switch s := scr.(type) {
	case *menuScreen:
		return [][2]string{{"1-9, 0", "choose the numbered option (0 = return)"}, {"Up / Down, Enter", "move and choose"}}
	case *tableScreen:
		rows := [][2]string{{"/", "filter the rows: type words, Enter keeps the filter, Esc clears it"}, {"R", "refresh (keeps the filter)"}, {"Q, Esc", "go back"}}
		if s.extra != nil {
			rows = append(rows, [2]string{"other keys", "shown in this screen's title line"})
		}
		return rows
	case *formScreen:
		if s.panelID == "PICK" {
			return [][2]string{{"a number", "choose that item"}, {"part of a name", "narrow the list; one match is chosen for you"}, {"anything else", "kept as typed text where the screen allows it"}}
		}
		return [][2]string{{"Enter", "accept the field and go to the next; on the last one, submit"}, {"Backspace", "erase; on an empty field, go back a field"}, {"Q, Enter", "typed in the first field, cancel the screen (Esc does it from anywhere)"}}
	}
	return nil
}

var globalHelpKeys = [][2]string{
	{"F1  or  ?", "this help (on a form only F1: a ? there is text)"},
	{"Ctrl-K  or  F2", "command palette: type to jump to any screen, part or set"},
	{"F3, F12, Esc, Q", "go back; Q at the main menu signs out"},
	{"F6  or  +", "Quick Add"},
	{"F9  or  U", "undo the last change made in this session"},
	{"F10  or  L", "lock the session (also locks itself when idle)"},
	{"G", "jump between the main menu and LEGO Collection"},
	{"Ctrl+C", "quit"},
}

func (a *App) helpView() string {
	t := a.theme
	var b strings.Builder
	row := func(k, v string) { b.WriteString("  " + t.Strong.Render(padRight(k, 24)) + t.Text.Render(v) + "\n") }
	b.WriteString(t.Muted.Render("On this screen") + "\n")
	if scr := a.screens[a.cur]; scr != nil {
		keys := helpKeys(scr)
		if len(keys) == 0 {
			b.WriteString(t.Muted.Render("  (nothing special here)") + "\n")
		}
		for _, kv := range keys {
			row(kv[0], kv[1])
		}
	}
	b.WriteString("\n" + t.Muted.Render("Everywhere") + "\n")
	for _, kv := range globalHelpKeys {
		row(kv[0], kv[1])
	}
	b.WriteString("\n" + t.Muted.Render("Press any key to close."))
	return ui.RenderPanel(t, t.Brand, "Keys", b.String())
}

func padRight(s string, n int) string {
	if pad := n - len([]rune(s)); pad > 0 {
		return s + strings.Repeat(" ", pad)
	}
	return s + " "
}

// closeHelpOn swallows the key that closes the sheet.
func (a *App) closeHelpOn(msg tea.Msg) bool {
	if !a.helpOpen {
		return false
	}
	switch msg.(type) {
	case tea.KeyMsg, tea.MouseMsg:
		a.helpOpen = false
		return true
	}
	return false
}
