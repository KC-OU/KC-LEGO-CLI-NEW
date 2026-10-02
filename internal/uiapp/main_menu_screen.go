package uiapp

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
)

// Admin → Access Control → Customize the main menu: one global layout for
// everyone (see internal/access.Settings.MainMenu and hub.go's
// mainMenuCatalog/hubOptions), not a per-user preference — reorder what's
// already on the top-level menu, drop tabs nobody here uses, or promote a
// specific Admin row (Assign Work, say) straight onto it. Each viewer still
// only ever sees what their own permissions allow; this only changes where
// it appears, never who can reach it.

const scrCustomMenu = "custom_menu"

type customMenuScreen struct {
	base
	row  int
	vals []string // working copy: the chosen, ordered subset of mainMenuCatalog keys
}

func (s *customMenuScreen) PanelID() string { return "MAINMENU" }
func (s *customMenuScreen) Title() string   { return "Customize the Main Menu" }
func (s *customMenuScreen) FKeys() [][2]string {
	return [][2]string{{"F3", "Exit"}, {"F12", "Cancel"}}
}

func (s *customMenuScreen) OnEnter(app *App) {
	app.loadPolicy()
	s.row = 0
	s.vals = append([]string(nil), app.pol().Settings.MainMenu...)
	if len(s.vals) == 0 {
		s.vals = append([]string(nil), defaultMainMenuKeys...)
	}
}

// display is the on-screen order: chosen items first in their chosen
// sequence, then every other catalog item (not currently chosen) after,
// in catalog order — so nothing is ever missing from the list, just ranked.
func (s *customMenuScreen) display() []string {
	out := append([]string(nil), s.vals...)
	chosen := map[string]bool{}
	for _, k := range s.vals {
		chosen[k] = true
	}
	for _, it := range mainMenuCatalog {
		if !chosen[it.key] {
			out = append(out, it.key)
		}
	}
	return out
}

func (s *customMenuScreen) valIndex(key string) int {
	for i, k := range s.vals {
		if k == key {
			return i
		}
	}
	return -1
}

// capacity is how many rows of the (fixed, 16-item) catalog fit under this
// screen's own header/blank/hint lines and the surrounding chrome every
// screen gets — the same budget tableScreen's own capacity uses — so a short
// terminal scrolls the list instead of pushing the hint line off the bottom.
func (s *customMenuScreen) capacity(app *App) int {
	if app.height <= 0 {
		return len(mainMenuCatalog)
	}
	chrome := 8 + 2 + 2 // header band; this screen's own title/blank + hint/blank
	if app.message != "" {
		chrome++
	}
	return max(3, app.height-chrome)
}

func (s *customMenuScreen) Body(app *App) string {
	t := app.theme
	disp := s.display()
	s.row = min(s.row, max(0, len(disp)-1))
	cap := s.capacity(app)
	top, title := 0, "Everyone's top-level menu"
	if len(disp) > cap {
		top = max(0, min(s.row-cap/2, len(disp)-cap))
		title = fmt.Sprintf("Everyone's top-level menu  (%d-%d of %d)", top+1, top+cap, len(disp))
	}
	var b strings.Builder
	b.WriteString(t.Strong.Render(title) + "\n\n")
	for i := top; i < min(top+cap, len(disp)); i++ {
		key := disp[i]
		it := mainMenuItemByKey(key)
		pos, mark, style := "  —", "off", t.Muted
		if vi := s.valIndex(key); vi >= 0 {
			pos, mark, style = fmt.Sprintf("%3d", vi+1), "on ", t.Success
		}
		cell := fmt.Sprintf("%s  %-28s %s", pos, it.label, mark)
		if i == s.row {
			b.WriteString(t.TitleReverse.Render(cell) + "\n")
		} else {
			b.WriteString(style.Render(cell) + "\n")
		}
	}
	b.WriteString("\n" + t.Muted.Render("↑/↓ choose · Space on/off · , move up · . move down · S save · Esc"))
	return b.String()
}

func (s *customMenuScreen) HandleKey(app *App, msg tea.KeyMsg) {
	disp := s.display()
	if len(disp) == 0 {
		return
	}
	switch {
	case msg.Type == tea.KeyUp && s.row > 0:
		s.row--
		return
	case msg.Type == tea.KeyDown && s.row < len(disp)-1:
		s.row++
		return
	case msg.Type == tea.KeySpace || isKey(msg, ' '):
		key := disp[s.row]
		if vi := s.valIndex(key); vi >= 0 {
			s.vals = append(s.vals[:vi], s.vals[vi+1:]...)
		} else {
			s.vals = append(s.vals, key)
		}
		s.followRow(key)
	case isKey(msg, ','):
		key := disp[s.row]
		if vi := s.valIndex(key); vi > 0 {
			s.vals[vi-1], s.vals[vi] = s.vals[vi], s.vals[vi-1]
		}
		s.followRow(key)
	case isKey(msg, '.'):
		key := disp[s.row]
		if vi := s.valIndex(key); vi >= 0 && vi < len(s.vals)-1 {
			s.vals[vi], s.vals[vi+1] = s.vals[vi+1], s.vals[vi]
		}
		s.followRow(key)
	case isKey(msg, 's'):
		vals := append([]string(nil), s.vals...)
		app.saveAccess("main menu customized", func(p *access.Policy) error {
			p.Settings.MainMenu = vals
			return nil
		})
	}
}

// followRow keeps the cursor on the item it was on before a toggle/move
// changed display()'s order out from under it.
func (s *customMenuScreen) followRow(key string) {
	for i, k := range s.display() {
		if k == key {
			s.row = i
			return
		}
	}
}
