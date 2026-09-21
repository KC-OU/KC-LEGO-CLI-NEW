// Package tools is the one list of things the operator does from a shell or from the TUI's Script Hub:
// check status, read logs, restart a service, back up, ship. Each entry is a command line; the launcher
// (`wms menu`) and the Script Hub show and run the same list.
package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

// Where says which front end may offer a tool.
type Where string

const (
	Shell Where = "shell" // only the local launcher (deploys and log followers must not run inside a telnet session)
	TUI   Where = "tui"
	Both  Where = "both"
)

// Tool is one entry. In Argv, "{self}" is this program, "{repo}" the source checkout, and "{0}", "{1}"
// are the answers to the Ask prompts.
type Tool struct {
	ID    string   `json:"id"`
	Group string   `json:"group"`
	Title string   `json:"title"`
	Hint  string   `json:"hint,omitempty"`
	Argv  []string `json:"argv"`
	Risky bool     `json:"risky,omitempty"` // asks yes/no first
	Admin bool     `json:"admin,omitempty"` // only administrators may run it from the TUI
	Where Where    `json:"where,omitempty"`
	Ask   []string `json:"ask,omitempty"` // prompts for {0}, {1}...
}

// In reports whether the tool may be offered in front end w.
func (t Tool) In(w Where) bool {
	where := t.Where
	if where == "" {
		where = Both
	}
	return where == Both || where == w
}

var idRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

// Validate checks one entry, so a typo in tools.json is reported instead of run.
func Validate(t Tool) error {
	switch {
	case !idRE.MatchString(t.ID):
		return fmt.Errorf("id %q: use lowercase letters, digits and dashes (up to 40)", t.ID)
	case strings.TrimSpace(t.Title) == "" || len(t.Title) > 80:
		return fmt.Errorf("%s: a title of 1-80 characters is required", t.ID)
	case len(t.Group) > 24 || len(t.Hint) > 160:
		return fmt.Errorf("%s: group or hint too long", t.ID)
	case len(t.Argv) == 0 || strings.TrimSpace(t.Argv[0]) == "" || len(t.Argv) > 32:
		return fmt.Errorf("%s: argv needs a program and up to 32 arguments", t.ID)
	case t.Where != "" && t.Where != Shell && t.Where != TUI && t.Where != Both:
		return fmt.Errorf("%s: where must be shell, tui or both", t.ID)
	case len(t.Ask) > 4:
		return fmt.Errorf("%s: at most 4 prompts", t.ID)
	}
	for _, a := range t.Argv {
		if len(a) > 512 || strings.ContainsRune(a, 0) {
			return fmt.Errorf("%s: an argument is too long or contains a NUL", t.ID)
		}
	}
	for i := range t.Ask {
		if !strings.Contains(strings.Join(t.Argv, "\x00"), fmt.Sprintf("{%d}", i)) {
			return fmt.Errorf("%s: prompt %d is never used (put {%d} in argv)", t.ID, i, i)
		}
	}
	return nil
}

// LoadFile reads extra tools from a JSON file. The file must belong to the current user or root and not
// be writable by anyone else: it names programs that will be run.
func LoadFile(path string) ([]Tool, error) {
	fi, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if fi.Mode().Perm()&0o022 != 0 {
		return nil, fmt.Errorf("%s is writable by others: chmod go-w %s", path, path)
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && int(st.Uid) != os.Geteuid() && st.Uid != 0 {
		return nil, fmt.Errorf("%s belongs to another user", path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(b)
}

// Parse decodes and validates a tools.json document. It never panics on bad input.
func Parse(b []byte) ([]Tool, error) {
	var list []Tool
	if err := json.Unmarshal(b, &list); err != nil {
		return nil, fmt.Errorf("tools file: %w", err)
	}
	if len(list) > 200 {
		return nil, fmt.Errorf("tools file: more than 200 entries")
	}
	seen := map[string]bool{}
	for i := range list {
		if err := Validate(list[i]); err != nil {
			return nil, err
		}
		if seen[list[i].ID] {
			return nil, fmt.Errorf("tools file: id %q is used twice", list[i].ID)
		}
		seen[list[i].ID] = true
		if list[i].Group == "" {
			list[i].Group = "Mine"
		}
		if list[i].Where == "" {
			list[i].Where = Shell // your own commands stay out of telnet sessions unless you say so
		}
	}
	return list, nil
}

// Merge appends user tools to the built-in ones; a user tool with a built-in id replaces it.
func Merge(builtin, user []Tool) []Tool {
	out := append([]Tool(nil), builtin...)
	idx := map[string]int{}
	for i, t := range out {
		idx[t.ID] = i
	}
	for _, t := range user {
		if i, ok := idx[t.ID]; ok {
			out[i] = t
		} else {
			idx[t.ID] = len(out)
			out = append(out, t)
		}
	}
	return out
}

// Expand fills in {self}, {repo} and the answers. A missing answer is an error.
func Expand(t Tool, self, repo string, answers []string) ([]string, error) {
	if len(answers) < len(t.Ask) {
		return nil, fmt.Errorf("%s needs %d answer(s)", t.ID, len(t.Ask))
	}
	out := make([]string, len(t.Argv))
	for i, a := range t.Argv {
		a = strings.ReplaceAll(a, "{self}", self)
		a = strings.ReplaceAll(a, "{repo}", repo)
		for j, ans := range answers {
			a = strings.ReplaceAll(a, fmt.Sprintf("{%d}", j), ans)
		}
		out[i] = a
	}
	return out, nil
}

// Answer checks a typed answer: it becomes one argument, so it may not be empty, hold control
// characters, or start with "-" (it would be read as an option).
func Answer(s string) (string, error) {
	s = strings.TrimSpace(s)
	switch {
	case s == "":
		return "", fmt.Errorf("an answer is required")
	case len(s) > 120:
		return "", fmt.Errorf("too long")
	case strings.HasPrefix(s, "-"):
		return "", fmt.Errorf("cannot start with a dash")
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("control characters are not allowed")
		}
	}
	return s, nil
}

// Groups lists the group names in first-seen order.
func Groups(list []Tool) []string {
	var out []string
	seen := map[string]bool{}
	for _, t := range list {
		if !seen[t.Group] {
			seen[t.Group] = true
			out = append(out, t.Group)
		}
	}
	return out
}

// Filter returns the tools matching every word of query (in id, group, title or hint), best first
// within the given order.
func Filter(list []Tool, query string) []Tool {
	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 {
		return list
	}
	var out []Tool
	for _, t := range list {
		hay := strings.ToLower(t.ID + " " + t.Group + " " + t.Title + " " + t.Hint)
		ok := true
		for _, w := range words {
			if !strings.Contains(hay, w) {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, t)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return titleHit(out[i], words) && !titleHit(out[j], words) })
	return out
}

func titleHit(t Tool, words []string) bool {
	title := strings.ToLower(t.Title + " " + t.ID)
	for _, w := range words {
		if !strings.Contains(title, w) {
			return false
		}
	}
	return true
}

// FilePath is where the user's own tools live: WMS_TOOLS_FILE, else tools.json next to settings.json.
func FilePath() string {
	if p := strings.TrimSpace(os.Getenv("WMS_TOOLS_FILE")); p != "" {
		return p
	}
	return filepath.Join(filepath.Dir(config.Get(config.SettingsFile)), "tools.json")
}

// StatePath is where pins and recents are kept, shared by the shell launcher and the Script Hub.
func StatePath() string {
	return filepath.Join(filepath.Dir(config.Get(config.SettingsFile)), "tools-state.json")
}

// Load is the built-in catalogue plus the user's file. A bad file is reported and ignored: the
// launcher must still open, with the built-in tools.
func Load() ([]Tool, error) {
	extra, err := LoadFile(FilePath())
	return Merge(Builtin(), extra), err
}
