package preflight

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var (
	green  = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	yellow = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	red    = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	dim    = lipgloss.NewStyle().Faint(true)
	bold   = lipgloss.NewStyle().Bold(true)
)

func icon(s Status) string {
	switch s {
	case OK:
		return green.Render("✓")
	case Warn:
		return yellow.Render("!")
	case Fail:
		return red.Render("✗")
	}
	return dim.Render("·")
}

func line(o Outcome, width int) string {
	name := o.Check.Name
	nameW := min(max(width*2/5, 20), 46)
	if lipgloss.Width(name) > nameW {
		name = ansi.Truncate(name, nameW, "…")
	}
	head := fmt.Sprintf(" %s %-8s %s", icon(o.Result.Status), o.Check.Group, name+strings.Repeat(" ", nameW-lipgloss.Width(name)))
	tail := fmt.Sprintf("%.1fs", o.Duration.Seconds())
	room := width - lipgloss.Width(head) - lipgloss.Width(tail) - 3
	detail := ""
	if room > 6 {
		detail = " " + dim.Render(ansi.Truncate(o.Result.Detail, room, "…"))
	}
	pad := max(width-lipgloss.Width(head)-lipgloss.Width(detail)-lipgloss.Width(tail)-1, 1)
	return head + detail + strings.Repeat(" ", pad) + dim.Render(tail)
}

// PlainProgress prints one line per finished check (no colour codes when colour is false).
func PlainProgress(w io.Writer, width int) func(Event) {
	return func(ev Event) {
		if !ev.Done {
			return
		}
		l := line(ev.Outcome, width)
		fmt.Fprintln(w, l)
	}
}

// Report prints what needs attention and the verdict, after the run.
func Report(w io.Writer, outs []Outcome, v Verdict, width int) {
	var bad, warn []Outcome
	for _, o := range outs {
		switch o.Result.Status {
		case Fail:
			bad = append(bad, o)
		case Warn:
			warn = append(warn, o)
		}
	}
	wrap := lipgloss.NewStyle().Width(max(width-6, 30))
	if len(bad) > 0 {
		fmt.Fprintln(w, "\n"+red.Render(bold.Render(" Must fix")))
		for _, o := range bad {
			fmt.Fprintf(w, "  %s %s\n", icon(Fail), bold.Render(o.Check.Name))
			fmt.Fprintln(w, indent(wrap.Render(o.Result.Detail), "      "))
			if o.Result.Hint != "" {
				fmt.Fprintln(w, indent(wrap.Render(dim.Render("→ "+o.Result.Hint)), "      "))
			}
		}
	}
	if len(warn) > 0 {
		fmt.Fprintln(w, "\n"+yellow.Render(bold.Render(" Worth a look")))
		for _, o := range warn {
			fmt.Fprintf(w, "  %s %s\n", icon(Warn), o.Check.Name)
			fmt.Fprintln(w, indent(wrap.Render(o.Result.Detail), "      "))
			if o.Result.Hint != "" {
				fmt.Fprintln(w, indent(wrap.Render(dim.Render("→ "+o.Result.Hint)), "      "))
			}
		}
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, Banner(v))
}

func indent(s, pad string) string {
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = pad + strings.TrimRight(lines[i], " ")
	}
	return strings.Join(lines, "\n")
}

// Banner is the big verdict.
func Banner(v Verdict) string {
	counts := fmt.Sprintf("%d passed · %d warning(s) · %d failure(s)", v.Passed, v.Warnings, v.Failures)
	if v.Skipped > 0 {
		counts += fmt.Sprintf(" · %d skipped", v.Skipped)
	}
	label, style := " GO ", lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("10"))
	verb := v.Target
	if verb == TargetAll || verb == "" {
		verb = "ship"
	}
	note := "safe to " + verb
	if !v.Go {
		label, style = " NO-GO ", lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("9"))
		note = "do not " + verb + " yet"
	}
	if v.Quick {
		note += " (quick run: the release gate needs a full one)"
	}
	return " " + style.Render(label) + "  " + bold.Render(note) + "\n " + dim.Render(counts)
}

// ---- the animated view ----

type evMsg Event
type doneMsg struct{}
type tickMsg time.Time

type model struct {
	header string
	total  int
	outs   []Outcome
	cur    *Event
	frame  int
	start  time.Time
	width  int
	height int
	events chan Event
	final  *Verdict
}

var frames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func (m model) Init() tea.Cmd { return tea.Batch(waitEvent(m.events), tick()) }

func tick() tea.Cmd {
	return tea.Tick(90*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func waitEvent(ch chan Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return doneMsg{}
		}
		return evMsg(ev)
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch t := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = t.Width, t.Height
	case tea.KeyMsg:
		if t.String() == "ctrl+c" || t.String() == "q" {
			return m, tea.Quit
		}
	case tickMsg:
		m.frame++
		return m, tick()
	case evMsg:
		ev := Event(t)
		if ev.Done {
			m.outs = append(m.outs, ev.Outcome)
			m.cur = nil
		} else {
			m.cur = &ev
		}
		return m, waitEvent(m.events)
	case doneMsg:
		v := Summarise("", "", false, time.Now(), m.outs)
		m.final = &v
		return m, tea.Quit
	}
	return m, nil
}

func (m model) View() string {
	w := m.width
	if w <= 0 {
		w = 80
	}
	h := m.height
	if h <= 0 {
		h = 24
	}
	rows := max(min(h-7, 14), 3)
	elapsed := time.Since(m.start).Round(time.Second)
	var b strings.Builder
	b.WriteString(" " + bold.Render("wms preflight") + dim.Render(" · "+m.header+" · "+elapsed.String()) + "\n")
	done := len(m.outs)
	barW := max(min(w-14, 40), 10)
	fill := 0
	if m.total > 0 {
		fill = barW * done / m.total
	}
	b.WriteString(" " + green.Render(strings.Repeat("▰", fill)) + dim.Render(strings.Repeat("▱", barW-fill)) + fmt.Sprintf("  %d/%d\n\n", done, m.total))
	shown := m.outs
	if len(shown) > rows-1 {
		shown = shown[len(shown)-(rows-1):]
	}
	for _, o := range shown {
		b.WriteString(line(o, w) + "\n")
	}
	if m.cur != nil {
		b.WriteString(fmt.Sprintf(" %s %-8s %s\n", yellow.Render(frames[m.frame%len(frames)]), m.cur.Check.Group, bold.Render(m.cur.Check.Name)))
	}
	ok, wn, fl := 0, 0, 0
	for _, o := range m.outs {
		switch o.Result.Status {
		case OK:
			ok++
		case Warn:
			wn++
		case Fail:
			fl++
		}
	}
	b.WriteString(fmt.Sprintf("\n %s %d   %s %d   %s %d   %s\n", green.Render("✓"), ok, yellow.Render("!"), wn, red.Render("✗"), fl, dim.Render("q or Ctrl-C stops")))
	return b.String()
}

// RunAnimated runs the checks under the animated view and returns their outcomes. The view is inline
// (no alternate screen), so what it printed stays in the scrollback.
func RunAnimated(ctx context.Context, e *Env, checks []Check, header string) ([]Outcome, error) {
	ch := make(chan Event)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var outs []Outcome
	go func() {
		outs = Execute(ctx, e, checks, func(ev Event) { ch <- ev })
		close(ch)
	}()
	m := model{header: header, total: len(checks), start: time.Now(), events: ch}
	final, err := tea.NewProgram(m, tea.WithContext(ctx)).Run()
	if err != nil {
		cancel()
		return nil, err
	}
	if fm, ok := final.(model); ok && fm.final == nil { // stopped early: let the goroutine finish skipping
		cancel()
		for range ch {
		}
	}
	return outs, nil
}
