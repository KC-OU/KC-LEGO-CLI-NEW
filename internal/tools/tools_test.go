package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestEveryBuiltinToolIsValidAndSafelyPlaced(t *testing.T) {
	seen := map[string]bool{}
	for _, tl := range Builtin() {
		if err := Validate(tl); err != nil {
			t.Errorf("%v", err)
		}
		if seen[tl.ID] {
			t.Errorf("duplicate id %s", tl.ID)
		}
		seen[tl.ID] = true
		args := strings.Join(tl.Argv, " ")
		switch {
		case (strings.Contains(args, " -f") || tl.ID == "deploy" || tl.ID == "publish" || tl.ID == "preflight" || strings.HasPrefix(tl.ID, "restart-")) && tl.In(TUI):
			t.Errorf("%s must stay out of telnet/web sessions", tl.ID)
		case (strings.HasPrefix(tl.ID, "restart-") || tl.ID == "deploy" || tl.ID == "publish" || tl.ID == "receive" || tl.ID == "user-reset") && !tl.Risky:
			t.Errorf("%s changes things and must ask first", tl.ID)
		case len(tl.Ask) > 0 && tl.In(TUI):
			t.Errorf("%s asks questions, which the TUI hub does not support", tl.ID)
		}
	}
	if len(seen) < 25 {
		t.Errorf("the catalogue looks too small: %d", len(seen))
	}
}

func TestParseValidatesAndDefaultsSafely(t *testing.T) {
	good := `[{"id":"hello","title":"Say hello","argv":["echo","hi"]},
	          {"id":"greet","title":"Greet","argv":["echo","{0}"],"ask":["Who"],"where":"both"}]`
	list, err := Parse([]byte(good))
	if err != nil || len(list) != 2 {
		t.Fatalf("%v %v", list, err)
	}
	if list[0].Where != Shell || list[0].Group != "Mine" {
		t.Errorf("your own commands stay out of telnet sessions by default: %+v", list[0])
	}
	for name, bad := range map[string]string{
		"not json":      `nope`,
		"bad id":        `[{"id":"Bad Id","title":"x","argv":["a"]}]`,
		"no title":      `[{"id":"a","title":"","argv":["a"]}]`,
		"no argv":       `[{"id":"a","title":"x","argv":[]}]`,
		"unused prompt": `[{"id":"a","title":"x","argv":["echo"],"ask":["Who"]}]`,
		"bad where":     `[{"id":"a","title":"x","argv":["a"],"where":"everywhere"}]`,
		"duplicate":     `[{"id":"a","title":"x","argv":["a"]},{"id":"a","title":"y","argv":["b"]}]`,
		"nul in argv":   "[{\"id\":\"a\",\"title\":\"x\",\"argv\":[\"a\\u0000b\"]}]",
		"too many asks": `[{"id":"a","title":"x","argv":["{0}{1}{2}{3}{4}"],"ask":["1","2","3","4","5"]}]`,
	} {
		if _, err := Parse([]byte(bad)); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
}

func TestToolsFileMustNotBeWritableByOthers(t *testing.T) {
	p := filepath.Join(t.TempDir(), "tools.json")
	os.WriteFile(p, []byte(`[{"id":"a","title":"x","argv":["echo"]}]`), 0o666)
	os.Chmod(p, 0o666)
	if _, err := LoadFile(p); err == nil || !strings.Contains(err.Error(), "writable by others") {
		t.Errorf("%v", err)
	}
	os.Chmod(p, 0o644)
	if l, err := LoadFile(p); err != nil || len(l) != 1 {
		t.Errorf("%v %v", l, err)
	}
	if l, err := LoadFile(filepath.Join(t.TempDir(), "absent.json")); err != nil || l != nil {
		t.Errorf("a missing file is fine: %v %v", l, err)
	}
}

func TestExpandAndAnswers(t *testing.T) {
	tl := Tool{ID: "x", Argv: []string{"{self}", "users", "reset", "{0}", "--repo={repo}"}, Ask: []string{"who"}}
	got, err := Expand(tl, "/bin/wms", "/src", []string{"bob"})
	if err != nil || strings.Join(got, " ") != "/bin/wms users reset bob --repo=/src" {
		t.Errorf("%v %v", got, err)
	}
	if _, err := Expand(tl, "/bin/wms", "/src", nil); err == nil {
		t.Error("a missing answer is an error")
	}
	for _, bad := range []string{"", "  ", "-rf", "a\nb", "a\x00b", strings.Repeat("x", 121)} {
		if _, err := Answer(bad); err == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
	if a, err := Answer("  bob smith "); err != nil || a != "bob smith" {
		t.Errorf("%q %v", a, err)
	}
	// an answer is one argument: it cannot inject another
	got, _ = Expand(Tool{Argv: []string{"echo", "{0}"}, Ask: []string{"a"}}, "", "", []string{"x; rm -rf /"})
	if len(got) != 2 {
		t.Errorf("%v", got)
	}
}

func TestFilterOrderAndPins(t *testing.T) {
	list := []Tool{
		{ID: "status", Group: "Status", Title: "System health"},
		{ID: "restart-wms", Group: "Services", Title: "Restart ModernWMS"},
		{ID: "logs-wms", Group: "Services", Title: "Logs: ModernWMS"},
		{ID: "backup", Group: "Backups", Title: "Back up ModernWMS", Hint: "sqlite"},
	}
	if got := Filter(list, "wms restart"); len(got) != 1 || got[0].ID != "restart-wms" {
		t.Errorf("all words must match: %+v", got)
	}
	if got := Filter(list, "sqlite"); len(got) != 1 || got[0].ID != "backup" {
		t.Errorf("hints are searched: %+v", got)
	}
	if got := Filter(list, "   "); len(got) != 4 {
		t.Errorf("no query lists all: %d", len(got))
	}
	var st State
	st.Record("logs-wms", time.Unix(100, 0))
	st.Record("status", time.Unix(200, 0))
	if !st.TogglePin("backup") || !st.IsPinned("backup") {
		t.Error("pin")
	}
	order := ""
	for _, tl := range st.Order(list) {
		order += tl.ID + " "
	}
	if order != "backup status logs-wms restart-wms " {
		t.Errorf("pinned, then recent (newest first), then the rest: %q", order)
	}
	if st.TogglePin("backup") || st.IsPinned("backup") {
		t.Error("unpin")
	}
	path := filepath.Join(t.TempDir(), "sub", "state.json")
	st.TogglePin("status")
	if err := st.Save(path); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Errorf("state is private: %v", fi.Mode())
	}
	if back := LoadState(path); !back.IsPinned("status") || back.Used["logs-wms"].Count != 1 {
		t.Errorf("%+v", back)
	}
	if bad := LoadState(filepath.Join(t.TempDir(), "none")); bad.Used == nil {
		t.Error("a missing file is an empty state")
	}
}

func key(p *Picker, s string) {
	switch s {
	case "enter":
		p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	case "esc":
		p.Update(tea.KeyMsg{Type: tea.KeyEsc})
	case "down":
		p.Update(tea.KeyMsg{Type: tea.KeyDown})
	case "ctrl-p":
		p.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	case "bs":
		p.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	default:
		for _, r := range s {
			p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		}
	}
}

func TestPickerRunsRiskyToolsOnlyAfterYes(t *testing.T) {
	list := []Tool{
		{ID: "status", Group: "Status", Title: "System health"},
		{ID: "restart-wms", Group: "Services", Title: "Restart ModernWMS", Risky: true},
		{ID: "greet", Group: "Mine", Title: "Greet", Argv: []string{"{0}"}, Ask: []string{"Who"}},
	}
	p := &Picker{All: list}
	key(p, "restart")
	key(p, "enter")
	if p.finished || !strings.Contains(p.View(), "Run it?") {
		t.Fatalf("a risky tool asks first:\n%s", p.View())
	}
	key(p, "n")
	if p.finished || p.mode != modeList || p.query != "restart" {
		t.Fatalf("anything but y cancels and keeps the list: %+v", p)
	}
	key(p, "enter")
	key(p, "y")
	if !p.finished || p.Chosen().Tool.ID != "restart-wms" {
		t.Fatalf("y runs it: %+v", p.Chosen())
	}

	p = &Picker{All: list}
	key(p, "greet")
	key(p, "enter")
	key(p, "enter") // empty answer
	if p.finished || p.notice == "" {
		t.Fatal("an empty answer is refused")
	}
	key(p, "bob")
	key(p, "enter")
	if ch := p.Chosen(); !p.finished || ch.Tool.ID != "greet" || len(ch.Answers) != 1 || ch.Answers[0] != "bob" {
		t.Fatalf("%+v", p.Chosen())
	}

	p = &Picker{All: list}
	key(p, "1") // the first row runs at once (status is not risky)
	if !p.finished || p.Chosen().Tool.ID != "status" {
		t.Errorf("digits run a row: %+v", p.Chosen())
	}
	p = &Picker{All: list}
	key(p, "esc")
	if !p.Chosen().Quit {
		t.Error("Esc quits")
	}
}

func TestPickerPinsAndShowsStatusAndFitsTheScreen(t *testing.T) {
	var list []Tool
	for _, tl := range Builtin() {
		if tl.In(Shell) {
			list = append(list, tl)
		}
	}
	p := &Picker{All: list, Rows: 8}
	p.Update(statusMsg("[WMS+PartDB] 7/7 up · gateway up"))
	p.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	key(p, "down")
	key(p, "ctrl-p")
	if len(p.State.Pinned) != 1 {
		t.Fatal("Ctrl-P pins the selected tool")
	}
	if first := p.visible()[0]; first.ID != p.State.Pinned[0] {
		t.Errorf("pinned tools go first: %s", first.ID)
	}
	out := p.View()
	if !strings.Contains(out, "7/7 up") || !strings.Contains(out, "★") {
		t.Errorf("status and pin marks:\n%s", out)
	}
	for _, size := range [][2]int{{80, 24}, {60, 12}, {120, 40}, {40, 10}} {
		p.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		lines := strings.Split(strings.TrimRight(p.View(), "\n"), "\n")
		if len(lines) > min(size[1]-1, 14) {
			t.Errorf("%dx%d: %d lines", size[0], size[1], len(lines))
		}
		for _, l := range lines {
			if w := lipgloss.Width(l); w > size[0] {
				t.Errorf("%dx%d: a line is %d columns", size[0], size[1], w)
			}
		}
	}
	key(p, "zzzzzz")
	if !strings.Contains(p.View(), "nothing matches") {
		t.Error("no matches is said")
	}
	key(p, "enter")
	if p.finished {
		t.Error("Enter on an empty result runs nothing")
	}
}

func FuzzParse(f *testing.F) {
	f.Add([]byte(`[{"id":"a","title":"x","argv":["echo","{0}"],"ask":["q"]}]`))
	f.Add([]byte(`[{}]`))
	f.Add([]byte(`null`))
	f.Fuzz(func(t *testing.T, b []byte) {
		list, err := Parse(b)
		if err != nil {
			return
		}
		for _, tl := range list {
			if Validate(tl) != nil {
				t.Fatalf("Parse accepted an entry Validate refuses: %+v", tl)
			}
		}
	})
}
