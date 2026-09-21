// Package plugin runs your own programs from wms, git-style: `wms foo` runs
// `wms-foo` from a plugin folder, and enabled plugins can be told about events
// (a part was added, stock is low, a price dropped) with a JSON message on stdin.
//
// Plugins run with the gateway's power, so the model is strict:
//   - programs are found ONLY in the plugin folder, never on $PATH, and the folder and
//     every program must not be writable by anyone but their owner;
//   - a plugin does nothing until you enable it, which pins the SHA-256 of the file:
//     if the file changes, it refuses to run until you enable it again;
//   - it runs with a scrubbed environment (no keys, tokens or settings), a time limit
//     and, when wms itself runs as root, as an unprivileged user (nobody by default);
//   - every run is reported to the audit log.
package plugin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

// Events a plugin can subscribe to.
var Events = []string{"part_added", "part_updated", "low_stock", "price_drop", "backup_done", "catalog_refreshed"}

const (
	registryFile = "plugins.json"
	prefix       = "wms-"
	HookTimeout  = 10 * time.Second
	maxOutput    = 64 << 10
)

var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

// Entry is what is remembered about an enabled (or once enabled) plugin.
type Entry struct {
	SHA256    string    `json:"sha256"`
	Enabled   bool      `json:"enabled"`
	Hooks     []string  `json:"hooks,omitempty"`
	User      string    `json:"user,omitempty"` // "" = the configured default (nobody); "root" only if you chose --as-root
	EnabledAt time.Time `json:"enabled_at"`
}

type registry struct {
	Plugins map[string]Entry `json:"plugins"`
}

// Manager works on one plugin folder.
type Manager struct {
	Dir         string
	DefaultUser string // who plugins run as when wms runs as root ("nobody")
	Audit       func(action, status, details string)
	timeout     time.Duration // 0 = HookTimeout (tests shorten it)
	// Env is the base environment passed to plugins (already scrubbed); WMS_PLUGIN_* are added.
	Env []string
}

// New returns a Manager for dir. Audit may be nil.
func New(dir, defaultUser string) *Manager {
	if defaultUser == "" {
		defaultUser = "nobody"
	}
	return &Manager{Dir: dir, DefaultUser: defaultUser, Env: []string{"PATH=/usr/local/bin:/usr/bin:/bin", "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "HOME=/nonexistent"}}
}

// ValidName reports whether name is a legal plugin name (no path characters, ever).
func ValidName(name string) bool { return nameRE.MatchString(name) }

func (m *Manager) audit(action, status, details string) {
	if m.Audit != nil {
		m.Audit(action, status, details)
	}
}

func selfUID() int { return os.Geteuid() }

// checkOwned verifies a file or directory is owned by root or by the user running
// wms, and is not writable by group or others.
func checkOwned(path string, fi os.FileInfo) error {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("cannot read the owner of %s", path)
	}
	if int(st.Uid) != 0 && int(st.Uid) != selfUID() {
		return fmt.Errorf("%s is owned by user %d, not root or you", path, st.Uid)
	}
	if fi.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("%s is writable by other users (mode %04o): fix with chmod go-w", path, fi.Mode().Perm())
	}
	return nil
}

// CheckDir verifies the plugin folder is safe to run programs from.
func (m *Manager) CheckDir() error {
	fi, err := os.Lstat(m.Dir)
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
		return fmt.Errorf("%s must be a real directory, not a link", m.Dir)
	}
	return checkOwned(m.Dir, fi)
}

func (m *Manager) path(name string) (string, error) {
	if !ValidName(name) {
		return "", fmt.Errorf("%q is not a valid plugin name (lower-case letters, digits, - and _)", name)
	}
	return filepath.Join(m.Dir, prefix+name), nil
}

// checkProgram verifies one plugin file: a regular file (not a link), owned safely,
// not writable by others, and executable.
func (m *Manager) checkProgram(name string) (string, os.FileInfo, error) {
	if err := m.CheckDir(); err != nil {
		return "", nil, err
	}
	p, err := m.path(name)
	if err != nil {
		return "", nil, err
	}
	fi, err := os.Lstat(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil, fmt.Errorf("no plugin %q in %s", name, m.Dir)
		}
		return "", nil, err
	}
	if fi.Mode()&os.ModeSymlink != 0 || !fi.Mode().IsRegular() {
		return "", nil, fmt.Errorf("%s must be a regular file, not a link", p)
	}
	if err := checkOwned(p, fi); err != nil {
		return "", nil, err
	}
	if fi.Mode().Perm()&0o100 == 0 {
		return "", nil, fmt.Errorf("%s is not executable (chmod 755 it)", p)
	}
	return p, fi, nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (m *Manager) load() (*registry, error) {
	reg := &registry{Plugins: map[string]Entry{}}
	b, err := os.ReadFile(filepath.Join(m.Dir, registryFile))
	if errors.Is(err, os.ErrNotExist) {
		return reg, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, reg); err != nil {
		return nil, fmt.Errorf("%s is damaged: %w", registryFile, err)
	}
	if reg.Plugins == nil {
		reg.Plugins = map[string]Entry{}
	}
	return reg, nil
}

func (m *Manager) save(reg *registry) error {
	b, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(m.Dir, ".plugins-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(m.Dir, registryFile))
}

// Info describes a plugin file and its registration.
type Info struct {
	Name     string
	SHA256   string // of the file now
	Entry    Entry
	Known    bool   // it has been enabled at some point
	Problem  string // why it cannot run (empty = fine)
	Modified bool   // enabled, but the file has changed since
}

// List shows every wms-* file in the folder with its state.
func (m *Manager) List() ([]Info, error) {
	if err := m.CheckDir(); err != nil {
		return nil, err
	}
	reg, err := m.load()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(m.Dir)
	if err != nil {
		return nil, err
	}
	var out []Info
	seen := map[string]bool{}
	for _, e := range entries {
		n := e.Name()
		if !strings.HasPrefix(n, prefix) || !ValidName(strings.TrimPrefix(n, prefix)) {
			continue
		}
		name := strings.TrimPrefix(n, prefix)
		seen[name] = true
		info := Info{Name: name}
		ent, known := reg.Plugins[name]
		info.Entry, info.Known = ent, known
		if p, _, err := m.checkProgram(name); err != nil {
			info.Problem = err.Error()
		} else if sum, err := fileSHA256(p); err != nil {
			info.Problem = err.Error()
		} else {
			info.SHA256 = sum
			info.Modified = known && ent.Enabled && ent.SHA256 != sum
		}
		out = append(out, info)
	}
	for name, ent := range reg.Plugins { // enabled once, file now gone
		if !seen[name] {
			out = append(out, Info{Name: name, Entry: ent, Known: true, Problem: "the file is missing"})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Inspect returns the plugin's current SHA-256, for showing before it is enabled.
func (m *Manager) Inspect(name string) (sum string, err error) {
	p, _, err := m.checkProgram(name)
	if err != nil {
		return "", err
	}
	return fileSHA256(p)
}

// Enable pins the plugin's current contents and turns it on. hooks are the events
// it should be told about; asUser "" means the default unprivileged user.
func (m *Manager) Enable(name string, hooks []string, asUser string) (Entry, error) {
	sum, err := m.Inspect(name)
	if err != nil {
		return Entry{}, err
	}
	for _, h := range hooks {
		if !containsStr(Events, h) {
			return Entry{}, fmt.Errorf("unknown event %q (events: %s)", h, strings.Join(Events, ", "))
		}
	}
	if asUser != "" {
		if _, err := user.Lookup(asUser); err != nil {
			return Entry{}, fmt.Errorf("no user %q on this machine", asUser)
		}
	}
	reg, err := m.load()
	if err != nil {
		return Entry{}, err
	}
	ent := Entry{SHA256: sum, Enabled: true, Hooks: hooks, User: asUser, EnabledAt: time.Now().UTC()}
	reg.Plugins[name] = ent
	if err := m.save(reg); err != nil {
		return Entry{}, err
	}
	m.audit("PLUGIN_ENABLED", "SUCCESS", fmt.Sprintf("%s sha256=%s hooks=%s user=%s", name, sum[:12], strings.Join(hooks, ","), orDefault(asUser, m.DefaultUser)))
	return ent, nil
}

// Disable turns a plugin off (its registration is kept, marked disabled).
func (m *Manager) Disable(name string) error {
	if !ValidName(name) {
		return fmt.Errorf("%q is not a valid plugin name", name)
	}
	reg, err := m.load()
	if err != nil {
		return err
	}
	ent, ok := reg.Plugins[name]
	if !ok {
		return fmt.Errorf("plugin %q was never enabled", name)
	}
	ent.Enabled = false
	reg.Plugins[name] = ent
	if err := m.save(reg); err != nil {
		return err
	}
	m.audit("PLUGIN_DISABLED", "SUCCESS", name)
	return nil
}

func containsStr(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// verified returns the program path if the plugin is enabled and unchanged.
func (m *Manager) verified(name string) (string, Entry, error) {
	p, _, err := m.checkProgram(name)
	if err != nil {
		return "", Entry{}, err
	}
	reg, err := m.load()
	if err != nil {
		return "", Entry{}, err
	}
	ent, ok := reg.Plugins[name]
	if !ok || !ent.Enabled {
		return "", Entry{}, fmt.Errorf("plugin %q is not enabled (review it, then: wms plugin enable %s)", name, name)
	}
	sum, err := fileSHA256(p)
	if err != nil {
		return "", Entry{}, err
	}
	if sum != ent.SHA256 {
		m.audit("PLUGIN_REFUSED", "DENIED", fmt.Sprintf("%s changed since it was enabled", name))
		return "", Entry{}, fmt.Errorf("plugin %q has changed since you enabled it (was %s, now %s): review it and enable it again", name, ent.SHA256[:12], sum[:12])
	}
	return p, ent, nil
}

// dropTo returns who the plugin runs as, and the credential to switch to it. We can only
// drop privileges when we are root; otherwise the plugin runs as us.
func (m *Manager) dropTo(ent Entry) (*syscall.Credential, string, error) {
	who := orDefault(ent.User, m.DefaultUser)
	if selfUID() != 0 || who == "root" {
		return nil, who, nil
	}
	u, err := user.Lookup(who)
	if err != nil {
		return nil, who, fmt.Errorf("plugins run as %q, but there is no such user: create it or set WMS_PLUGIN_USER", who)
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	return &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}, who, nil
}

// limited caps how much output is kept.
type limited struct {
	buf bytes.Buffer
	n   int
}

func (l *limited) Write(p []byte) (int, error) {
	if room := maxOutput - l.n; room > 0 {
		l.buf.Write(p[:min(len(p), room)])
	}
	l.n += len(p)
	return len(p), nil
}

// Result is what a plugin run produced.
type Result struct {
	Plugin   string
	ExitCode int
	Output   string // combined stdout and stderr, capped
	TimedOut bool
	Duration time.Duration
	RanAs    string
}

func (m *Manager) command(ctx context.Context, path string, ent Entry, args []string, extraEnv []string) (*exec.Cmd, string, error) {
	cred, who, err := m.dropTo(ent)
	if err != nil {
		return nil, "", err
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = append(append([]string{}, m.Env...), extraEnv...)
	cmd.Dir = "/"
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Credential: cred}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) } // the whole process group, not just the shell
	cmd.WaitDelay = 2 * time.Second
	return cmd, who, nil
}

// Hook tells one plugin about an event: JSON on stdin, HookTimeout, output captured.
func (m *Manager) Hook(ctx context.Context, name, event string, payload any) (*Result, error) {
	path, ent, err := m.verified(name)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(map[string]any{"event": event, "at": time.Now().UTC().Format(time.RFC3339), "data": payload})
	if err != nil {
		return nil, err
	}
	limit := HookTimeout
	if m.timeout > 0 {
		limit = m.timeout
	}
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	cmd, who, err := m.command(ctx, path, ent, []string{"hook", event}, []string{"WMS_PLUGIN_NAME=" + name, "WMS_PLUGIN_EVENT=" + event})
	if err != nil {
		return nil, err
	}
	out := &limited{}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = bytes.NewReader(body), out, out
	start := time.Now()
	runErr := cmd.Run()
	res := &Result{Plugin: name, Output: strings.TrimSpace(out.buf.String()), Duration: time.Since(start), RanAs: who, TimedOut: errors.Is(ctx.Err(), context.DeadlineExceeded)}
	var ee *exec.ExitError
	switch {
	case runErr == nil:
	case errors.As(runErr, &ee):
		res.ExitCode = ee.ExitCode()
	default:
		res.ExitCode = -1
		runErr = m.explain(runErr, path, who)
	}
	status := "SUCCESS"
	if res.ExitCode != 0 || res.TimedOut {
		status = "FAILED"
	}
	m.audit("PLUGIN_HOOK", status, fmt.Sprintf("%s event=%s exit=%d timeout=%v ran_as=%s", name, event, res.ExitCode, res.TimedOut, who))
	if runErr != nil && !res.TimedOut && res.ExitCode == -1 {
		return res, runErr
	}
	return res, nil
}

// Emit tells every enabled plugin subscribed to event, one after another, and
// returns their results. A failing plugin never stops the others or the caller.
func (m *Manager) Emit(ctx context.Context, event string, payload any) []*Result {
	if !containsStr(Events, event) {
		return nil
	}
	reg, err := m.load()
	if err != nil {
		return nil
	}
	var names []string
	for name, ent := range reg.Plugins {
		if ent.Enabled && containsStr(ent.Hooks, event) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	var results []*Result
	for _, n := range names {
		if res, err := m.Hook(ctx, n, event, payload); res != nil {
			results = append(results, res)
		} else if err != nil {
			m.audit("PLUGIN_HOOK", "FAILED", fmt.Sprintf("%s event=%s: %v", n, event, err))
		}
	}
	return results
}

// Run executes an enabled plugin as a command for the person at the keyboard: their
// terminal is attached, arguments are passed through, and there is no time limit.
func (m *Manager) Run(ctx context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	path, ent, err := m.verified(name)
	if err != nil {
		return 1, err
	}
	cmd, who, err := m.command(ctx, path, ent, args, []string{"WMS_PLUGIN_NAME=" + name, "TERM=" + orDefault(os.Getenv("TERM"), "dumb")})
	if err != nil {
		return 1, err
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
	runErr := cmd.Run()
	code := 0
	var ee *exec.ExitError
	if errors.As(runErr, &ee) {
		code = ee.ExitCode()
		runErr = nil
	} else if runErr != nil {
		runErr = m.explain(runErr, path, who)
	}
	status := "SUCCESS"
	if code != 0 || runErr != nil {
		status = "FAILED"
	}
	m.audit("PLUGIN_RUN", status, fmt.Sprintf("%s args=%d exit=%d ran_as=%s", name, len(args), code, who))
	return code, runErr
}

// Scaffold writes an example plugin (not enabled) so you have something to start from.
func (m *Manager) Scaffold(name string) (string, error) {
	p, err := m.path(name)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(m.Dir, 0o755); err != nil {
		return "", err
	}
	if err := m.CheckDir(); err != nil {
		return "", err
	}
	script := fmt.Sprintf(`#!/bin/bash
# wms plugin "%[1]s". Nothing runs until you review this file and enable it:
#   wms plugin enable %[1]s --hooks part_added,low_stock
# Run it by hand with:  wms %[1]s [arguments]
# It runs with a scrubbed environment, as an unprivileged user, so it can NOT read wms's keys or database.
set -euo pipefail

if [[ "${1:-}" == "hook" ]]; then
    event="${2:-unknown}"
    payload="$(cat)"            # the event as one line of JSON on stdin
    echo "%[1]s saw $event: $payload" >&2
    # e.g. curl -fsS -d "$payload" https://example.com/hook   (only if you trust that URL)
    exit 0
fi

echo "hello from the %[1]s plugin, arguments: $*"
`, name)
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		return "", err
	}
	return p, nil
}

// explain turns the bare "permission denied" of a failed exec into what to do about it.
func (m *Manager) explain(err error, path, who string) error {
	if errors.Is(err, os.ErrPermission) {
		return fmt.Errorf("%q cannot run %s: the plugin folder and every directory above it must be searchable by that user (chmod o+x), and the file readable (chmod o+rx) — or enable the plugin with --as-user <someone who can>: %w", who, path, err)
	}
	return err
}

// Default builds a Manager from the settings (WMS_PLUGIN_DIR, WMS_PLUGIN_USER).
// audit may be nil. When the folder does not exist every method is a cheap no-op or error.
func Default(audit func(action, status, details string)) *Manager {
	m := New(config.Get(config.PluginDir), config.Get(config.PluginUser))
	m.Audit = audit
	return m
}
