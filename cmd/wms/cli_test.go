package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/auth"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/partdb"
)

func TestExitCodes(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{errors.New("boom"), exitFailure},
		{usageError("bad"), exitUsage},
		{errors.New(`unknown flag: --nope`), exitUsage},
		{errors.New(`accepts 1 arg(s), received 0`), exitUsage},
		{auth.ErrInvalidCredentials, exitAuth},
		{fmt.Errorf("wrapped: %w", partdb.ErrNoToken), exitAuth},
		{&partdb.APIError{Status: 401}, exitAuth},
		{&partdb.APIError{Status: 403}, exitAuth},
		{&partdb.APIError{Status: 404}, exitNotFound},
		{&partdb.APIError{Status: 500}, exitNetwork},
		{context.DeadlineExceeded, exitNetwork},
		{errors.New(`user "bob" not found in either system`), exitNotFound},
		{withCode(exitNotFound, errors.New("part 9 gone")), exitNotFound},
		{fmt.Errorf("outer: %w", withCode(exitAuth, errors.New("inner"))), exitAuth},
	}
	for _, c := range cases {
		if got := exitCodeFor(c.err); got != c.want {
			t.Errorf("exitCodeFor(%v) = %d, want %d", c.err, got, c.want)
		}
	}
}

// run executes the real command tree and returns stdout, stderr text and the exit status.
func run(t *testing.T, args ...string) (stdout string, code int) {
	t.Helper()
	oldOut, oldErr, oldIn := os.Stdout, os.Stderr, os.Stdin
	r, w, _ := os.Pipe()
	os.Stdout, os.Stderr = w, w
	null, _ := os.Open(os.DevNull) // stdin is not a terminal
	os.Stdin = null
	out.JSON, out.Quiet = false, false
	root := newRootCmd()
	root.SilenceErrors, root.SilenceUsage = true, true
	root.SetOut(w)
	root.SetErr(w)
	err := execute(root, args)
	code = 0
	if err != nil {
		code = reportError(err)
	}
	w.Close()
	os.Stdout, os.Stderr, os.Stdin = oldOut, oldErr, oldIn
	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String(), code
}

func TestDryRunChangesNothingAndSaysWhat(t *testing.T) {
	stdout, code := run(t, "users", "create", "bob", "Picker", "--dry-run")
	if code != 0 || !strings.Contains(stdout, "dry-run") || !strings.Contains(stdout, `"bob"`) {
		t.Fatalf("code=%d out=%q", code, stdout)
	}
	stdout, code = run(t, "users", "delete", "wms", "bob", "--dry-run", "--json")
	var doc map[string]any
	if code != 0 || json.Unmarshal([]byte(stdout), &doc) != nil || doc["dry_run"] != true {
		t.Fatalf("--dry-run --json should print one JSON document: code=%d out=%q", code, stdout)
	}
}

func TestDestructiveCommandsRefuseToGuessWithoutATerminal(t *testing.T) {
	stdout, code := run(t, "users", "delete", "wms", "bob") // stdin is /dev/null, no --yes
	if code != exitUsage || !strings.Contains(stdout, "--yes") {
		t.Fatalf("code=%d out=%q", code, stdout)
	}
}

func TestQuietPrintsNothingOnSuccess(t *testing.T) {
	stdout, code := run(t, "users", "create", "bob", "--dry-run", "--quiet")
	if code != 0 || strings.TrimSpace(stdout) != "" {
		t.Fatalf("--quiet must print nothing: code=%d out=%q", code, stdout)
	}
}

func TestUsageErrorsAndJSONErrors(t *testing.T) {
	if _, code := run(t, "users", "toggle", "wms", "bob"); code != exitUsage { // neither --enable nor --disable
		t.Errorf("missing --enable/--disable = %d, want %d", code, exitUsage)
	}
	if _, code := run(t, "users", "list", "--nope"); code != exitUsage {
		t.Errorf("unknown flag = %d, want %d", code, exitUsage)
	}
	if _, code := run(t, "users", "reset"); code != exitUsage {
		t.Errorf("missing argument = %d, want %d", code, exitUsage)
	}
	stdout, code := run(t, "users", "reset", "--json")
	var e map[string]any
	if code != exitUsage || json.Unmarshal([]byte(strings.TrimSpace(stdout)), &e) != nil || e["code"] != float64(exitUsage) || e["error"] == "" {
		t.Errorf("--json errors are one JSON object on stderr: code=%d out=%q", code, stdout)
	}
}

func TestCompletionHelpers(t *testing.T) {
	got := withPrefix([]string{"bob", "Bobby", "alice", "bob", ""}, "bo")
	if strings.Join(got, ",") != "Bobby,bob" {
		t.Errorf("withPrefix = %v", got)
	}
	names, _ := completeSystemThenUser(nil, nil, "p")
	if len(names) != 1 || names[0] != "partdb" {
		t.Errorf("system completion = %v", names)
	}
}

// pluginEnv gives the CLI a scratch plugin folder (traversable, so the unprivileged user can run from it).
func pluginEnv(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "plugins")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for p := dir; strings.HasPrefix(p, os.TempDir()) && p != os.TempDir(); p = filepath.Dir(p) {
		os.Chmod(p, 0o755)
	}
	t.Setenv("WMS_PLUGIN_DIR", dir)
	t.Setenv("AUDIT_LOG_FILE", filepath.Join(t.TempDir(), "audit.log"))
	return dir
}

func TestPluginCLILifecycleAndDispatch(t *testing.T) {
	dir := pluginEnv(t)
	if _, code := run(t, "plugin", "new", "greeter"); code != 0 {
		t.Fatalf("plugin new: %d", code)
	}
	if stdout, _ := run(t, "plugin", "list"); !strings.Contains(stdout, "greeter") || !strings.Contains(stdout, "not enabled") {
		t.Errorf("list:\n%s", stdout)
	}
	if _, code := run(t, "plugin", "new", "../evil"); code != exitUsage {
		t.Errorf("a bad name = %d", code)
	}
	// `wms greeter` before enabling: refused
	if stdout, code := run(t, "greeter", "x"); code == 0 || !strings.Contains(stdout, "not enabled") {
		t.Errorf("unreviewed plugin: code=%d %q", code, stdout)
	}
	if _, code := run(t, "plugin", "enable", "greeter", "--hooks", "part_added"); code != exitUsage {
		t.Errorf("enabling needs --yes when there is no terminal: %d", code)
	}
	if _, code := run(t, "plugin", "enable", "greeter", "--hooks", "not_real", "--yes"); code != exitUsage {
		t.Errorf("unknown event: %d", code)
	}
	stdout, code := run(t, "plugin", "enable", "greeter", "--hooks", "part_added", "--yes")
	if code != 0 || !strings.Contains(stdout, "SHA-256") {
		t.Fatalf("enable: code=%d %q", code, stdout)
	}
	// dispatch: `wms greeter hello world` reaches the plugin
	stdout, code = run(t, "greeter", "hello", "world")
	if code != 0 || !strings.Contains(stdout, "hello from the greeter plugin, arguments: hello world") {
		t.Errorf("dispatch: code=%d %q", code, stdout)
	}
	// a built-in command always wins over a plugin of the same name
	os.WriteFile(filepath.Join(dir, "wms-users"), []byte("#!/bin/bash\necho HIJACKED\n"), 0o755)
	run(t, "plugin", "enable", "users", "--yes")
	if stdout, _ := run(t, "users", "--help"); strings.Contains(stdout, "HIJACKED") {
		t.Error("a plugin must never replace a built-in command")
	}
	// tampering
	os.WriteFile(filepath.Join(dir, "wms-greeter"), []byte("#!/bin/bash\necho evil\n"), 0o755)
	if stdout, code := run(t, "greeter"); code == 0 || strings.Contains(stdout, "evil") || !strings.Contains(stdout, "changed since you enabled") {
		t.Errorf("tampered: code=%d %q", code, stdout)
	}
	if stdout, _ := run(t, "plugin", "list", "--json"); !strings.Contains(stdout, `"Modified": true`) {
		t.Errorf("list shows the change:\n%s", stdout)
	}
	if stdout, _ := run(t, "doctor", "--json"); !strings.Contains(stdout, "changed since it was enabled") {
		t.Errorf("doctor flags it:\n%s", stdout)
	}
	if _, code := run(t, "plugin", "disable", "greeter"); code != 0 {
		t.Error("disable")
	}
	if _, code := run(t, "plugin", "disable", "never-there"); code != exitNotFound {
		t.Errorf("disable unknown = %d", code)
	}
	// an unknown command that is not a plugin is an ordinary usage error
	if _, code := run(t, "definitely-not-a-command"); code != exitUsage {
		t.Errorf("unknown command = %d", code)
	}
}

func TestHooksFireFromCLIActionsAndAreAudited(t *testing.T) {
	dir := pluginEnv(t)
	log := filepath.Join(t.TempDir(), "hook.log")
	os.WriteFile(filepath.Join(dir, "wms-logger"), []byte("#!/bin/bash\nif [[ \"$1\" == hook ]]; then cat >> "+log+"; echo >> "+log+"; fi\n"), 0o755)
	os.Chmod(filepath.Dir(log), 0o777)
	if _, code := run(t, "plugin", "enable", "logger", "--hooks", "backup_done,catalog_refreshed,part_added", "--yes"); code != 0 {
		t.Fatal("enable")
	}
	emitHook("backup_done", map[string]any{"file": "/x/y.db"})
	emitHook("low_stock", map[string]any{}) // not subscribed
	b, _ := os.ReadFile(log)
	if !strings.Contains(string(b), `"event":"backup_done"`) || !strings.Contains(string(b), "/x/y.db") || strings.Contains(string(b), "low_stock") {
		t.Errorf("hook log: %q", b)
	}
	a, _ := os.ReadFile(os.Getenv("AUDIT_LOG_FILE"))
	if !strings.Contains(string(a), "ACTION:PLUGIN_ENABLED") || !strings.Contains(string(a), "ACTION:PLUGIN_HOOK | STATUS:SUCCESS") {
		t.Errorf("audit log:\n%s", a)
	}
}

func TestCLIReferenceIsGeneratedFromTheCommandTree(t *testing.T) {
	dir := t.TempDir()
	if _, code := run(t, "docs-gen", dir); code != 0 {
		t.Fatal("docs-gen failed")
	}
	idx, err := os.ReadFile(filepath.Join(dir, "index.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"lego-search-parts", "lego-export", "bricklink-price", "plugin-enable", "backup-decrypt", "update", "doctor"} {
		if !strings.Contains(string(idx), "("+want+".md)") {
			t.Errorf("the index lacks %s", want)
		}
	}
	if strings.Contains(string(idx), "docs-gen") || strings.Contains(string(idx), "completion") {
		t.Error("hidden and built-in helper commands are left out")
	}
	page, _ := os.ReadFile(filepath.Join(dir, "lego-export.md"))
	for _, want := range []string{"# `wms lego export`", "rebrickable-csv", "## Examples", "## Options", "--format"} {
		if !strings.Contains(string(page), want) {
			t.Errorf("lego-export page lacks %q:\n%s", want, page)
		}
	}
	if page, _ := os.ReadFile(filepath.Join(dir, "lego.md")); !strings.Contains(string(page), "## Subcommands") {
		t.Error("a command group lists its subcommands")
	}
}
