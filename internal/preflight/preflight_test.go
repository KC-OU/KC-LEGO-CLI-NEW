package preflight

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
)

type fakeRunner struct {
	out   map[string]string // "name arg arg" -> output
	fail  map[string]bool
	after func(name string, env []string) // runs before the answer (a program with side effects)
	calls []string
}

func (f *fakeRunner) Run(ctx context.Context, dir string, env []string, name string, args ...string) (string, error) {
	key := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, key)
	if f.after != nil {
		f.after(name, env)
	}
	for k, v := range f.out {
		if strings.Contains(key, k) {
			if f.fail[k] {
				return v, errors.New("exit status 1")
			}
			return v, nil
		}
	}
	return "", nil
}

func env(t *testing.T, r Runner) *Env {
	t.Helper()
	dir := t.TempDir()
	tools := filepath.Join(dir, "tools") // stand-ins, so no check tries to install the real tools
	_ = os.MkdirAll(tools, 0o755)
	for _, n := range []string{"staticcheck", "govulncheck", "gosec"} {
		_ = os.WriteFile(filepath.Join(tools, n), nil, 0o755)
	}
	return &Env{Repo: dir, Run: r, Now: time.Now, Scratch: filepath.Join(dir, "scratch"), Trap: filepath.Join(dir, "trap"), ToolDir: tools}
}

func TestSelectHonoursTargetAndQuick(t *testing.T) {
	cs := []Check{
		{Name: "a", Targets: both},
		{Name: "slow", Targets: both, Slow: true},
		{Name: "pub", Targets: []string{TargetPublish}},
		{Name: "dep", Targets: []string{TargetDeploy}},
	}
	names := func(cs []Check) string {
		var n []string
		for _, c := range cs {
			n = append(n, c.Name)
		}
		return strings.Join(n, ",")
	}
	for _, tc := range []struct {
		target string
		quick  bool
		want   string
	}{{TargetDeploy, false, "a,slow,dep"}, {TargetPublish, false, "a,slow,pub"}, {TargetAll, false, "a,slow,pub,dep"}, {TargetAll, true, "a,pub,dep"}} {
		if got := names(Select(cs, tc.target, tc.quick)); got != tc.want {
			t.Errorf("%s quick=%v: %s, want %s", tc.target, tc.quick, got, tc.want)
		}
	}
}

func TestACrashingCheckIsAFailureNotACrash(t *testing.T) {
	cs := []Check{{Name: "boom", Run: func(context.Context, *Env) Result { panic("x") }}, {Name: "fine", Run: func(context.Context, *Env) Result { return Pass("ok") }}}
	outs := Execute(context.Background(), &Env{}, cs, nil)
	if len(outs) != 2 || outs[0].Result.Status != Fail || outs[1].Result.Status != OK {
		t.Fatalf("%+v", outs)
	}
	if v := Summarise("deploy", "abc", false, time.Now(), outs); v.Go || v.Failures != 1 || v.Passed != 1 {
		t.Errorf("one failure is a NO-GO: %+v", v)
	}
}

func TestACancelledRunSkipsTheRest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cs := []Check{
		{Name: "first", Run: func(context.Context, *Env) Result { cancel(); return Pass("done") }},
		{Name: "second", Run: func(context.Context, *Env) Result { t.Error("must not run"); return Pass("") }},
	}
	outs := Execute(ctx, &Env{Workers: 1}, cs, nil) // one at a time: what is still queued is skipped
	if outs[1].Result.Status != Skip {
		t.Errorf("%+v", outs[1])
	}
}

func TestAVerdictOnlyLetsTheSameCommitShipWhileItIsFresh(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "v.json")
	now := time.Now()
	if _, why := Fresh(path, "deploy", "abc", "p", time.Minute, now, false); !strings.Contains(why, "no preflight") {
		t.Errorf("none saved: %q", why)
	}
	good := Verdict{Target: "all", Head: "abcdef123456", At: now, Go: true, Paths: "p"}
	if err := SaveVerdict(path, good); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Errorf("the verdict file is private: %v", fi.Mode())
	}
	cases := []struct {
		name   string
		v      Verdict
		target string
		head   string
		age    time.Duration
		quick  bool
		want   string
	}{
		{"fresh GO for all covers deploy", good, "deploy", "abcdef123456", 0, false, ""},
		{"other commit", good, "deploy", "ffffffff", 0, false, "code has changed"},
		{"too old", good, "deploy", "abcdef123456", 2 * time.Hour, false, "old"},
		{"no-go", Verdict{Target: "all", Head: "abcdef123456", At: now, Paths: "p"}, "deploy", "abcdef123456", 0, false, "NO-GO"},
		{"quick is not enough", Verdict{Target: "all", Head: "abcdef123456", At: now, Go: true, Quick: true, Paths: "p"}, "deploy", "abcdef123456", 0, false, "--quick"},
		{"earned on scratch data", Verdict{Target: "all", Head: "abcdef123456", At: now, Go: true, Paths: "scratch"}, "deploy", "abcdef123456", 0, false, "different files"},
		{"other target", Verdict{Target: "publish", Head: "abcdef123456", At: now, Go: true, Paths: "p"}, "deploy", "abcdef123456", 0, false, "not deploy"},
	}
	for _, c := range cases {
		if err := SaveVerdict(path, c.v); err != nil {
			t.Fatal(err)
		}
		_, why := Fresh(path, c.target, c.head, "p", time.Hour, now.Add(c.age), c.quick)
		if (c.want == "") != (why == "") || !strings.Contains(why, c.want) {
			t.Errorf("%s: got %q, want %q", c.name, why, c.want)
		}
	}
}

func TestTheScanFindsSecretsAndPersonalDataWithoutEverPrintingThem(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "sub")
	os.MkdirAll(dir, 0o755)
	secretValue := "s3cr3t-value-that-must-not-leak"
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n// the key is "+secretValue+"\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.md"), []byte("mail me at someone@private-domain.example\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "c.txt"), []byte("-----BEGIN RSA "+"PRIVATE KEY-----\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "d.txt"), []byte("clean line\nnothing here\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "e.bin"), []byte("\x00\x00"+secretValue), 0o644)

	settings := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(settings, []byte(`{"REBRICKABLE_API_KEY":"`+secretValue+`","short":"abc","nested":{"x":"another-long-secret-1"}}`), 0o600)
	pii := filepath.Join(t.TempDir(), "pii.txt")
	os.WriteFile(pii, []byte("# personal\nprivate-domain\\.example\n"), 0o600)

	secrets := LoadSecrets(settings, "", "")
	if len(secrets) != 2 {
		t.Fatalf("short values are ignored, nested ones are read: %+v", secrets)
	}
	res, bad := LoadPII(pii)
	if len(res) != 1 || len(bad) != 0 {
		t.Fatalf("%v %v", res, bad)
	}
	finds, err := ScanTree(root, res, secrets)
	if err != nil {
		t.Fatal(err)
	}
	joined := ""
	for _, f := range finds {
		joined += f.String() + "\n"
	}
	for _, want := range []string{"sub/a.go:2 (value from settings.json:REBRICKABLE_API_KEY)", "sub/b.md:1 (personal-data pattern)", "sub/c.txt:1 (private key block)"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, secretValue) || strings.Contains(joined, "private-domain") || strings.Contains(joined, "d.txt") || strings.Contains(joined, "e.bin") {
		t.Errorf("a finding must never carry the matched text, and clean or binary files are not hits:\n%s", joined)
	}
}

func TestDataFilesAndLargeFilesAreFlagged(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "lego.db"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(root, "settings.json"), []byte("{}"), 0o644)
	os.WriteFile(filepath.Join(root, "config.example.env"), []byte("A=1"), 0o644)
	os.WriteFile(filepath.Join(root, "big.txt"), make([]byte, 2<<20), 0o644)
	data, large := DataFiles(root)
	if strings.Join(data, ",") != "lego.db,settings.json" || len(large) != 1 || large[0] != "big.txt" {
		t.Errorf("data=%v large=%v", data, large)
	}
}

func TestCleanTreeCheck(t *testing.T) {
	f := &fakeRunner{out: map[string]string{"git status": " M x.go\n", "git rev-parse": "main\n"}}
	if r := checkClean(context.Background(), env(t, f)); r.Status != Fail || !strings.Contains(r.Detail, "uncommitted") {
		t.Errorf("%+v", r)
	}
	f = &fakeRunner{out: map[string]string{"git status": "", "git rev-parse": "feature\n"}}
	if r := checkClean(context.Background(), env(t, f)); r.Status != Warn {
		t.Errorf("a clean tree on another branch is a warning: %+v", r)
	}
	f = &fakeRunner{out: map[string]string{"git status": "", "git rev-parse": "main\n"}}
	if r := checkClean(context.Background(), env(t, f)); r.Status != OK {
		t.Errorf("%+v", r)
	}
}

func TestGosecOnlyStopsOnNewHighFindings(t *testing.T) {
	accepted := `{"Issues":[{"severity":"HIGH","rule_id":"G115","file":"/x/internal/plugin/plugin.go"},{"severity":"MEDIUM","rule_id":"G304","file":"/x/a.go"}]}`
	e := env(t, &fakeRunner{out: map[string]string{"gosec": "Autofix:\n" + accepted}})
	e.Repo = "/x"
	if r := checkGosec(context.Background(), e); r.Status != OK {
		t.Errorf("reviewed findings are accepted: %+v", r)
	}
	fresh := `{"Issues":[{"severity":"HIGH","rule_id":"G402","file":"/x/internal/api/server.go"}]}`
	e = env(t, &fakeRunner{out: map[string]string{"gosec": fresh}, fail: map[string]bool{"gosec": true}})
	e.Repo = "/x"
	if r := checkGosec(context.Background(), e); r.Status != Fail || !strings.Contains(r.Detail, "G402 internal/api/server.go") {
		t.Errorf("a new HIGH is a NO-GO: %+v", r)
	}
}

func TestATestThatWritesToADefaultPathIsCaught(t *testing.T) {
	e := env(t, nil)
	e.Run = &fakeRunner{after: func(name string, envv []string) {
		for _, kv := range envv {
			if strings.HasPrefix(kv, "LEGO_DB_PATH=") {
				os.WriteFile(strings.TrimPrefix(kv, "LEGO_DB_PATH="), []byte("x"), 0o600) // what a leaky test does
			}
		}
	}}
	if r := checkTests(context.Background(), e); r.Status != Fail || !strings.Contains(r.Detail, "lego.db") {
		t.Errorf("%+v", r)
	}
	e = env(t, &fakeRunner{})
	if r := checkTests(context.Background(), e); r.Status != OK || !strings.Contains(r.Detail, "-race") {
		t.Errorf("%+v", r)
	}
	e.Quick = true
	f := &fakeRunner{}
	e.Run = f
	checkTests(context.Background(), e)
	if strings.Contains(strings.Join(f.calls, ";"), "-race") {
		t.Errorf("--quick leaves the race detector out: %v", f.calls)
	}
}

func TestDeploySettingsMustBePrivateAndComplete(t *testing.T) {
	dir := t.TempDir()
	e := &Env{Settings: filepath.Join(dir, "settings.json"), DeployValues: filepath.Join(dir, "deploy.env")}
	os.WriteFile(e.Settings, []byte("{}"), 0o644)
	if r := checkSettings(context.Background(), e); r.Status != Fail || !strings.Contains(r.Detail, "mode 644") {
		t.Errorf("%+v", r)
	}
	os.Chmod(e.Settings, 0o600)
	if r := checkSettings(context.Background(), e); r.Status != Fail || !strings.Contains(r.Detail, "missing") {
		t.Errorf("%+v", r)
	}
	os.WriteFile(e.DeployValues, []byte("PARTDB_URL=https://x\nMODERNWMS_URL=https://y\nSYNC_VIEWONLY_AUTH=\n"), 0o600)
	r := checkSettings(context.Background(), e)
	if r.Status != Fail || !strings.Contains(r.Detail, "SYNC_VIEWONLY_AUTH") || !strings.Contains(r.Detail, "SYNC_VIEWONLY_EMAIL") {
		t.Errorf("empty and absent values are both missing: %+v", r)
	}
	os.WriteFile(e.DeployValues, []byte("PARTDB_URL=https://x\nMODERNWMS_URL=https://y\nSYNC_VIEWONLY_AUTH=abc\nSYNC_VIEWONLY_EMAIL=v@example.test\n"), 0o600)
	if r := checkSettings(context.Background(), e); r.Status != OK {
		t.Errorf("%+v", r)
	}
}

func TestShipScriptsMustParseAndRequireAPreflight(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "deploy.sh")
	os.WriteFile(good, []byte("#!/bin/bash\nwms-go preflight --require deploy\n"), 0o755)
	e := &Env{Repo: dir, Run: ExecRunner{}, Scripts: []string{good}}
	if r := checkScripts(context.Background(), e); r.Status != OK {
		t.Errorf("%+v", r)
	}
	nogate := filepath.Join(dir, "publish.sh")
	os.WriteFile(nogate, []byte("#!/bin/bash\necho hi\n"), 0o755)
	e.Scripts = []string{nogate}
	if r := checkScripts(context.Background(), e); r.Status != Fail || !strings.Contains(r.Detail, "does not require") {
		t.Errorf("%+v", r)
	}
	broken := filepath.Join(dir, "x.sh")
	os.WriteFile(broken, []byte("#!/bin/bash\npreflight\nif then fi (\n"), 0o755)
	e.Scripts = []string{broken}
	if r := checkScripts(context.Background(), e); r.Status != Fail {
		t.Errorf("a syntax error is a NO-GO: %+v", r)
	}
}

func TestRepoFilesCheck(t *testing.T) {
	e := env(t, nil)
	e.Module = "example.com/m"
	os.WriteFile(filepath.Join(e.Repo, "go.mod"), []byte("module example.com/other\n"), 0o644)
	if r := checkRepoFiles(context.Background(), e); r.Status != Fail || !strings.Contains(r.Detail, "expected module") {
		t.Errorf("%+v", r)
	}
	os.WriteFile(filepath.Join(e.Repo, "go.mod"), []byte("module example.com/m\n"), 0o644)
	if r := checkRepoFiles(context.Background(), e); r.Status != Fail || !strings.Contains(r.Detail, "LICENSE") {
		t.Errorf("%+v", r)
	}
	for _, f := range []string{"LICENSE", "SECURITY.md", "CONTRIBUTING.md", "README.md"} {
		os.WriteFile(filepath.Join(e.Repo, f), []byte("x"), 0o644)
	}
	os.WriteFile(filepath.Join(e.Repo, "CHANGELOG.md"), []byte("## [Unreleased]\n"), 0o644)
	if r := checkRepoFiles(context.Background(), e); r.Status != OK {
		t.Errorf("%+v", r)
	}
}

func TestOutputFitsAnEightyColumnTerminalAndTheVerdictIsUnmissable(t *testing.T) {
	long := strings.Repeat("a very long detail ", 20)
	outs := []Outcome{
		{Check: Check{Group: "Code", Name: "gofmt"}, Result: Pass("formatted"), Duration: 120 * time.Millisecond},
		{Check: Check{Group: "Publish", Name: "no secrets or personal data in what would be published"}, Result: Stop(long, "remove it"), Duration: 2 * time.Second},
		{Check: Check{Group: "Deploy", Name: "backups"}, Result: Caution(long, "run wms backup"), Duration: time.Second},
	}
	for _, o := range outs {
		if w := lipgloss.Width(line(o, 80)); w > 80 {
			t.Errorf("a progress line is %d columns wide", w)
		}
	}
	v := Summarise("deploy", "abcdef123456", false, time.Now(), outs)
	var sb strings.Builder
	Report(&sb, outs, v, 80)
	plain := regexp.MustCompile(`\x1b\[[0-9;]*m`).ReplaceAllString(sb.String(), "")
	for _, want := range []string{"Must fix", "Worth a look", "NO-GO", "do not deploy yet", "→ remove it", "1 passed · 1 warning(s) · 1 failure(s)"} {
		if !strings.Contains(plain, want) {
			t.Errorf("report lacks %q:\n%s", want, plain)
		}
	}
	for _, l := range strings.Split(plain, "\n") {
		if lipgloss.Width(l) > 80 {
			t.Errorf("report row of %d columns: %q", lipgloss.Width(l), l)
		}
	}
	if b := regexp.MustCompile(`\x1b\[[0-9;]*m`).ReplaceAllString(Banner(Verdict{Target: "publish", Go: true, Passed: 3, Quick: true}), ""); !strings.Contains(b, "GO") || !strings.Contains(b, "quick run") {
		t.Errorf("%q", b)
	}
}

func TestTheAnimatedViewNeverExceedsTheTerminal(t *testing.T) {
	m := model{header: "deploy · abc12345", total: 40, start: time.Now(), width: 80, height: 24}
	for i := 0; i < 30; i++ {
		m.outs = append(m.outs, Outcome{Check: Check{Group: "Code", Name: "check number " + strings.Repeat("x", i)}, Result: Pass("fine"), Duration: time.Second})
	}
	ev := Event{Check: Check{Group: "Deploy", Name: "the current one"}}
	m.cur = &ev
	for _, size := range [][2]int{{80, 24}, {80, 20}, {120, 40}, {60, 24}} {
		m.width, m.height = size[0], size[1]
		lines := strings.Split(strings.TrimRight(m.View(), "\n"), "\n")
		if len(lines) > size[1]-1 {
			t.Errorf("%dx%d: %d lines", size[0], size[1], len(lines))
		}
		for _, l := range lines {
			if w := lipgloss.Width(l); w > size[0] {
				t.Errorf("%dx%d: a line is %d columns", size[0], size[1], w)
			}
		}
	}
}

func TestASymbolicLinkInTheTreeIsReportedNotFollowed(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	os.WriteFile(outside, []byte("-----BEGIN RSA "+"PRIVATE KEY-----\n"), 0o600)
	if err := os.Symlink(outside, filepath.Join(root, "link.txt")); err != nil {
		t.Skip("no symlinks here")
	}
	finds, err := ScanTree(root, nil, nil)
	if err != nil || len(finds) != 1 || !strings.Contains(finds[0].What, "symbolic link") {
		t.Fatalf("%v %v", finds, err)
	}
}

func TestParallelRunKeepsTheChecksOrder(t *testing.T) {
	var cs []Check
	for i := 0; i < 12; i++ {
		d := time.Duration(12-i) * time.Millisecond // later checks finish first
		st := []Status{OK, Warn, Fail}[i%3]
		cs = append(cs, Check{Name: fmt.Sprint(i), Run: func(context.Context, *Env) Result {
			time.Sleep(d)
			return Result{Status: st, Detail: fmt.Sprint(i)}
		}})
	}
	var events int
	par := Execute(context.Background(), &Env{Workers: 4}, cs, func(Event) { events++ })
	ser := Execute(context.Background(), &Env{Workers: 1}, cs, nil)
	for i := range cs {
		if par[i].Check.Name != ser[i].Check.Name || par[i].Result != ser[i].Result {
			t.Fatalf("outcome %d differs: %+v vs %+v", i, par[i], ser[i])
		}
	}
	if events != 24 {
		t.Errorf("every check reports start and done: %d events", events)
	}
}
