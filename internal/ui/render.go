package ui

import (
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Status renders a single ✓/✗ status line. Callers print it themselves.
func Status(t Theme, ok bool, message string) string {
	if ok {
		return t.Success.Render(IconOK+" "+t.label("OK")) + message
	}
	return t.Danger.Render(IconFail+" "+t.label("FAIL")) + message
}

// Warn renders a single ⚠ warning line.
func Warn(t Theme, message string) string {
	return t.Warning.Render(IconWarn+" "+t.label("WARN")) + message
}

// label is the word beside an icon in the accessible themes ("OK: "), and
// nothing in the standard ones.
func (t Theme) label(word string) string {
	if !t.Labels {
		return ""
	}
	return word + ": "
}

// Fact renders a "• Label: Value" line, value emphasized.
func Fact(t Theme, label, value string) string {
	return t.Muted.Render(IconKey+" "+label+": ") + t.Accent.Render(value)
}

func rule(width int) string {
	return strings.Repeat("-", width)
}

func RenderRule(width int) string {
	if width <= 0 {
		width = Width
	}
	return rule(width)
}

// RuleLine is a full-width divider: the Python TUI's rich Rule (─) in the
// classic layout, the 5250 dashed rule otherwise.
func RuleLine(t Theme) string {
	ch := "-"
	if t.Classic {
		ch = "─"
	}
	return t.Muted.Render(strings.Repeat(ch, t.W()))
}

// HeaderBand renders the fixed AS/400 signon-screen-style header block:
// system name (top-left) / timestamp (top-right, at the real terminal edge),
// an optional screen-id line, a centered UPPERCASE reverse-video title, a
// user/role/extra line with the badge appended, and a closing rule.
func HeaderBand(t Theme, systemName, screenID, title, user, role, when, extra, badge string) string {
	var b strings.Builder

	topLeft := t.Brand.Render(systemName)
	topRight := t.Text.Render(when)
	pad := t.W() - lipglossWidth(topLeft) - lipglossWidth(topRight)
	if pad < 1 {
		pad = 1
	}
	fmt.Fprintf(&b, "%s%s%s\n", topLeft, strings.Repeat(" ", pad), topRight)

	if screenID != "" {
		fmt.Fprintf(&b, "%s\n", t.Muted.Render(strings.ToUpper(screenID)))
	}

	upperTitle := strings.ToUpper(title)
	titleLine := centerText(upperTitle, t.W())
	fmt.Fprintf(&b, "%s\n", t.TitleReverse.Render(titleLine))

	if t.Classic {
		// wms_console.draw_5250_header: dim labels, plain values.
		info := t.Muted.Render(" User: ") + t.Text.Render(user+"   ") + t.Muted.Render("Role: ") + t.Text.Render(role+"   ")
		if extra != "" {
			info += t.Muted.Render(extra + "   ")
		}
		fmt.Fprintf(&b, "%s%s\n", info, badge)
	} else {
		info := fmt.Sprintf(" User: %s   Role: %s", user, role)
		if extra != "" {
			info += "   " + extra
		}
		info += "   "
		fmt.Fprintf(&b, "%s%s\n", t.Text.Render(info), badge)
	}

	b.WriteString(RuleLine(t))
	return b.String()
}

// MessageLine is the row-23 single-line message area: bold red on error,
// success style otherwise, cleared by the caller on next input rather than
// scrolled like the original's toast history.
func MessageLine(t Theme, message string, isError bool) string {
	if message == "" {
		return ""
	}
	if isError {
		return t.Danger.Render(message)
	}
	return t.Success.Render(message)
}

// CommandLine is the row-24 "Selection or command ===>" prompt label.
func CommandLine() string {
	return "Selection or command\n===>"
}

// FKeyRow renders the bottom function-key legend, e.g.
// "F3=Exit  F9=Undo  F6=Quick Add  F10=Lock  F12=Cancel", each Fn label in
// reverse video and its action in muted text, plus the letter-mnemonic
// fallback line for terminals that don't forward real F-key events.
func FKeyRow(t Theme, pairs [][2]string, mnemonics string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", RuleLine(t))
	parts := make([]string, 0, len(pairs))
	for _, p := range pairs {
		parts = append(parts, t.KeyLegend.Render(p[0]+"=")+t.Muted.Render(p[1]))
	}
	fmt.Fprintf(&b, "%s", strings.Join(parts, "  "))
	if mnemonics != "" {
		fmt.Fprintf(&b, "\n%s", t.Muted.Render(mnemonics))
	}
	return b.String()
}

// ClassicLegend is wms_console.render_fkey_bar's legend line (the rule above
// it is the caller's): two leading spaces, then each key as a reverse-video
// " F3" cell followed by a dim "=Exit", separated by two spaces.
func ClassicLegend(t Theme, pairs [][2]string) string {
	parts := make([]string, 0, len(pairs))
	for _, p := range pairs {
		parts = append(parts, t.KeyLegend.Render(" "+p[0])+t.Muted.Render("="+p[1]))
	}
	return "  " + strings.Join(parts, "  ")
}

// TabBar is draw_tab_bar's single row: a leading space, then "key=Label"
// items joined by three spaces, the active one reverse-video and the rest dim.
func TabBar(t Theme, tabs [][2]string, active string) string {
	build := func(sep string, label func(string) string) string {
		parts := make([]string, len(tabs))
		for i, tb := range tabs {
			text := tb[0] + "=" + label(tb[1])
			if tb[0] == active {
				parts[i] = t.TitleReverse.Render(text)
			} else {
				parts[i] = t.Muted.Render(text)
			}
		}
		return " " + strings.Join(parts, sep)
	}
	full := func(l string) string { return l }
	firstWord := func(l string) string { return strings.Fields(l + " ")[0] }
	// The original spacing is three spaces; on a narrow window (a plain 80-column telnet
	// client) squeeze the gaps, then shorten the labels, rather than let the row wrap.
	for _, try := range []struct {
		sep   string
		label func(string) string
	}{{"   ", full}, {"  ", full}, {" ", full}, {" ", firstWord}} {
		if row := build(try.sep, try.label); lipgloss.Width(row) <= t.W() {
			return row
		}
	}
	return build(" ", firstWord)
}

// cursorBlock stands in for the terminal cursor, which bubbletea hides.
func cursorBlock() string {
	return lipgloss.NewStyle().Reverse(true).Render(" ")
}

// RenderPrompt is smart_input's inline prompt: the label in bold bright
// green, a space, then what has been typed and (when active) a block cursor.
func RenderPrompt(t Theme, label, value string, active bool) string {
	out := t.Brand.Render(label) + " " + value
	if active {
		out += cursorBlock()
	}
	return out
}

// RenderPanel is wms_console.render_panel: a full-width square-corner box
// with the title spliced into the top border (┌──── Title ────┐) and one
// column of padding inside. border picks the color (t.Brand for the login
// card, t.Warning for the forced-password-change card).
func RenderPanel(t Theme, border lipgloss.Style, title, body string) string {
	return renderPanel(t, border, title, body, "┌", "┐", "└", "┘")
}

// RenderPanelRounded is RenderPanel with soft corners instead of square ones — used
// for hub/menu screens and the alert popup, so the rest of the app reads closer to
// the sign-on card's look rather than inventing a second panel style.
func RenderPanelRounded(t Theme, border lipgloss.Style, title, body string) string {
	return renderPanel(t, border, title, body, "╭", "╮", "╰", "╯")
}

func renderPanel(t Theme, border lipgloss.Style, title, body, topLeft, topRight, bottomLeft, bottomRight string) string {
	w := t.W()
	inner := w - 4
	if inner < 1 {
		inner = 1
	}
	lines := strings.Split(lipgloss.NewStyle().Width(inner).Render(body), "\n")

	seg := ""
	if title != "" {
		seg = " " + title + " "
	}
	fill := w - 2 - lipgloss.Width(seg)
	if fill < 0 {
		seg, fill = "", w-2
	}
	if fill < 0 {
		fill = 0
	}
	left := fill / 2
	top := topLeft + strings.Repeat("─", left) + seg + strings.Repeat("─", fill-left) + topRight
	bottom := bottomLeft + strings.Repeat("─", w-2) + bottomRight

	var b strings.Builder
	b.WriteString(border.Render(top))
	for _, l := range lines {
		b.WriteString("\n" + border.Render("│") + " " + l + " " + border.Render("│"))
	}
	b.WriteString("\n" + border.Render(bottom))
	return b.String()
}

// RenderGrid is wms_console.render_table: a SQUARE-box grid (top border,
// reverse-video header row, ├─┼┤ separator, data rows, bottom border) in the
// muted color, sized to its content rather than the terminal (rich's
// expand=False), with the title as a reverse-video bar centered above it.
func RenderGrid(t Theme, columns []string, rows [][]string, title string) string {
	n := len(columns)
	widths := make([]int, n)
	for i, c := range columns {
		widths[i] = lipgloss.Width(c)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i < n && lipgloss.Width(cell) > widths[i] {
				widths[i] = lipgloss.Width(cell)
			}
		}
	}

	if t.Width > 0 { // a known terminal width; command-line output is never cut
		fitWidths(widths, t.Width-1-3*n)
	}
	total := 1
	for _, w := range widths {
		total += w + 3
	}
	hline := func(l, m, r string) string {
		parts := make([]string, n)
		for i, w := range widths {
			parts[i] = strings.Repeat("─", w+2)
		}
		return t.Muted.Render(l + strings.Join(parts, m) + r)
	}
	vbar := t.Muted.Render("│")
	line := func(cells []string, style lipgloss.Style) string {
		parts := make([]string, n)
		for i, w := range widths {
			v := ""
			if i < len(cells) {
				v = ansi.Truncate(cells[i], w, "…")
			}
			parts[i] = style.Render(" " + v + strings.Repeat(" ", w-lipgloss.Width(v)) + " ")
		}
		return vbar + strings.Join(parts, vbar) + vbar
	}

	var out []string
	if title != "" {
		out = append(out, t.TitleReverse.Render(centerText(title, total)))
	}
	out = append(out, hline("┌", "┬", "┐"))
	if strings.Join(columns, "") != "" { // a two-column record view has no heading row
		out = append(out, line(columns, t.HeaderReverse), hline("├", "┼", "┤"))
	}
	for _, row := range rows {
		out = append(out, line(row, t.Text))
	}
	out = append(out, hline("└", "┴", "┘"))
	return strings.Join(out, "\n")
}

// RenderColumns renders plain fixed-width columnar data — no box-drawing
// borders, since real 5250 subfiles didn't have them — with a reverse-video
// column-heading row, the one place reverse video is used for tabular data.
func RenderColumns(t Theme, columns []string, rows [][]string, title string) string {
	widths := make([]int, len(columns))
	for i, c := range columns {
		widths[i] = lipgloss.Width(c)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) && lipgloss.Width(cell) > widths[i] {
				widths[i] = lipgloss.Width(cell)
			}
		}
	}
	if t.Width > 0 {
		fitWidths(widths, t.Width-2*(len(columns)-1))
	}

	var b strings.Builder
	if title != "" {
		for _, l := range strings.Split(title, "\n") {
			if t.Width > 0 {
				l = ansi.Truncate(l, t.Width, "…")
			}
			fmt.Fprintf(&b, "%s\n", t.Brand.Render(l))
		}
		b.WriteString("\n")
	}

	header := make([]string, len(columns))
	for i, c := range columns {
		header[i] = padRight(c, widths[i])
	}
	fmt.Fprintf(&b, "%s\n", t.HeaderReverse.Render(strings.Join(header, "  ")))

	for _, row := range rows {
		fmt.Fprintf(&b, "%s\n", t.Text.Render(renderDataRow(row, widths)))
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderDataRow(row []string, widths []int) string {
	cells := make([]string, len(widths))
	for i := range widths {
		val := ""
		if i < len(row) {
			val = row[i]
		}
		cells[i] = padRight(ansi.Truncate(val, widths[i], "…"), widths[i])
	}
	return strings.Join(cells, "  ")
}

// FrameBodyRow is the 0-indexed terminal row the legacy (5250) Frame's body
// starts on. Every screen sets a non-empty screenID (its panelID), so
// HeaderBand always renders exactly 5 lines (system/time, screen ID, title,
// user/role, rule) and Frame adds one blank line before the body. The
// classic layout computes its own body row per render (see ClassicFrame).
const FrameBodyRow = 6

// MenuOptionBodyRow returns the 0-indexed row, within a menu screen's body,
// that option i's line is drawn on: RenderClassicMenu puts the caption on row
// 0 and the options on the following rows with no blank lines between them;
// the legacy RenderMenu draws "Select..." + a blank line, then one line + one
// blank line per option. Kept next to both renderers so a layout change to
// one can't silently desync from touch-mode click handling
// (internal/uiapp menuScreen.HandleClick).
func MenuOptionBodyRow(t Theme, i int) int {
	if t.Classic {
		return 1 + i
	}
	return 2 + 2*i
}

// RenderMenu renders an AS/400-style option menu: "Select one of the
// following:" then blank-line-separated numbered options, not a bordered
// list widget.
func RenderMenu(t Theme, options [][2]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", t.Text.Render("Select one of the following:"))
	for _, opt := range options {
		fmt.Fprintf(&b, "     %s. %s\n\n", t.Accent.Render(opt[0]), t.Text.Render(opt[1]))
	}
	return strings.TrimRight(b.String(), "\n")
}

// RenderClassicMenu is render_selectable_list under a muted caption line:
// tight one-line-per-option rows ` {marker} {key:>3}. {label}`, the key in
// bold white and a ">" marker on the active row.
func RenderClassicMenu(t Theme, caption string, options [][2]string, active string) string {
	var b strings.Builder
	b.WriteString(t.Muted.Render(caption))
	for _, o := range options {
		marker := " "
		if o[0] == active {
			marker = t.Strong.Render(">")
		}
		fmt.Fprintf(&b, "\n %s %s %s", marker, t.Strong.Render(fmt.Sprintf("%3s.", o[0])), t.Text.Render(o[1]))
	}
	return b.String()
}

// PrintHelp renders a --help screen shared by every subcommand.
func PrintHelp(t Theme, command string, usageLines []string, title string) string {
	var b strings.Builder
	if title == "" {
		title = command
	}
	fmt.Fprintf(&b, "%s\n%s\n\n", t.TitleReverse.Render(centerText(strings.ToUpper(title), Width)), t.Muted.Render(rule(Width)))
	for _, line := range usageLines {
		fmt.Fprintf(&b, "%s\n", t.Text.Render(line))
	}
	return strings.TrimRight(b.String(), "\n")
}

func padRight(s string, width int) string {
	if w := lipgloss.Width(s); w < width {
		return s + strings.Repeat(" ", width-w)
	}
	return s
}

func centerText(s string, width int) string {
	if len(s) >= width {
		return s
	}
	total := width - len(s)
	left := total / 2
	right := total - left
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", right)
}

// lipglossWidth strips ANSI styling to measure visible width; rendered
// strings from this package's own styles are plain ASCII/box-safe text, so a
// straightforward rune count after stripping SGR codes is sufficient here.
func lipglossWidth(s string) int {
	inEscape := false
	n := 0
	for _, r := range s {
		if r == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if r == 'm' {
				inEscape = false
			}
			continue
		}
		n++
	}
	return n
}

// Swatch is a two-cell block filled with the given hex colour (six digits, no
// '#'), for showing a LEGO colour next to its name. lipgloss reduces it to the
// nearest colour the terminal supports; an unusable value is plain spaces.
func Swatch(rgb string) string {
	if len(rgb) != 6 {
		return "  "
	}
	for _, r := range rgb {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return "  "
		}
	}
	return lipgloss.NewStyle().Background(lipgloss.Color("#" + rgb)).Render("  ")
}

// fitWidths narrows the widest columns until they add up to at most avail, so a table never runs
// past the edge of the terminal (the cells are cut with an ellipsis). The widest columns give up
// width first and evenly, so a long name and a long theme share the loss instead of one being
// reduced to nothing. A column is never narrowed below 8, which lets a very narrow window overflow
// rather than show nothing.
func fitWidths(widths []int, avail int) {
	const floor = 8
	for {
		sum, m := 0, 0
		for _, w := range widths {
			sum += w
			m = max(m, w)
		}
		over := sum - avail
		if over <= 0 || m <= floor {
			return
		}
		var widest []int
		next := floor // the next level down: the second-widest column, or the floor
		for i, w := range widths {
			switch {
			case w == m:
				widest = append(widest, i)
			case w > next:
				next = w
			}
		}
		cut := min(m-next, (over+len(widest)-1)/len(widest))
		for _, i := range widest {
			widths[i] -= cut
		}
	}
}

// AchievementLine is one milestone: a filled or hollow star, the name, a progress
// bar and the count. The star is backed by the word DONE where the theme spells
// meaning out (Labels), so it never rests on a glyph or colour alone.
func AchievementLine(t Theme, name, desc string, have, goal int) string {
	const barW = 12
	done := have >= goal
	filled := barW
	if !done && goal > 0 {
		filled = min(barW, have*barW/goal)
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barW-filled)
	star, style := "☆", t.Muted
	if done {
		star, style = "★", t.Success
	}
	label := ""
	if done && t.Labels {
		label = " DONE"
	}
	return fmt.Sprintf(" %s %s %s %s %s", style.Render(star), t.Strong.Render(fmt.Sprintf("%-16s", name)), style.Render(bar),
		t.Text.Render(fmt.Sprintf("%d/%d%s", have, goal, label)), t.Muted.Render(desc))
}
