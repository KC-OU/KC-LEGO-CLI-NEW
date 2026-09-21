package ui

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestRoleBadge(t *testing.T) {
	th := greenTheme()
	cases := []struct {
		role     string
		canWrite bool
		want     string
	}{
		{"Admin", true, "ADMIN"},
		{"admin", true, "ADMIN"},
		{"Picker", true, "OPERATOR"},
		{"Picker", false, "VIEW-ONLY"},
		{"ViewOnly", true, "VIEW-ONLY"},
		{"Read Only", true, "VIEW-ONLY"},
	}
	for _, c := range cases {
		got := RoleBadge(th, c.role, c.canWrite)
		if !strings.Contains(got, c.want) {
			t.Errorf("RoleBadge(%q, %v) = %q, want containing %q", c.role, c.canWrite, got, c.want)
		}
	}
}

func TestHeaderBand(t *testing.T) {
	th := greenTheme()
	out := HeaderBand(th, "KCPARTS", "USRMGT", "user security maintenance", "admin", "Admin", "2026-09-14 12:00", "Source: WMS", RoleBadge(th, "Admin", true))
	for _, want := range []string{"KCPARTS", "USRMGT", "USER SECURITY MAINTENANCE", "User: admin", "Role: Admin", "ADMIN"} {
		if !strings.Contains(out, want) {
			t.Errorf("HeaderBand output missing %q\n---\n%s", want, out)
		}
	}
}

func TestRenderColumns(t *testing.T) {
	out := RenderColumns(greenTheme(), []string{"ID", "Name"}, [][]string{{"1", "Widget"}, {"2", "Gadget"}}, "Parts")
	for _, want := range []string{"Parts", "ID", "Name", "1", "Widget", "2", "Gadget"} {
		if !strings.Contains(out, want) {
			t.Errorf("RenderColumns missing %q\n---\n%s", want, out)
		}
	}
}

func TestRenderPanel(t *testing.T) {
	th := greenTheme()
	th.Width = 60
	out := RenderPanel(th, th.Brand, "Secure Login", "Login using ModernWMS or PartDB account credentials.")
	lines := strings.Split(out, "\n")
	if len(lines) != 3 {
		t.Fatalf("expected top border, one body line, bottom border; got %d lines:\n%s", len(lines), out)
	}
	for i, l := range lines {
		if w := lipgloss.Width(l); w != 60 {
			t.Errorf("line %d is %d columns wide, want the full 60:\n%s", i, w, out)
		}
	}
	if !strings.Contains(lines[0], "┌") || !strings.Contains(lines[0], " Secure Login ") || !strings.Contains(lines[0], "┐") {
		t.Errorf("title must sit inside the top border line, got %q", lines[0])
	}
	if !strings.Contains(lines[1], "Login using ModernWMS or PartDB account credentials.") || !strings.Contains(lines[2], "└") {
		t.Errorf("unexpected body/bottom lines:\n%s", out)
	}
}

func TestRenderGrid(t *testing.T) {
	out := RenderGrid(greenTheme(), []string{"Field", "Value"}, [][]string{{"ID", "7"}, {"Name", "Widget"}}, "Part Detail")
	lines := strings.Split(out, "\n")
	// title bar, top, header, separator, 2 rows, bottom
	if len(lines) != 7 {
		t.Fatalf("expected 7 lines, got %d:\n%s", len(lines), out)
	}
	for i, glyph := range map[int]string{1: "┌", 3: "├", 6: "└"} {
		if !strings.Contains(lines[i], glyph) {
			t.Errorf("line %d missing %q:\n%s", i, glyph, out)
		}
	}
	w := lipgloss.Width(lines[1])
	for i := 1; i < len(lines); i++ {
		if lipgloss.Width(lines[i]) != w {
			t.Errorf("line %d is %d wide, table is %d wide:\n%s", i, lipgloss.Width(lines[i]), w, out)
		}
	}
}

func TestTabBarMarksActive(t *testing.T) {
	th := greenTheme()
	out := TabBar(th, [][2]string{{"1", "Overview"}, {"2", "PartDB Hub"}}, "2")
	if !strings.HasPrefix(out, " ") || !strings.Contains(out, "1=Overview") || !strings.Contains(out, "2=PartDB Hub") {
		t.Fatalf("unexpected tab bar %q", out)
	}
}

func TestThemeWidthFallback(t *testing.T) {
	th := greenTheme()
	if th.W() != Width {
		t.Fatalf("no reported size should mean %d columns, got %d", Width, th.W())
	}
	th.Width = 132
	if got := lipgloss.Width(RuleLine(th)); got != 132 {
		t.Fatalf("RuleLine should span the reported width, got %d", got)
	}
}

func TestClassicMenuRowsMatchTouchMath(t *testing.T) {
	th := greenTheme()
	th.Classic = true
	menu := RenderClassicMenu(th, "Main Navigation Hub:", [][2]string{{"1", "Overview"}, {"2", "PartDB Hub"}, {"e", "LEGO Collection"}}, "2")
	lines := strings.Split(menu, "\n")
	for i, want := range []string{"Overview", "PartDB Hub", "LEGO Collection"} {
		if row := MenuOptionBodyRow(th, i); !strings.Contains(lines[row], want) {
			t.Errorf("option %d should be on body row %d, but that row is %q", i, row, lines[row])
		}
	}
}

func TestRenderMenu(t *testing.T) {
	out := RenderMenu(greenTheme(), [][2]string{{"1", "Overview"}, {"2", "PartDB Hub"}})
	for _, want := range []string{"Select one of the following:", "1", "Overview", "2", "PartDB Hub"} {
		if !strings.Contains(out, want) {
			t.Errorf("RenderMenu missing %q\n---\n%s", want, out)
		}
	}
}

func withColor(t *testing.T) {
	t.Helper()
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })
}

func TestStatusLabelsAppearOnlyInTheAccessibleThemes(t *testing.T) {
	if got := plainText(Status(greenTheme(), true, "saved")); got != "✓ saved" {
		t.Errorf("the default theme keeps the terse form, got %q", got)
	}
	for _, name := range []string{"high-contrast", "colorblind"} {
		th := ByName(name)
		if got := plainText(Status(th, true, "saved")); got != "✓ OK: saved" {
			t.Errorf("%s: %q", name, got)
		}
		if got := plainText(Status(th, false, "nope")); got != "✗ FAIL: nope" {
			t.Errorf("%s: %q", name, got)
		}
		if got := plainText(Warn(th, "careful")); got != "⚠ WARN: careful" {
			t.Errorf("%s: %q", name, got)
		}
	}
}

var ansiSGR = regexp.MustCompile(`\x1b\[([0-9;]*)m`)

func plainText(s string) string { return ansiSGR.ReplaceAllString(s, "") }

func TestNoColorWinsOverAnyThemeAndEmitsNoColourCodes(t *testing.T) {
	withColor(t)
	t.Setenv("MODERNWMS_TUI_THEME", "amber")
	t.Setenv("NO_COLOR", "1")
	th := New()
	if th.Name != "mono" || !th.Mono || !th.Labels {
		t.Fatalf("theme = %+v", th)
	}
	for _, out := range []string{Status(th, false, "x"), Warn(th, "x"), th.Brand.Render("x"), th.TitleReverse.Render("x"), RoleBadge(th, "Admin", true)} {
		for _, m := range ansiSGR.FindAllStringSubmatch(out, -1) {
			for _, code := range strings.Split(m[1], ";") {
				if n, _ := strconv.Atoi(code); (n >= 30 && n <= 37) || (n >= 40 && n <= 47) || (n >= 90 && n <= 107) || n == 38 || n == 48 {
					t.Errorf("mono output contains colour code %q: %q", m[0], out)
				}
			}
		}
	}
}

func TestThemeSelectionByName(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	for name, want := range map[string]string{"": "green", "nonsense": "green", "AMBER": "amber", "high-contrast": "high-contrast", "colorblind": "colorblind"} {
		t.Setenv("MODERNWMS_TUI_THEME", name)
		if got := New().Name; got != want {
			t.Errorf("theme %q -> %q, want %q", name, got, want)
		}
	}
	for _, n := range Themes {
		if ByName(n).Name != n {
			t.Errorf("Themes lists %q but ByName does not know it", n)
		}
	}
}

// Red and green must not be the only difference between OK and FAIL for the
// colour-blind theme, and high contrast must not dim anything.
func TestAccessibleThemesAvoidRedGreenAndDimming(t *testing.T) {
	withColor(t)
	cb := ByName("colorblind")
	ok, fail := cb.Success.Render("x"), cb.Danger.Render("x")
	if ok == fail {
		t.Error("OK and FAIL must render differently")
	}
	for _, out := range []string{ok, fail} {
		if strings.Contains(out, "38;5;2m") || strings.Contains(out, "38;5;1m") || strings.Contains(out, "[31m") || strings.Contains(out, "[32m") {
			t.Errorf("colour-blind theme uses plain red/green: %q", out)
		}
	}
	hc := ByName("high-contrast")
	for name, st := range map[string]lipgloss.Style{"Muted": hc.Muted, "Protected": hc.Protected} {
		if st.GetFaint() {
			t.Errorf("high-contrast %s is dimmed", name)
		}
	}
}

func TestTabBarNeverWrapsOnANarrowWindow(t *testing.T) {
	tabs := [][2]string{{"1", "Overview"}, {"2", "PartDB Hub"}, {"3", "Operations"}, {"4", "Script Hub"}, {"e", "LEGO Collection"}, {"9", "Admin"}}
	for _, w := range []int{80, 100, 120} {
		th := greenTheme()
		th.Width = w
		row := plainText(TabBar(th, tabs, "1"))
		if lipgloss.Width(row) > w {
			t.Errorf("width %d: the tab bar is %d columns: %q", w, lipgloss.Width(row), row)
		}
		if !strings.Contains(row, "1=Overview") || !strings.Contains(row, "9=") {
			t.Errorf("width %d: every tab stays reachable: %q", w, row)
		}
	}
	th := greenTheme()
	th.Width = 120
	if row := plainText(TabBar(th, tabs, "1")); !strings.Contains(row, "2=PartDB Hub   3=Operations") {
		t.Errorf("a wide window keeps the original spacing: %q", row)
	}
}

func TestANSIToHTML(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain", "plain"},
		{"a<b>&\"c\"", "a&lt;b&gt;&amp;&#34;c&#34;"},
		{"\x1b[31mred\x1b[0m done", `<span style="color:#cd0000">red</span> done`},
		{"\x1b[1;32mbold green\x1b[m", `<span style="color:#00cd00;font-weight:bold">bold green</span>`},
		{"\x1b[38;2;255;128;0;48;2;0;0;255m▀\x1b[0m", `<span style="color:#ff8000;background:#0000ff">▀</span>`},
		{"\x1b[38;5;208mo\x1b[0m", `<span style="color:#ff8700">o</span>`},
		{"\x1b[38;5;244mg\x1b[0m", `<span style="color:#808080">g</span>`},
		{"\x1b[7mrev\x1b[0m", `<span style="color:#0c0c0c;background:#cccccc">rev</span>`},
		{"\x1b[2J\x1b[Hgone\x1b[?25l", "gone"},
		{"\x1b[4mu\x1b[24mn", `<span style="text-decoration:underline">u</span>n`},
		{"\x1b[91mbright\x1b[39m plain", `<span style="color:#ff0000">bright</span> plain`},
		{"x\x1b[999", "x"},
	}
	for _, c := range cases {
		if got := ANSIToHTML(c.in); got != c.want {
			t.Errorf("ANSIToHTML(%q)\n got %s\nwant %s", c.in, got, c.want)
		}
	}
	// hostile input can never inject markup or a raw escape
	evil := "\x1b[31m<script>alert(1)</script>\x1b[0m\x1b]0;title\x07"
	out := ANSIToHTML(evil)
	if strings.Contains(out, "<script>") || strings.Contains(out, "\x1b") {
		t.Errorf("unsafe output: %q", out)
	}
}

func FuzzANSIToHTML(f *testing.F) {
	f.Add("\x1b[38;2;1;2;3mx\x1b[0m")
	f.Add("\x1b[999;999;999;999;999m")
	f.Fuzz(func(t *testing.T, s string) {
		out := ANSIToHTML(s)
		if strings.Contains(out, "<script") || strings.Contains(out, "\x1b") {
			t.Fatalf("unsafe output %q for %q", out, s)
		}
	})
}

func TestTypingOverASuggestedValueReplacesItButBackspaceEditsIt(t *testing.T) {
	typeAll := func(f *FieldList, s string) {
		for _, r := range s {
			f.Type(r)
		}
	}
	f := NewFieldList(Field{Label: "Qty", Value: "1", Fresh: true}, Field{Label: "Name", Value: "Brick"})
	typeAll(f, "12")
	if f.Value(0) != "12" {
		t.Errorf("typing over a suggested 1 must give 12, got %q", f.Value(0))
	}
	f = NewFieldList(Field{Label: "Qty", Value: "12", Fresh: true})
	f.Backspace()
	typeAll(f, "5")
	if f.Value(0) != "15" {
		t.Errorf("Backspace switches to editing: got %q, want 15", f.Value(0))
	}
	f = NewFieldList(Field{Label: "Name", Value: "Brick"})
	typeAll(f, "s")
	if f.Value(0) != "Bricks" {
		t.Errorf("an ordinary prefilled field is still appended to: %q", f.Value(0))
	}
}

func TestFitWidthsSharesTheLossBetweenTheWidestColumns(t *testing.T) {
	w := []int{15, 60, 40, 4}
	fitWidths(w, 70)
	sum := 0
	for _, x := range w {
		sum += x
	}
	if sum > 70 || w[1] < 20 || w[2] < 20 || w[0] != 15 || w[3] != 4 {
		t.Errorf("the two long columns share the cut, the short ones are untouched: %v (sum %d)", w, sum)
	}
	n := []int{10, 12}
	fitWidths(n, 5)
	if n[0] != 8 || n[1] != 8 { // both come down evenly, but never below 8
		t.Errorf("floor: %v", n)
	}
	small := []int{4, 30}
	fitWidths(small, 20)
	if small[0] != 4 || small[1] != 16 {
		t.Errorf("a column already under the floor is left alone: %v", small)
	}
}

func TestCommandLineTablesAreNeverCutButTheScreenOnesFit(t *testing.T) {
	rows := [][]string{{"75192-1", strings.Repeat("N", 70), strings.Repeat("T", 50)}}
	cli := New() // width unknown: piped output
	if out := RenderColumns(cli, []string{"Set", "Name", "Theme"}, rows, ""); strings.Contains(out, "…") {
		t.Errorf("command-line output must keep every character:\n%s", out)
	}
	scr := RenderColumns(New().WithWidth(80), []string{"Set", "Name", "Theme"}, rows, "")
	for _, l := range strings.Split(scr, "\n") {
		if w := lipgloss.Width(l); w > 80 {
			t.Errorf("a screen row must fit 80 columns, got %d", w)
		}
	}
}
