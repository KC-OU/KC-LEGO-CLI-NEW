package tools

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Choice is what the picker returns: the tool and the answers to its prompts, or nothing if the person quit.
type Choice struct {
	Tool    Tool
	Answers []string
	Quit    bool
}

type mode int

const (
	modeList mode = iota
	modeAsk
	modeConfirm
)

// Picker is a compact inline list (not full screen): a status line, a filter box, up to Rows tools, and a
// hint line. It works at 80x24 and leaves its last frame in the scrollback.
type Picker struct {
	All      []Tool
	State    State
	StatusFn func() string               // slow; run once in the background
	Preview  func(Tool, []string) string // the command about to run, shown when asking for confirmation
	Rows     int

	query    string
	cursor   int
	mode     mode
	pending  Tool
	asked    int
	answers  []string
	input    string
	notice   string
	status   string
	width    int
	height   int
	choice   Choice
	finished bool
	Now      func() time.Time
}

type statusMsg string

var (
	styleDim   = lipgloss.NewStyle().Faint(true)
	styleBold  = lipgloss.NewStyle().Bold(true)
	styleSel   = lipgloss.NewStyle().Reverse(true)
	stylePin   = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	styleRisky = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	styleAsk   = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
)

func (p *Picker) Init() tea.Cmd {
	if p.StatusFn == nil {
		return nil
	}
	fn := p.StatusFn
	return func() tea.Msg { return statusMsg(fn()) }
}

func (p *Picker) visible() []Tool {
	return Filter(p.State.Order(p.All), p.query)
}

func (p *Picker) rows() int {
	r := p.Rows
	if r <= 0 {
		r = 8
	}
	if p.height > 0 {
		r = min(r, max(p.height-5, 3))
	}
	return r
}

func (p *Picker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case statusMsg:
		p.status = string(m)
	case tea.WindowSizeMsg:
		p.width, p.height = m.Width, m.Height
	case tea.KeyMsg:
		switch p.mode {
		case modeAsk:
			return p.askKey(m)
		case modeConfirm:
			return p.confirmKey(m)
		}
		return p.listKey(m)
	}
	return p, nil
}

func (p *Picker) listKey(m tea.KeyMsg) (tea.Model, tea.Cmd) {
	vis := p.visible()
	p.notice = ""
	switch m.Type {
	case tea.KeyCtrlC, tea.KeyEsc:
		p.choice, p.finished = Choice{Quit: true}, true
		return p, tea.Quit
	case tea.KeyCtrlP:
		if len(vis) > 0 {
			id := vis[min(p.cursor, len(vis)-1)].ID
			if p.State.TogglePin(id) {
				p.notice = "pinned"
			} else {
				p.notice = "unpinned"
			}
		}
	case tea.KeyUp:
		p.cursor = max(p.cursor-1, 0)
	case tea.KeyDown:
		p.cursor = min(p.cursor+1, max(len(vis)-1, 0))
	case tea.KeyPgUp:
		p.cursor = max(p.cursor-p.rows(), 0)
	case tea.KeyPgDown:
		p.cursor = min(p.cursor+p.rows(), max(len(vis)-1, 0))
	case tea.KeyHome:
		p.cursor = 0
	case tea.KeyEnd:
		p.cursor = max(len(vis)-1, 0)
	case tea.KeyBackspace:
		if r := []rune(p.query); len(r) > 0 {
			p.query = string(r[:len(r)-1])
			p.cursor = 0
		}
	case tea.KeyCtrlU:
		p.query, p.cursor = "", 0
	case tea.KeyEnter:
		if len(vis) == 0 {
			p.notice = "nothing matches"
			return p, nil
		}
		return p.start(vis[min(p.cursor, len(vis)-1)])
	case tea.KeySpace:
		p.query += " "
		p.cursor = 0
	case tea.KeyRunes:
		s := string(m.Runes)
		if p.query == "" && len(m.Runes) == 1 && m.Runes[0] >= '1' && m.Runes[0] <= '9' {
			if n := int(m.Runes[0] - '0'); n <= min(len(vis), p.rows()) {
				return p.start(vis[n-1])
			}
			return p, nil
		}
		p.query += s
		p.cursor = 0
	}
	return p, nil
}

func (p *Picker) start(t Tool) (tea.Model, tea.Cmd) {
	p.pending, p.asked, p.answers, p.input = t, 0, nil, ""
	switch {
	case len(t.Ask) > 0:
		p.mode = modeAsk
	case t.Risky:
		p.mode = modeConfirm
	default:
		return p.finish()
	}
	return p, nil
}

func (p *Picker) askKey(m tea.KeyMsg) (tea.Model, tea.Cmd) {
	p.notice = ""
	switch m.Type {
	case tea.KeyCtrlC, tea.KeyEsc:
		p.mode = modeList
	case tea.KeyBackspace:
		if r := []rune(p.input); len(r) > 0 {
			p.input = string(r[:len(r)-1])
		}
	case tea.KeyEnter:
		ans, err := Answer(p.input)
		if err != nil {
			p.notice = err.Error()
			return p, nil
		}
		p.answers = append(p.answers, ans)
		p.input = ""
		p.asked++
		if p.asked >= len(p.pending.Ask) {
			if p.pending.Risky {
				p.mode = modeConfirm
				return p, nil
			}
			return p.finish()
		}
	case tea.KeySpace:
		p.input += " "
	case tea.KeyRunes:
		p.input += string(m.Runes)
	}
	return p, nil
}

func (p *Picker) confirmKey(m tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.Type == tea.KeyRunes && len(m.Runes) == 1 && (m.Runes[0] == 'y' || m.Runes[0] == 'Y') {
		return p.finish()
	}
	p.mode = modeList
	p.notice = "cancelled"
	return p, nil
}

func (p *Picker) finish() (tea.Model, tea.Cmd) {
	p.choice, p.finished = Choice{Tool: p.pending, Answers: p.answers}, true
	return p, tea.Quit
}

// Chosen is the result once the program has ended.
func (p *Picker) Chosen() Choice { return p.choice }

func (p *Picker) View() string {
	if p.finished {
		return ""
	}
	w := p.width
	if w <= 0 {
		w = 80
	}
	var b strings.Builder
	status := p.status
	if status == "" {
		status = styleDim.Render("checking…")
	}
	b.WriteString(" " + ansi.Truncate(status, w-2, "…") + "\n")
	b.WriteString(" " + styleDim.Render(strings.Repeat("─", max(w-2, 10))) + "\n")
	switch p.mode {
	case modeAsk:
		b.WriteString(" " + styleBold.Render(p.pending.Title) + "\n\n")
		b.WriteString(" " + styleAsk.Render(p.pending.Ask[p.asked]+": ") + p.input + "█\n")
		if p.notice != "" {
			b.WriteString(" " + styleRisky.Render(p.notice) + "\n")
		}
		b.WriteString("\n " + styleDim.Render("Enter accepts · Esc goes back"))
		return b.String() + "\n"
	case modeConfirm:
		b.WriteString(" " + styleBold.Render(p.pending.Title) + "\n")
		if p.pending.Hint != "" {
			b.WriteString(" " + styleDim.Render(p.pending.Hint) + "\n")
		}
		if p.Preview != nil {
			b.WriteString(" " + styleDim.Render(ansi.Truncate("$ "+p.Preview(p.pending, p.answers), w-2, "…")) + "\n")
		}
		b.WriteString("\n " + styleRisky.Render("Run it? ") + styleBold.Render("y") + " yes · anything else cancels\n")
		return b.String()
	}
	b.WriteString(" " + styleAsk.Render("›") + " " + p.query + "█\n")
	vis := p.visible()
	rows := p.rows()
	cur := min(p.cursor, max(len(vis)-1, 0))
	start := 0
	if cur >= rows {
		start = cur - rows + 1
	}
	if len(vis) == 0 {
		b.WriteString("   " + styleDim.Render("nothing matches") + "\n")
	}
	for i := start; i < min(start+rows, len(vis)); i++ {
		t := vis[i]
		mark := " "
		switch {
		case p.State.IsPinned(t.ID):
			mark = stylePin.Render("★")
		case t.Risky:
			mark = styleRisky.Render("!")
		}
		num := " "
		if p.query == "" && i-start < 9 {
			num = fmt.Sprintf("%d", i-start+1)
		}
		label := fmt.Sprintf("%-8s %s", t.Group, t.Title)
		if t.Hint != "" {
			label += styleDim.Render("  " + t.Hint)
		}
		line := fmt.Sprintf(" %s %s %s", num, mark, ansi.Truncate(label, w-8, "…"))
		if i == cur {
			line = " " + styleSel.Render(ansi.Truncate(fmt.Sprintf("%s %s %s ", num, plainMark(t, p.State), fmt.Sprintf("%-8s %s", t.Group, t.Title)), w-2, "…"))
		}
		b.WriteString(line + "\n")
	}
	hint := "type to filter · Enter runs · Ctrl-P pin · Esc quits"
	if p.notice != "" {
		hint = p.notice
	}
	if len(vis) > rows {
		hint = fmt.Sprintf("%d of %d · ", cur+1, len(vis)) + hint
	}
	b.WriteString(" " + styleDim.Render(ansi.Truncate(hint, w-2, "…")) + "\n")
	return b.String()
}

func plainMark(t Tool, s State) string {
	switch {
	case s.IsPinned(t.ID):
		return "★"
	case t.Risky:
		return "!"
	}
	return " "
}
