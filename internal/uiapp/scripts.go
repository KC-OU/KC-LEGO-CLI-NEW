package uiapp

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/tools"
)

// toolDoneMsg reports a tool that ran under tea.ExecProcess.
type toolDoneMsg struct {
	ID  string
	Err error
}

// toolWrapper clears the screen, runs the tool, then waits for Enter so its output stays until it has been read.
const toolWrapper = `printf '\033[2J\033[H'; "$@"; rc=$?; printf '\n[exit %s] press Enter to return ' "$rc"; read _`

// toolCommand is the command the Script Hub runs for t.
func toolCommand(t tools.Tool) (*exec.Cmd, error) {
	self, _ := os.Executable()
	argv, err := tools.Expand(t, self, "", nil)
	if err != nil {
		return nil, err
	}
	return exec.Command("/bin/sh", append([]string{"-c", toolWrapper, "sh"}, argv...)...), nil
}

// scriptsHubScreen is the Script Hub: the same catalogue as the `wms menu` launcher, minus what must not
// run inside a telnet or web session (deploys, restarts, log followers, anything that asks questions).
// Enter runs the highlighted tool with the terminal handed over to it; risky tools ask first, admin-only
// tools need an administrator, and every run is audited.
type toolsScreen struct {
	base
	all       []tools.Tool
	rows      []tools.Tool
	state     tools.State
	cursor    int
	filter    string
	filtering bool
	confirm   *tools.Tool
	loadNote  string
}

func scriptsHubScreen() screenModel { return &toolsScreen{} }

func (s *toolsScreen) PanelID() string { return "SCRIPT" }
func (s *toolsScreen) Title() string   { return "Script Hub" }

func (s *toolsScreen) capturing() bool { return s.filtering }

func (s *toolsScreen) OnEnter(app *App) {
	list, err := tools.Load()
	s.loadNote = ""
	if err != nil {
		s.loadNote = err.Error()
	}
	s.all = s.all[:0]
	for _, t := range list {
		if t.In(tools.TUI) && len(t.Ask) == 0 {
			s.all = append(s.all, t)
		}
	}
	s.state = tools.LoadState(tools.StatePath())
	s.filter, s.filtering, s.cursor, s.confirm = "", false, 0, nil
	s.apply()
}

func (s *toolsScreen) apply() {
	s.rows = tools.Filter(s.state.Order(s.all), s.filter)
	s.cursor = max(0, min(s.cursor, len(s.rows)-1))
}

func (s *toolsScreen) capacity(app *App) int {
	if app.height <= 0 {
		return 1 << 30
	}
	chrome := 8 + 3 + 2 // header band, title lines and the blank under them, blank and prompt
	if app.message != "" {
		chrome++
	}
	return max(3, app.height-chrome)
}

func (s *toolsScreen) Body(app *App) string {
	t := app.theme
	title := fmt.Sprintf("%d tool(s) · Enter runs · / filters · Ctrl-P pins", len(s.rows))
	if s.filtering || s.filter != "" {
		cursor := ""
		if s.filtering {
			cursor = "_"
		}
		title = fmt.Sprintf("[/ %s%s : %d of %d]", s.filter, cursor, len(s.rows), len(s.all))
	}
	var b strings.Builder
	b.WriteString(t.Brand.Render(ansi.Truncate(title, t.W(), "…")) + "\n")
	if s.loadNote != "" {
		b.WriteString(t.Warning.Render(ansi.Truncate("tools.json ignored: "+s.loadNote, t.W(), "…")) + "\n")
	} else {
		b.WriteString("\n")
	}
	if len(s.rows) == 0 {
		return b.String() + t.Muted.Render("nothing matches")
	}
	capRows := s.capacity(app) - 1
	start := 0
	if s.cursor >= capRows {
		start = s.cursor - capRows + 1
	}
	for i := start; i < min(start+capRows, len(s.rows)); i++ {
		tl := s.rows[i]
		mark := " "
		switch {
		case s.state.IsPinned(tl.ID):
			mark = "★"
		case tl.Risky:
			mark = "!"
		}
		hint := ""
		if tl.Hint != "" {
			hint = "  " + tl.Hint
		}
		line := ansi.Truncate(fmt.Sprintf(" %s %-8s %s%s", mark, tl.Group, tl.Title, hint), t.W()-1, "…")
		if i == s.cursor {
			line = t.TitleReverse.Render(ansi.Truncate(fmt.Sprintf("> %s %-8s %s", mark, tl.Group, tl.Title), t.W()-1, "…"))
		}
		b.WriteString(line + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (s *toolsScreen) selected() *tools.Tool {
	if s.cursor < 0 || s.cursor >= len(s.rows) {
		return nil
	}
	return &s.rows[s.cursor]
}

func (s *toolsScreen) HandleKey(app *App, msg tea.KeyMsg) {
	if s.confirm != nil {
		t := *s.confirm
		s.confirm = nil
		app.setMsg("", false)
		if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && (msg.Runes[0] == 'y' || msg.Runes[0] == 'Y') {
			s.run(app, t)
		} else {
			app.setMsg("Cancelled.", false)
		}
		return
	}
	if s.filtering {
		switch msg.Type {
		case tea.KeyEnter:
			s.filtering = false
		case tea.KeyEsc:
			s.filtering, s.filter = false, ""
		case tea.KeyBackspace:
			if r := []rune(s.filter); len(r) > 0 {
				s.filter = string(r[:len(r)-1])
			}
		case tea.KeySpace:
			s.filter += " "
		case tea.KeyRunes:
			s.filter += string(msg.Runes)
		}
		s.cursor = 0
		s.apply()
		return
	}
	c := s.capacity(app)
	switch msg.Type {
	case tea.KeyUp:
		s.cursor--
	case tea.KeyDown:
		s.cursor++
	case tea.KeyPgUp:
		s.cursor -= max(c-2, 1)
	case tea.KeyPgDown:
		s.cursor += max(c-2, 1)
	case tea.KeyHome:
		s.cursor = 0
	case tea.KeyEnd:
		s.cursor = len(s.rows) - 1
	case tea.KeyCtrlP:
		if t := s.selected(); t != nil {
			s.state.TogglePin(t.ID)
			_ = s.state.Save(tools.StatePath())
			s.apply()
		}
	case tea.KeyEnter:
		if t := s.selected(); t != nil {
			s.start(app, *t)
		}
	case tea.KeyRunes:
		if len(msg.Runes) == 1 && msg.Runes[0] == '/' {
			s.filtering = true
		}
	}
	s.cursor = max(0, min(s.cursor, len(s.rows)-1))
}

func (s *toolsScreen) start(app *App, t tools.Tool) {
	if t.Admin && !app.checkAdmin("script hub: "+t.ID) {
		return
	}
	if t.Risky {
		s.confirm = &t
		app.setMsg(fmt.Sprintf("Run %q? Press Y to confirm, any other key cancels.", t.Title), false)
		return
	}
	s.run(app, t)
}

func (s *toolsScreen) run(app *App, t tools.Tool) {
	cmd, err := toolCommand(t)
	if err != nil {
		app.setMsg(err.Error(), true)
		return
	}
	user, role := "", ""
	if app.session != nil {
		user, role = app.session.Username, app.session.Role
	}
	app.audit.Log(user, role, "TOOL_RUN", "STARTED", "tool="+t.ID+" via=script-hub")
	s.state.Record(t.ID, time.Now())
	_ = s.state.Save(tools.StatePath())
	id := t.ID
	app.pendingCmd = tea.ExecProcess(cmd, func(err error) tea.Msg { return toolDoneMsg{ID: id, Err: err} })
}

func auditLogScreen() screenModel {
	return &tableScreen{
		panelID: "AUDLOG",
		title:   "Security Audit Log (last 20)",
		columns: []string{"Entry"},
		fetch: func(app *App) ([][]string, string, error) {
			lines, err := app.audit.Tail(20)
			if err != nil {
				return nil, "", err
			}
			rows := make([][]string, len(lines))
			for i, l := range lines {
				rows[i] = []string{l}
			}
			return rows, "", nil
		},
	}
}
