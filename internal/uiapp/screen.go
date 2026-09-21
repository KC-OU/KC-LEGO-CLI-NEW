// Package uiapp is the root bubbletea program: navigation, the undo stack,
// the quick-add drawer, and the 5250 screen registry. It lives outside
// internal/ui to keep that package's rendering primitives free of
// wmsdb/partdb/auth/sync dependencies.
package uiapp

import (
	"fmt"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

// screenModel is the contract every screen implements; App owns the shared
// row 1/2/3-4/23/24 chrome (via ui.Frame) so a screen only supplies its
// body content, field list (if any), and key handling.
type screenModel interface {
	PanelID() string
	Title() string
	OnEnter(app *App)
	HandleKey(app *App, msg tea.KeyMsg)
	Body(app *App) string
	FKeys() [][2]string
	ActiveForm() *ui.FieldList
}

type base struct{}

func (base) FKeys() [][2]string        { return ui.DefaultFKeys }
func (base) ActiveForm() *ui.FieldList { return nil }

// menuOption is one AS/400-style numbered/lettered menu line.
type menuOption struct {
	Key   string
	Label string
	Go    func(app *App)
}

// menuScreen renders "Select one of the following:" and dispatches a single
// keypress matching an option's Key — no box-drawing, no Enter required,
// matching the AS/400 option-menu convention.
type menuScreen struct {
	base
	panelID, title string
	options        func(app *App) []menuOption
	writeGated     bool
	adminGated     bool // requires Permissions.IsAdmin, stricter than writeGated's CanWrite — see App.checkAdmin
	deniedAction   string
	caption        string                // classic style's line above the option list; "Select Option:" when empty
	intro          func(app *App) string // shown under the options when non-empty (the LEGO hub's low-stock badge)
}

func (s *menuScreen) PanelID() string { return s.panelID }
func (s *menuScreen) Title() string   { return s.title }

func (s *menuScreen) OnEnter(app *App) {
	if s.writeGated && !app.checkWrite(s.deniedAction) {
		app.onBack()
		return
	}
	if s.adminGated && !app.checkAdmin(s.deniedAction) {
		app.onBack()
	}
}

func (s *menuScreen) Body(app *App) string {
	body := s.menuBody(app)
	if s.intro != nil {
		if note := s.intro(app); note != "" {
			body += "\n" + note
		}
	}
	return body
}

func (s *menuScreen) menuBody(app *App) string {
	opts := s.options(app)
	rendered := make([][2]string, len(opts))
	for i, o := range opts {
		rendered[i] = [2]string{o.Key, o.Label}
	}
	if !app.theme.Classic {
		return ui.RenderMenu(app.theme, rendered)
	}
	caption, active := s.caption, ""
	if caption == "" {
		caption = "Select Option:"
	}
	if app.cur == scrHub {
		active = app.activeTab
	}
	return ui.RenderClassicMenu(app.theme, caption, rendered, active)
}

// selectLabel is the inline prompt a classic sub-menu ends with, e.g.
// "Select Option [1-3, Q]:", built from the numbered options it actually has.
func (s *menuScreen) selectLabel(app *App) string {
	hi := 0
	var letters []string
	for _, o := range s.options(app) {
		switch k := o.Key; {
		case len(k) == 1 && k[0] >= '1' && k[0] <= '9':
			hi = max(hi, int(k[0]-'0'))
		case len(k) == 1 && strings.ContainsAny(k, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"):
			letters = append(letters, strings.ToUpper(k))
		}
	}
	sort.Strings(letters)
	var opts []string
	switch hi {
	case 0:
	case 1:
		opts = append(opts, "1")
	default:
		opts = append(opts, fmt.Sprintf("1-%d", hi))
	}
	opts = append(opts, letters...)
	opts = append(opts, "Q")
	return "Select Option [" + strings.Join(opts, ", ") + "]:"
}

func (s *menuScreen) HandleKey(app *App, msg tea.KeyMsg) {
	if msg.Type != tea.KeyRunes || len(msg.Runes) != 1 {
		return
	}
	key := strings.ToLower(string(msg.Runes[0]))
	for _, o := range s.options(app) {
		if strings.ToLower(o.Key) == key {
			o.Go(app)
			return
		}
	}
}

// HandleClick is menuScreen's half of touch-mode tap support (see
// App.handleMouse): it triggers whichever option was drawn on bodyRow, the
// same action a keypress on that option's Key would.
func (s *menuScreen) HandleClick(app *App, bodyRow int) {
	for i, o := range s.options(app) {
		if bodyRow == ui.MenuOptionBodyRow(app.theme, i) {
			o.Go(app)
			return
		}
	}
}

// tableScreen is a read-only, plain-columnar data display (no box borders,
// reverse-video heading row) fetched on entry and re-fetchable with
// Enter/R. extra handles any screen-specific single-key action beyond
// refresh (e.g. jumping to a detail lookup).
type tableScreen struct {
	base
	panelID, title string
	columns        []string
	fetch          func(app *App) (rows [][]string, subtitle string, err error)
	extra          func(app *App, msg tea.KeyMsg)
	boxed          bool // classic style only: draws a SQUARE-box grid table, matching the original's Field/Value "record inspector" tables

	all       [][]string // what fetch returned; rows is all narrowed by the "/" filter (see filter.go)
	rows      [][]string
	subtitle  string
	filter    string
	filtering bool
	top       int // first visible row when the list is longer than the screen
}

func (s *tableScreen) PanelID() string { return s.panelID }
func (s *tableScreen) Title() string   { return s.title }

func (s *tableScreen) OnEnter(app *App) {
	s.filter, s.filtering, s.top = "", false, 0
	s.reload(app)
}

// reload fetches again and keeps the current filter.
func (s *tableScreen) reload(app *App) {
	rows, subtitle, err := s.fetch(app)
	s.all, s.subtitle = rows, subtitle
	s.applyFilter()
	if err != nil {
		app.setMsg(err.Error(), true)
	}
}

func (s *tableScreen) Body(app *App) string {
	title := s.subtitle
	if title == "" {
		title = s.title
	}
	rows, pos := s.rows, ""
	lines := strings.Split(title, "\n")
	if c := s.capacity(app) - (len(lines) - 1); len(rows) > c {
		s.top = max(0, min(s.top, len(rows)-c))
		rows = rows[s.top : s.top+c]
		pos = fmt.Sprintf("  rows %d-%d of %d (PgUp/PgDn)", s.top+1, s.top+c, len(s.rows))
	} else {
		s.top = 0
	}
	label := pos + s.filterLabel() // never cut: it is how the person sees a filter or a scroll position
	last := len(lines) - 1
	lines[last] = ansi.Truncate(lines[last], max(app.theme.W()-lipgloss.Width(label), 10), "…") + label
	title = strings.Join(lines, "\n")
	if s.boxed && app.theme.Classic {
		return ui.RenderGrid(app.theme, s.columns, rows, title)
	}
	return ui.RenderColumns(app.theme, s.columns, rows, title)
}

// capacity is how many data rows fit under the header band, title and column heading and above
// the message and prompt. A taller list would scroll the top of the screen off, so it pages.
func (s *tableScreen) capacity(app *App) int {
	if app.height <= 0 {
		return 1 << 30
	}
	chrome := 8 + 3 + 2 // header band, title + blank + column heading, blank + prompt
	if s.boxed && app.theme.Classic {
		chrome += 2 // the grid's top and bottom borders and the rule under the heading
	}
	if app.message != "" {
		chrome++
	}
	return max(3, app.height-chrome)
}

func (s *tableScreen) scroll(app *App, msg tea.KeyMsg) bool {
	c := s.capacity(app)
	switch msg.Type {
	case tea.KeyUp:
		s.top--
	case tea.KeyDown:
		s.top++
	case tea.KeyPgUp:
		s.top -= max(c-1, 1)
	case tea.KeyPgDown:
		s.top += max(c-1, 1)
	case tea.KeyHome:
		s.top = 0
	case tea.KeyEnd:
		s.top = len(s.rows)
	default:
		return false
	}
	s.top = max(0, min(s.top, len(s.rows)-c))
	return true
}

func (s *tableScreen) HandleKey(app *App, msg tea.KeyMsg) {
	if s.handleFilterKey(msg) || s.scroll(app, msg) {
		return
	}
	switch msg.Type {
	case tea.KeyEnter:
		s.reload(app)
		return
	case tea.KeyRunes:
		if len(msg.Runes) == 1 && (msg.Runes[0] == 'r' || msg.Runes[0] == 'R') {
			s.reload(app)
			return
		}
	}
	if s.extra != nil {
		s.extra(app, msg)
	}
}

// formScreen is a field-addressed data-entry screen: Tab/Shift-Tab (or
// Up/Down) move between fields, typing edits the active field in place,
// Enter on the last field submits. writeGated screens re-check
// auth.CheckWritePermission on every entry and bounce back with a denial
// message otherwise, mirroring the original's check_write_permission gate.
type formScreen struct {
	base
	panelID, title string
	writeGated     bool
	deniedAction   string
	build          func(app *App) []ui.Field
	submit         func(app *App, values []string)
	preamble       func(app *App) string // rendered above the fields when non-nil (the classic sign-on cards)
	bare           bool                  // classic style only: no header/legend/prompt frame at all — the pre-login screens draw just preamble + message + prompts

	fl *ui.FieldList
}

func (s *formScreen) PanelID() string { return s.panelID }
func (s *formScreen) Title() string   { return s.title }

func (s *formScreen) OnEnter(app *App) {
	if s.writeGated && !app.checkWrite(s.deniedAction) {
		s.fl = nil
		app.onBack()
		return
	}
	s.fl = ui.NewFieldList(s.build(app)...)
}

func (s *formScreen) Body(app *App) string {
	if s.fl == nil {
		return ""
	}
	if !app.theme.Classic {
		fields := s.fl.Render(app.theme)
		if s.preamble == nil {
			return fields
		}
		return s.preamble(app) + "\n\n" + fields
	}
	var parts []string
	if s.preamble != nil {
		pre := s.preamble(app)
		if !s.bare { // the frame clips a line at the terminal width, so a long hint has to wrap
			pre = lipgloss.NewStyle().Width(app.theme.Width).Render(pre)
		}
		parts = append(parts, pre)
	}
	// Framed screens get their message line from ui.ClassicFrame; a bare
	// screen has no frame, so it shows the status line itself, between the
	// card and the prompts like the original's "✗ Authentication failed!".
	if s.bare && app.message != "" {
		parts = append(parts, ui.Status(app.theme, !app.messageErr, app.message))
	}
	parts = append(parts, s.fl.RenderClassic(app.theme))
	return strings.Join(parts, "\n")
}

func (s *formScreen) ActiveForm() *ui.FieldList { return s.fl }

func (s *formScreen) HandleKey(app *App, msg tea.KeyMsg) {
	if s.fl == nil {
		return
	}
	switch msg.Type {
	case tea.KeyTab, tea.KeyDown:
		s.fl.Next()
	case tea.KeyShiftTab, tea.KeyUp:
		s.fl.Prev()
	case tea.KeyBackspace:
		s.fl.Backspace()
	case tea.KeySpace:
		s.fl.Type(' ')
	case tea.KeyEnter:
		if s.fl.Active == 0 && strings.EqualFold(strings.TrimSpace(s.fl.Value(0)), "q") {
			s.fl.Fields[0].Value = ""
			app.onBack()
			return
		}
		if s.fl.Active == len(s.fl.Fields)-1 {
			s.submit(app, s.fl.Values())
		} else {
			s.fl.Next()
		}
	case tea.KeyRunes:
		for _, r := range msg.Runes {
			s.fl.Type(r)
		}
	}
}
