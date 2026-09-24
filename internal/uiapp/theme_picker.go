package uiapp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

// Each user can pick their own theme (saved in WMS_USER_PREFS_FILE and applied at
// sign-on), so two telnet sessions can look different; the global
// MODERNWMS_TUI_THEME stays the default for everyone who has not chosen. The picker
// previews each theme on a sample panel as you move through the list.

const scrMyTheme = "my_theme"

type userPref struct {
	Theme string `json:"theme,omitempty"`
	Tour  bool   `json:"tour,omitempty"` // has this user seen the first-run tour (see tour.go)?
}

func hasSeenTour(user string) bool { return loadPrefs()[user].Tour }

// markTourSeen sets the tour flag without disturbing the user's theme choice — a
// read-modify-write, not a replace, unlike savePref's "the whole pref is the theme".
func markTourSeen(user string) error {
	path := config.Get(config.UserPrefsFile)
	m := loadPrefs()
	p := m[user]
	p.Tour = true
	m[user] = p
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func loadPrefs() map[string]userPref {
	m := map[string]userPref{}
	if b, err := os.ReadFile(config.Get(config.UserPrefsFile)); err == nil {
		_ = json.Unmarshal(b, &m) // a damaged file is no preferences, not an error at sign-on
	}
	return m
}

// savePref stores one user's theme ("" clears it).
// ponytail: read-modify-write without a lock; two users saving in the same instant can lose one change, add a flock if that ever matters.
func savePref(user, theme string) error {
	path := config.Get(config.UserPrefsFile)
	m := loadPrefs()
	if theme == "" {
		delete(m, user)
	} else {
		m[user] = userPref{Theme: theme}
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// applyTheme switches to the named theme, keeping the layout and width. NO_COLOR
// always wins.
func (a *App) applyTheme(name string) {
	if os.Getenv("NO_COLOR") != "" {
		return
	}
	t := ui.ByName(name)
	t.Classic, t.Width = a.theme.Classic, a.theme.Width
	a.theme = t
}

// applyUserTheme is called at sign-on: the user's own theme, else the global one.
func (a *App) applyUserTheme() {
	name := config.Get(config.TUITheme)
	if a.session != nil {
		if p, ok := loadPrefs()[a.session.Username]; ok && p.Theme != "" {
			name = p.Theme
		}
	}
	a.applyTheme(name)
}

type themeScreen struct {
	base
	admin bool // the Settings entry: may also set everyone's default
	sel   int
}

func (s *themeScreen) PanelID() string {
	if s.admin {
		return "SETTHM"
	}
	return "MYTHM"
}
func (s *themeScreen) Title() string { return "Display Theme" }

func (s *themeScreen) OnEnter(app *App) {
	if s.admin && !app.checkAdmin("SETTINGS_THEME") {
		app.onBack()
		return
	}
	s.sel = 0
	for i, n := range ui.Themes {
		if n == app.theme.Name {
			s.sel = i
		}
	}
}

func (s *themeScreen) FKeys() [][2]string {
	return [][2]string{{"F3", "Exit"}, {"F12", "Cancel"}}
}

func (s *themeScreen) HandleKey(app *App, msg tea.KeyMsg) {
	switch {
	case msg.Type == tea.KeyUp || isKey(msg, 'k'):
		s.sel = (s.sel + len(ui.Themes) - 1) % len(ui.Themes)
	case msg.Type == tea.KeyDown || isKey(msg, 'j'):
		s.sel = (s.sel + 1) % len(ui.Themes)
	case msg.Type == tea.KeyEnter:
		s.save(app, false)
	case s.admin && isKey(msg, 'd'):
		s.save(app, true)
	}
}

func (s *themeScreen) save(app *App, everyone bool) {
	name := ui.Themes[s.sel]
	user, role := "", ""
	if app.session != nil {
		user, role = app.session.Username, app.session.Role
	}
	var err error
	if everyone {
		err = config.SetOverride(config.TUITheme, name)
	} else {
		err = savePref(user, name)
	}
	if err != nil {
		app.setMsg(err.Error(), true)
		return
	}
	app.applyTheme(name)
	if everyone {
		app.audit.Log(user, role, "SETTINGS_CHANGED", "SUCCESS", "tui_theme="+name)
		app.setMsg("Theme "+name+" is now everyone's default (users who picked their own keep theirs).", false)
	} else {
		app.audit.Log(user, role, "USER_THEME", "SUCCESS", name)
		app.setMsg("Your theme is now "+name+".", false)
	}
}

func (s *themeScreen) Body(app *App) string {
	t := app.theme
	var list strings.Builder
	for i, n := range ui.Themes {
		marker, style := "  ", t.Text
		if i == s.sel {
			marker, style = "▶ ", t.Strong
		}
		if n == app.theme.Name {
			n += " (current)"
		}
		list.WriteString(style.Render(marker+n) + "\n")
	}
	keys := "↑/↓ choose   Enter use for me"
	if s.admin {
		keys += "   D default for everyone"
	}
	left := strings.TrimRight(list.String(), "\n")
	preview := themePreview(app, ui.Themes[s.sel])
	body := left
	if t.W() >= 70 {
		body = lipgloss.JoinHorizontal(lipgloss.Top, lipgloss.NewStyle().Width(24).Render(left), preview)
	}
	return body + "\n\n" + t.Muted.Render(keys)
}

// themePreview is a small sample panel drawn in the named theme.
func themePreview(app *App, name string) string {
	p := ui.ByName(name)
	p.Classic, p.Width = app.theme.Classic, app.theme.Width
	if os.Getenv("NO_COLOR") != "" {
		return app.theme.Muted.Render("(NO_COLOR is set: colours are off)")
	}
	w := 44
	pad := func(s string) string { return s + strings.Repeat(" ", max(0, w-lipgloss.Width(s))) }
	lines := []string{
		p.TitleReverse.Render(pad(" " + strings.ToUpper(name) + " — preview")),
		p.Brand.Render("■ ") + p.Strong.Render("KC-PARTS") + p.Muted.Render("  sample panel"),
		p.HeaderReverse.Render(pad(" Part #   Colour       Qty")),
		p.Text.Render(" 3001     Red           12"),
		p.Text.Render(" 3023     Blue           4 ") + p.Warning.Render("LOW"),
		p.Muted.Render(" 3710     (display only)  8"),
		"",
		p.Success.Render(ui.IconOK+" OK") + "   " + p.Warning.Render(ui.IconWarn+" WARN") + "   " + p.Danger.Render(ui.IconFail+" FAIL"),
		p.Text.Render("Qty: ") + p.Accent.Render("  25  "),
		ui.RoleBadge(p, "admin", true) + " " + ui.RoleBadge(p, "operator", true) + " " + ui.RoleBadge(p, "view", false),
		"",
		p.KeyLegend.Render(" F3=Exit ") + " " + p.KeyLegend.Render(" F12=Cancel "),
	}
	return strings.Join(lines, "\n")
}
