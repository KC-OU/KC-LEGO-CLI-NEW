package plugin

import (
	"bytes"
	"context"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newDir makes a safe plugin folder (0755, ours) with the given plugins.
func newDir(t *testing.T, plugins map[string]string) *Manager {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "plugins")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, script := range plugins {
		if err := os.WriteFile(filepath.Join(dir, "wms-"+name), []byte("#!/bin/bash\n"+script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// When the tests run as root, plugins drop to "nobody", who must be able to walk down to the folder:
	// t.TempDir() directories are private (0700), so open up the path (it is all under the test's temp dir).
	for p := dir; strings.HasPrefix(p, os.TempDir()) && p != os.TempDir(); p = filepath.Dir(p) {
		os.Chmod(p, 0o755)
	}
	return New(dir, "nobody")
}

func TestNothingRunsUntilEnabledAndOnlyFromThePluginFolder(t *testing.T) {
	m := newDir(t, map[string]string{"hello": `echo "hi $*"`})
	var out bytes.Buffer
	if _, err := m.Run(context.Background(), "hello", nil, nil, &out, &out); err == nil || !strings.Contains(err.Error(), "not enabled") {
		t.Fatalf("an unreviewed plugin must not run: %v", err)
	}
	if _, err := m.Enable("hello", nil, ""); err != nil {
		t.Fatal(err)
	}
	code, err := m.Run(context.Background(), "hello", []string{"a", "b"}, nil, &out, &out)
	if err != nil || code != 0 || strings.TrimSpace(out.String()) != "hi a b" {
		t.Fatalf("enabled plugin: code=%d err=%v out=%q", code, err, out.String())
	}
	// a program on $PATH is never used
	t.Setenv("PATH", "/usr/bin:/bin")
	if _, err := m.Run(context.Background(), "ls", nil, nil, &out, &out); err == nil {
		t.Error("only the plugin folder is searched")
	}
	if err := m.Disable("hello"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Run(context.Background(), "hello", nil, nil, &out, &out); err == nil {
		t.Error("a disabled plugin must not run")
	}
}

func TestNamesCannotEscapeTheFolder(t *testing.T) {
	m := newDir(t, nil)
	for _, bad := range []string{"", "../x", "a/b", "A", "x y", "..", ".hidden", strings.Repeat("a", 40), "a;b", "$(id)"} {
		if ValidName(bad) {
			t.Errorf("%q must not be a valid name", bad)
		}
		if _, err := m.Enable(bad, nil, ""); err == nil {
			t.Errorf("Enable(%q) must fail", bad)
		}
		if _, err := m.Run(context.Background(), bad, nil, nil, os.Stdout, os.Stderr); err == nil {
			t.Errorf("Run(%q) must fail", bad)
		}
	}
	for _, good := range []string{"a", "hello", "price-alert", "x_1"} {
		if !ValidName(good) {
			t.Errorf("%q should be valid", good)
		}
	}
}

func TestAPluginThatChangesAfterEnablingIsRefused(t *testing.T) {
	m := newDir(t, map[string]string{"tool": `echo v1`})
	var audit []string
	m.Audit = func(a, s, d string) { audit = append(audit, a+"/"+s) }
	if _, err := m.Enable("tool", nil, ""); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if code, err := m.Run(context.Background(), "tool", nil, nil, &out, &out); err != nil || code != 0 {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(m.Dir, "wms-tool"), []byte("#!/bin/bash\necho v2-evil\n"), 0o755)
	out.Reset()
	if _, err := m.Run(context.Background(), "tool", nil, nil, &out, &out); err == nil || !strings.Contains(err.Error(), "has changed since you enabled it") {
		t.Fatalf("a modified plugin must be refused: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("the modified plugin must not have run: %q", out.String())
	}
	if res, err := m.Hook(context.Background(), "tool", "part_added", nil); err == nil || res != nil {
		t.Errorf("hooks refuse it too: %v", err)
	}
	found := false
	for _, a := range audit {
		if a == "PLUGIN_REFUSED/DENIED" {
			found = true
		}
	}
	if !found {
		t.Errorf("the refusal is audited: %v", audit)
	}
	infos, _ := m.List()
	if len(infos) != 1 || !infos[0].Modified {
		t.Errorf("List flags it as modified: %+v", infos)
	}
	// re-enabling (after review) pins the new contents
	if _, err := m.Enable("tool", nil, ""); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code, err := m.Run(context.Background(), "tool", nil, nil, &out, &out); err != nil || code != 0 || !strings.Contains(out.String(), "v2-evil") {
		t.Errorf("re-enabled: %v %q", err, out.String())
	}
}

func TestUnsafeFoldersAndFilesAreRefused(t *testing.T) {
	m := newDir(t, map[string]string{"tool": `echo ok`})
	if _, err := m.Enable("tool", nil, ""); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	run := func() error { _, err := m.Run(context.Background(), "tool", nil, nil, &out, &out); return err }
	if err := run(); err != nil {
		t.Fatal(err)
	}
	// a group-writable plugin file
	os.Chmod(filepath.Join(m.Dir, "wms-tool"), 0o775)
	if err := run(); err == nil || !strings.Contains(err.Error(), "writable by other users") {
		t.Errorf("a group-writable plugin: %v", err)
	}
	os.Chmod(filepath.Join(m.Dir, "wms-tool"), 0o755)
	// a world-writable folder
	os.Chmod(m.Dir, 0o777)
	if err := run(); err == nil || !strings.Contains(err.Error(), "writable by other users") {
		t.Errorf("a world-writable folder: %v", err)
	}
	os.Chmod(m.Dir, 0o755)
	// a symlink in place of the program
	os.Remove(filepath.Join(m.Dir, "wms-tool"))
	os.WriteFile(filepath.Join(t.TempDir(), "real"), []byte("#!/bin/bash\necho evil\n"), 0o755)
	target := filepath.Join(filepath.Dir(m.Dir), "real")
	os.WriteFile(target, []byte("#!/bin/bash\necho evil\n"), 0o755)
	os.Symlink(target, filepath.Join(m.Dir, "wms-tool"))
	if err := run(); err == nil || !strings.Contains(err.Error(), "not a link") {
		t.Errorf("a symlinked plugin: %v", err)
	}
	// a non-executable file
	os.Remove(filepath.Join(m.Dir, "wms-tool"))
	os.WriteFile(filepath.Join(m.Dir, "wms-tool"), []byte("#!/bin/bash\necho ok\n"), 0o644)
	if _, err := m.Enable("tool", nil, ""); err == nil || !strings.Contains(err.Error(), "not executable") {
		t.Errorf("not executable: %v", err)
	}
	// the folder itself a symlink
	link := filepath.Join(t.TempDir(), "link")
	os.Symlink(m.Dir, link)
	if err := New(link, "").CheckDir(); err == nil {
		t.Error("the plugin folder must be a real directory")
	}
}

func TestThePluginSeesNoSecrets(t *testing.T) {
	t.Setenv("BRICKLINK_TOKEN", "SUPERSECRET-TOKEN")
	t.Setenv("PARTDB_API_TOKEN", "ANOTHER-SECRET")
	t.Setenv("REBRICKABLE_API_KEY", "KEY-SECRET")
	t.Setenv("WMS_SETTINGS_FILE", "/root/docker-server/wms/settings.json")
	m := newDir(t, map[string]string{"env": `env; echo "cwd=$(pwd)"`})
	m.Enable("env", nil, "")
	var out bytes.Buffer
	if _, err := m.Run(context.Background(), "env", nil, nil, &out, &out); err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"SECRET", "BRICKLINK", "PARTDB", "REBRICKABLE", "settings.json"} {
		if strings.Contains(out.String(), leak) {
			t.Errorf("the plugin's environment leaks %q:\n%s", leak, out.String())
		}
	}
	if !strings.Contains(out.String(), "WMS_PLUGIN_NAME=env") || !strings.Contains(out.String(), "cwd=/") {
		t.Errorf("it gets its name and a neutral directory:\n%s", out.String())
	}
}

func TestHooksGetJSONAndOnlyReachSubscribers(t *testing.T) {
	m := newDir(t, map[string]string{
		"listener": `if [[ "$1" == hook ]]; then echo "event=$2 env=$WMS_PLUGIN_EVENT"; cat; fi`,
		"quiet":    `echo "quiet was called"`,
	})
	var audit []string
	m.Audit = func(a, s, d string) { audit = append(audit, a+"/"+s+": "+d) }
	m.Enable("listener", []string{"part_added", "low_stock"}, "")
	m.Enable("quiet", []string{"price_drop"}, "")
	if _, err := m.Enable("quiet", []string{"not_an_event"}, ""); err == nil {
		t.Error("an unknown event is refused")
	}
	res := m.Emit(context.Background(), "part_added", map[string]any{"part": "3001", "qty": 5})
	if len(res) != 1 || res[0].Plugin != "listener" || res[0].ExitCode != 0 {
		t.Fatalf("results = %+v", res)
	}
	if !strings.Contains(res[0].Output, "event=part_added env=part_added") || !strings.Contains(res[0].Output, `"part":"3001"`) || !strings.Contains(res[0].Output, `"event":"part_added"`) {
		t.Errorf("the plugin gets the event as JSON on stdin: %q", res[0].Output)
	}
	if got := m.Emit(context.Background(), "backup_done", nil); len(got) != 0 {
		t.Errorf("nobody subscribed to backup_done: %+v", got)
	}
	if got := m.Emit(context.Background(), "made_up", nil); got != nil {
		t.Errorf("an unknown event does nothing: %+v", got)
	}
	found := false
	for _, a := range audit {
		if strings.HasPrefix(a, "PLUGIN_HOOK/SUCCESS: listener event=part_added exit=0") {
			found = true
		}
	}
	if !found {
		t.Errorf("every hook run is audited: %v", audit)
	}
}

func TestASlowOrFailingHookNeverBlocksTheOthersAndTimeoutKillsTheWholeTree(t *testing.T) {
	m := newDir(t, map[string]string{
		"a-slow": `if [[ "$1" == hook ]]; then (sleep 30 &) ; sleep 30; fi`,
		"b-fail": `if [[ "$1" == hook ]]; then echo boom >&2; exit 3; fi`,
		"c-ok":   `if [[ "$1" == hook ]]; then echo fine; fi`,
	})
	m.timeout = 600 * time.Millisecond
	for _, n := range []string{"a-slow", "b-fail", "c-ok"} {
		m.Enable(n, []string{"low_stock"}, "")
	}
	start := time.Now()
	res := m.Emit(context.Background(), "low_stock", nil)
	if time.Since(start) > 5*time.Second {
		t.Fatalf("a hung plugin blocked for %v", time.Since(start))
	}
	if len(res) != 3 {
		t.Fatalf("all three ran: %+v", res)
	}
	if !res[0].TimedOut {
		t.Errorf("the slow one timed out: %+v", res[0])
	}
	if res[1].ExitCode != 3 || !strings.Contains(res[1].Output, "boom") {
		t.Errorf("the failing one is reported: %+v", res[1])
	}
	if res[2].ExitCode != 0 || res[2].Output != "fine" {
		t.Errorf("a failure elsewhere does not stop the rest: %+v", res[2])
	}
}

func TestOutputIsCapped(t *testing.T) {
	m := newDir(t, map[string]string{"loud": `if [[ "$1" == hook ]]; then head -c 5000000 /dev/zero | tr '\0' 'x'; fi`})
	m.Enable("loud", []string{"low_stock"}, "")
	res := m.Emit(context.Background(), "low_stock", nil)
	if len(res) != 1 || len(res[0].Output) > maxOutput {
		t.Errorf("output must be capped at %d bytes: got %d", maxOutput, len(res[0].Output))
	}
}

func TestRunsAsAnUnprivilegedUserWhenWmsIsRoot(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("only meaningful when the tests run as root")
	}
	nobody, err := user.Lookup("nobody")
	if err != nil {
		t.Skip("no nobody user here")
	}
	// the plugin folder must be readable/executable by nobody for this to work
	m := newDir(t, map[string]string{"whoami": `if [[ "$1" == hook ]]; then id -u; cat >/dev/null; fi`})
	os.Chmod(filepath.Dir(m.Dir), 0o755)
	os.Chmod(m.Dir, 0o755)
	m.Enable("whoami", []string{"low_stock"}, "")
	res := m.Emit(context.Background(), "low_stock", nil)
	if len(res) != 1 || res[0].RanAs != "nobody" || strings.TrimSpace(res[0].Output) != nobody.Uid {
		t.Fatalf("hooks must run as nobody (%s): %+v", nobody.Uid, res)
	}
	m.Enable("whoami", []string{"low_stock"}, "root")
	res = m.Emit(context.Background(), "low_stock", nil)
	if len(res) != 1 || strings.TrimSpace(res[0].Output) != "0" {
		t.Errorf("--as-root is the explicit opt-in: %+v", res)
	}
	if _, err := m.Enable("whoami", nil, "no-such-user-xyz"); err == nil {
		t.Error("an unknown run-as user is refused")
	}
}

func TestScaffoldAndList(t *testing.T) {
	m := newDir(t, map[string]string{"real": `echo hi`})
	p, err := m.Scaffold("mine")
	if err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o755 {
		t.Errorf("mode = %v", fi.Mode().Perm())
	}
	if _, err := m.Scaffold("../x"); err == nil {
		t.Error("scaffold validates the name")
	}
	m.Enable("real", []string{"low_stock"}, "")
	os.WriteFile(filepath.Join(m.Dir, "README"), []byte("not a plugin"), 0o644)
	os.WriteFile(filepath.Join(m.Dir, "wms-BAD NAME"), []byte("x"), 0o755)
	infos, err := m.List()
	if err != nil || len(infos) != 2 {
		t.Fatalf("List = %+v %v", infos, err)
	}
	if infos[0].Name != "mine" || infos[0].Entry.Enabled || infos[1].Name != "real" || !infos[1].Entry.Enabled || infos[1].Modified {
		t.Errorf("states: %+v", infos)
	}
	// the scaffolded plugin works once reviewed and enabled
	m.Enable("mine", []string{"part_added"}, "")
	var out bytes.Buffer
	if code, err := m.Run(context.Background(), "mine", []string{"x"}, nil, &out, &out); err != nil || code != 0 || !strings.Contains(out.String(), "hello from the mine plugin") {
		t.Errorf("scaffold: %v %q", err, out.String())
	}
	res := m.Emit(context.Background(), "part_added", map[string]string{"part": "3001"})
	if len(res) != 1 || !strings.Contains(res[0].Output, "mine saw part_added") {
		t.Errorf("scaffold hook: %+v", res)
	}
	// a plugin enabled once whose file was deleted is listed with a problem
	os.Remove(filepath.Join(m.Dir, "wms-real"))
	infos, _ = m.List()
	var missing bool
	for _, i := range infos {
		if i.Name == "real" && i.Problem == "the file is missing" {
			missing = true
		}
	}
	if !missing {
		t.Errorf("a deleted plugin is reported: %+v", infos)
	}
}

func FuzzPluginNames(f *testing.F) {
	for _, s := range []string{"a", "../x", "a/b", "", "hello", "..", "a\x00b", "x y", "é"} {
		f.Add(s)
	}
	m := New("/plugins", "nobody")
	f.Fuzz(func(t *testing.T, name string) {
		p, err := m.path(name)
		if err != nil {
			return
		}
		if filepath.Dir(p) != "/plugins" || !strings.HasPrefix(filepath.Base(p), "wms-") || strings.ContainsAny(name, "/\x00") {
			t.Fatalf("name %q produced %q, which escapes the plugin folder", name, p)
		}
	})
}
