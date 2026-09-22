package preflight

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	_ "modernc.org/sqlite" // registers the driver for the read-only integrity check of the live database
)

var both = []string{TargetDeploy, TargetPublish}

// acceptedGosec are gosec findings reviewed and accepted (false positives or deliberate: an integer
// that provably fits, a settings key that is named like a credential but holds none, a path already
// validated). Anything HIGH that is not listed here is a NO-GO.
var acceptedGosec = map[string]bool{
	"G115 internal/ui/img/render.go":    true,
	"G115 internal/plugin/plugin.go":    true,
	"G101 internal/config/config.go":    true,
	"G703 cmd/wms/plugin.go":            true,
	"G115 internal/preflight/checks.go": true, // a free-block count times the block size, far below 2^63
}

// Standard returns every built-in check, in the order they run. extra checks (the ones that reuse
// the doctor's code in package main) are appended by the caller.
func Standard() []Check {
	var cs []Check
	add := func(group, name string, targets []string, slow bool, run func(context.Context, *Env) Result) {
		cs = append(cs, Check{Group: group, Name: name, Targets: targets, Slow: slow, Run: run})
	}

	// ---- code ----
	add("Code", "working tree is clean", both, false, checkClean)
	add("Code", "gofmt", both, false, func(ctx context.Context, e *Env) Result {
		out, err := e.run(ctx, "gofmt", "-l", ".")
		if err != nil {
			return Stop("gofmt failed: "+lastLine(out), "")
		}
		if strings.TrimSpace(out) != "" {
			return Stop(fmt.Sprintf("%d file(s) need formatting", len(strings.Fields(out))), "run: gofmt -w .")
		}
		return Pass("formatted")
	})
	add("Code", "go vet", both, false, goTool("go vet ./...", "go", "vet", "./..."))
	add("Code", "staticcheck", both, true, func(ctx context.Context, e *Env) Result {
		name, err := e.tool(ctx, "staticcheck", "honnef.co/go/tools/cmd/staticcheck")
		if err != nil {
			return Caution("could not install staticcheck: "+firstLine(err.Error()), "go install honnef.co/go/tools/cmd/staticcheck@latest")
		}
		out, err := e.run(ctx, name, "./...")
		if err != nil {
			if offline(out) {
				return Caution("could not fetch staticcheck (offline?)", "install it: go install honnef.co/go/tools/cmd/staticcheck@latest")
			}
			return Stop(fmt.Sprintf("%d finding(s): %s", countLines(out), firstLine(out)), "run: staticcheck ./...")
		}
		return Pass("no findings")
	})
	add("Code", "go mod tidy has nothing to change", both, false, func(ctx context.Context, e *Env) Result {
		out, err := e.run(ctx, "go", "mod", "tidy", "-diff")
		if err != nil {
			return Stop("go.mod/go.sum are not tidy", "run: go mod tidy")
		}
		_ = out
		return Pass("tidy")
	})
	add("Code", "tests (with -race unless --quick) and no test touches a live path", both, false, checkTests)
	add("Code", "known vulnerabilities (govulncheck)", both, true, func(ctx context.Context, e *Env) Result {
		name, err := e.tool(ctx, "govulncheck", "golang.org/x/vuln/cmd/govulncheck")
		if err != nil {
			return Caution("could not install govulncheck: "+firstLine(err.Error()), "run: govulncheck ./...")
		}
		out, err := e.run(ctx, name, "./...")
		if err != nil {
			if offline(out) {
				return Caution("could not reach the vulnerability database (offline?)", "run: govulncheck ./...")
			}
			return Stop("a reachable vulnerability was found", "run: govulncheck ./...")
		}
		return Pass("none reachable")
	})
	add("Code", "security scan (gosec, HIGH findings)", both, true, checkGosec)

	// ---- publish ----
	add("Publish", "module path and licence files", []string{TargetPublish}, false, checkRepoFiles)
	add("Publish", "GitHub sign-in and target repository", []string{TargetPublish}, false, checkGitHub)
	add("Publish", "no secrets or personal data in what would be published", []string{TargetPublish}, false, checkSecrets)
	add("Publish", "no data files, credentials or large files", []string{TargetPublish}, false, checkDataFiles)
	add("Publish", "a clean checkout builds and vets", []string{TargetPublish}, false, checkCleanBuild)
	add("Publish", "workflow files look sane", []string{TargetPublish}, false, checkWorkflows)
	add("Publish", "release configuration (goreleaser check)", []string{TargetPublish}, false, func(ctx context.Context, e *Env) Result {
		if _, err := exec.LookPath("goreleaser"); err != nil {
			return Caution("goreleaser is not installed here", "the release workflow checks it when you tag; to check now: go install github.com/goreleaser/goreleaser/v2@latest")
		}
		if out, err := e.run(ctx, "goreleaser", "check"); err != nil {
			return Stop(lastLine(out), "run: goreleaser check")
		}
		return Pass("valid")
	})
	add("Publish", "documentation site builds (mkdocs --strict)", []string{TargetPublish}, true, checkDocs)

	// ---- deploy ----
	add("Deploy", "running as root with the tools deploy needs", []string{TargetDeploy}, false, func(ctx context.Context, e *Env) Result {
		var missing []string
		for _, t := range []string{"docker", "ttyd", "systemctl", "sqlite3"} {
			if _, err := exec.LookPath(t); err != nil {
				missing = append(missing, t)
			}
		}
		switch {
		case len(missing) > 0:
			return Stop("missing: "+strings.Join(missing, ", "), "install them before deploying")
		case os.Geteuid() != 0:
			return Caution("not running as root", "deploy.sh needs root to replace the binary and restart the service")
		}
		return Pass("docker, ttyd, systemctl and sqlite3 found")
	})
	add("Deploy", "enough free disk space", []string{TargetDeploy}, false, func(ctx context.Context, e *Env) Result {
		var problems []string
		for _, dir := range []string{filepath.Dir(e.LiveBinary), e.RollbackDir, filepath.Dir(e.LegoDB)} {
			var st syscall.Statfs_t
			if err := syscall.Statfs(dir, &st); err != nil {
				problems = append(problems, dir+": "+err.Error())
				continue
			}
			if free := st.Bavail * uint64(st.Bsize); free < 1<<30 {
				problems = append(problems, fmt.Sprintf("%s has only %d MB free", dir, free>>20))
			}
		}
		if len(problems) > 0 {
			return Stop(strings.Join(problems, "; "), "free at least 1 GB where the binary, backups and database live")
		}
		return Pass("over 1 GB free")
	})
	add("Deploy", "live LEGO database passes an integrity check (read-only)", []string{TargetDeploy}, false, checkLiveDB)
	add("Deploy", "settings file is private and the deploy values are ready", []string{TargetDeploy}, false, checkSettings)
	add("Deploy", "the new binary builds and runs against scratch data", []string{TargetDeploy}, true, checkBuildSmoke)
	add("Deploy", "a rollback copy of the current binary is possible", []string{TargetDeploy}, false, func(ctx context.Context, e *Env) Result {
		fi, err := os.Stat(e.LiveBinary)
		if err != nil || !fi.Mode().IsRegular() {
			return Stop("the live binary "+e.LiveBinary+" is not there", "")
		}
		if err := os.MkdirAll(e.RollbackDir, 0o700); err != nil {
			return Stop("cannot create "+e.RollbackDir+": "+err.Error(), "")
		}
		probe, err := os.CreateTemp(e.RollbackDir, ".preflight-*")
		if err != nil {
			return Stop("cannot write to "+e.RollbackDir+": "+err.Error(), "")
		}
		probe.Close()
		os.Remove(probe.Name())
		return Pass(fmt.Sprintf("%s (%d MB) can be copied to %s", e.LiveBinary, fi.Size()>>20, e.RollbackDir))
	})

	// ---- scripts ----
	add("Scripts", "deploy and publish scripts parse, and refuse to run without a preflight", both, false, checkScripts)
	return cs
}

// ---- helpers ----

func goTool(label, name string, args ...string) func(context.Context, *Env) Result {
	return func(ctx context.Context, e *Env) Result {
		out, err := e.run(ctx, name, args...)
		if err != nil {
			return Stop(firstLine(out), "run: "+label)
		}
		return Pass("clean")
	}
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func countLines(s string) int {
	n := 0
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			n++
		}
	}
	return n
}

func offline(out string) bool {
	return strings.Contains(out, "dial tcp") || strings.Contains(out, "no such host") || strings.Contains(out, "connection refused") || strings.Contains(out, "i/o timeout")
}

func checkClean(ctx context.Context, e *Env) Result {
	out, err := e.run(ctx, "git", "status", "--porcelain")
	if err != nil {
		return Stop("not a git checkout: "+firstLine(out), "")
	}
	if strings.TrimSpace(out) != "" {
		return Stop(fmt.Sprintf("%d uncommitted change(s), e.g. %s", countLines(out), firstLine(out)), "commit or stash them: what is shipped is what is committed")
	}
	branch, _ := e.run(ctx, "git", "rev-parse", "--abbrev-ref", "HEAD")
	if b := strings.TrimSpace(branch); b != "main" && b != "master" {
		return Caution("on branch "+b, "ship from main")
	}
	return Pass("clean, on " + strings.TrimSpace(branch))
}

func checkTests(ctx context.Context, e *Env) Result {
	// No -count=1: Go's test cache makes an unchanged package free, and the trap
	// paths are fixed (see cmd/wms/preflight.go) so they don't defeat the cache. The
	// race detector runs for a deploy; a publish leaves it to CI, which runs -race
	// on every push.
	args := []string{"test", "./..."}
	label := "passed"
	if !e.Quick && e.Target != TargetPublish {
		args = append(args, "-race")
		label = "passed with -race"
	}
	env := []string{}
	if e.Trap != "" {
		_ = os.MkdirAll(e.Trap, 0o700)
		env = []string{
			"LEGO_DB_PATH=" + filepath.Join(e.Trap, "lego.db"),
			"WMS_SETTINGS_FILE=" + filepath.Join(e.Trap, "settings.json"),
			"AUDIT_LOG_FILE=" + filepath.Join(e.Trap, "audit.log"),
			"TWOFA_FILE=" + filepath.Join(e.Trap, "2fa.json"),
			"WMS_IMAGE_DIR=" + filepath.Join(e.Trap, "img"),
			"WMS_PLUGIN_DIR=" + filepath.Join(e.Trap, "plugins"),
		}
	}
	out, err := e.Run.Run(ctx, e.Repo, env, "go", args...)
	if err != nil {
		// name the failing tests and packages, not the timing lines around them
		var failed []string
		for _, l := range strings.Split(out, "\n") {
			f := strings.Fields(l)
			switch {
			case strings.HasPrefix(l, "--- FAIL:") && len(f) >= 3:
				failed = append(failed, f[2])
			case strings.HasPrefix(l, "FAIL\t") && len(f) >= 2:
				failed = append(failed, f[1])
			case strings.Contains(l, "DATA RACE"):
				failed = append(failed, "a data race")
			case strings.HasPrefix(l, "panic:"):
				failed = append(failed, l)
			}
		}
		if len(failed) == 0 {
			failed = []string{lastLine(out)}
		}
		if len(failed) > 3 {
			failed = append(failed[:3], fmt.Sprintf("and %d more", len(failed)-3))
		}
		return Stop("tests failed: "+strings.Join(failed, ", "), "run: go test ./...")
	}
	if e.Trap != "" {
		if left, _ := os.ReadDir(e.Trap); len(left) > 0 {
			var names []string
			for _, f := range left {
				names = append(names, f.Name())
			}
			return Stop("a test wrote to a default (live) path: "+strings.Join(names, ", "), "give that test its own temp paths (see cmd/wms/main_test.go)")
		}
	}
	return Pass(label)
}

func checkGosec(ctx context.Context, e *Env) Result {
	args := []string{"-fmt=json", "-quiet", "-exclude-generated", "-exclude=G104", "./..."}
	name, err := e.tool(ctx, "gosec", "github.com/securego/gosec/v2/cmd/gosec")
	if err != nil {
		return Caution("could not install gosec: "+firstLine(err.Error()), "run: gosec ./...")
	}
	out, _ := e.run(ctx, name, args...) // gosec exits 1 when it finds anything
	i := strings.Index(out, "{")
	if i < 0 {
		if offline(out) {
			return Caution("could not fetch gosec (offline?)", "run: gosec ./...")
		}
		return Stop("gosec produced no report: "+firstLine(out), "run: gosec ./...")
	}
	var rep struct {
		Issues []struct {
			Severity string `json:"severity"`
			RuleID   string `json:"rule_id"`
			File     string `json:"file"`
		} `json:"Issues"`
	}
	if err := json.NewDecoder(strings.NewReader(out[i:])).Decode(&rep); err != nil {
		return Stop("could not read the gosec report", "run: gosec ./...")
	}
	var high []string
	medium := 0
	for _, is := range rep.Issues {
		file := is.File
		if j := strings.Index(file, e.Repo); j >= 0 {
			file = strings.TrimPrefix(file[j+len(e.Repo):], "/")
		}
		switch {
		case strings.EqualFold(is.Severity, "HIGH") && !acceptedGosec[is.RuleID+" "+file]:
			high = append(high, is.RuleID+" "+file)
		case strings.EqualFold(is.Severity, "MEDIUM"):
			medium++
		}
	}
	if len(high) > 0 {
		return Stop(fmt.Sprintf("%d new HIGH finding(s): %s", len(high), strings.Join(high[:min(len(high), 3)], ", ")), "run: gosec ./...  (fix, or add to the accepted list with a reason)")
	}
	return Pass(fmt.Sprintf("no new HIGH findings (%d MEDIUM reviewed earlier)", medium))
}

// ---- publish ----

func (e *Env) export(ctx context.Context) (string, error) {
	e.exportMu.Lock() // several checks share one export; the first one makes it
	defer e.exportMu.Unlock()
	dir := filepath.Join(e.Scratch, "tree")
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
		return dir, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	tarball := filepath.Join(e.Scratch, "tree.tar")
	if out, err := e.run(ctx, "git", "archive", "HEAD", "--format=tar", "-o", tarball); err != nil {
		return "", fmt.Errorf("git archive: %s", firstLine(out))
	}
	if out, err := e.Run.Run(ctx, dir, nil, "tar", "-xf", tarball); err != nil {
		return "", fmt.Errorf("tar: %s", firstLine(out))
	}
	return dir, nil
}

func checkRepoFiles(ctx context.Context, e *Env) Result {
	b, err := os.ReadFile(filepath.Join(e.Repo, "go.mod"))
	if err != nil {
		return Stop("no go.mod", "")
	}
	if first := firstLine(string(b)); e.Module != "" && first != "module "+e.Module {
		return Stop(first+", expected module "+e.Module, "go install would not work from GitHub")
	}
	for _, f := range []string{"LICENSE", "SECURITY.md", "CONTRIBUTING.md", "README.md", "CHANGELOG.md"} {
		if _, err := os.Stat(filepath.Join(e.Repo, f)); err != nil {
			return Stop(f+" is missing", "")
		}
	}
	cl, _ := os.ReadFile(filepath.Join(e.Repo, "CHANGELOG.md"))
	if !strings.Contains(string(cl), "## [") {
		return Stop("CHANGELOG.md has no release section", "")
	}
	return Pass("module path, LICENSE, SECURITY, CONTRIBUTING, README, CHANGELOG")
}

func checkGitHub(ctx context.Context, e *Env) Result {
	if _, err := exec.LookPath("gh"); err != nil {
		return Stop("the gh command is not installed", "install GitHub's CLI and run: gh auth login")
	}
	if out, err := e.run(ctx, "gh", "auth", "status"); err != nil {
		return Stop("not signed in to GitHub: "+firstLine(out), "run: gh auth login")
	}
	if e.Repository == "" {
		return Pass("signed in")
	}
	if _, err := e.run(ctx, "gh", "repo", "view", e.Repository, "--json", "name"); err == nil {
		return Pass("signed in; " + e.Repository + " exists (wms publish adds a commit to it)")
	}
	return Pass("signed in; " + e.Repository + " is free")
}

func checkSecrets(ctx context.Context, e *Env) Result {
	dir, err := e.export(ctx)
	if err != nil {
		return Stop(err.Error(), "")
	}
	pii, bad := LoadPII(e.PIIPatterns)
	secrets := LoadSecrets(e.Settings, e.TwoFA, e.DeployValues)
	finds, err := ScanTree(dir, pii, secrets)
	if err != nil {
		return Stop("scan failed: "+err.Error(), "")
	}
	if len(finds) > 0 {
		var where []string
		for _, f := range finds[:min(len(finds), 3)] {
			where = append(where, f.String())
		}
		return Stop(fmt.Sprintf("%d hit(s): %s", len(finds), strings.Join(where, "; ")), "remove it, and rewrite history if it was ever committed (the values are not shown here)")
	}
	note := fmt.Sprintf("clean (%d secret value(s) and %d personal pattern(s) checked)", len(secrets), len(pii))
	switch {
	case len(bad) > 0:
		return Caution(fmt.Sprintf("%d personal pattern(s) in %s are not valid regular expressions", len(bad), e.PIIPatterns), "fix the file")
	case len(pii) == 0:
		return Caution("clean, but no personal-data patterns are configured", "list your domain, e-mail and IP in "+e.PIIPatterns+" (one regular expression per line)")
	case len(secrets) == 0:
		return Caution("clean, but no live secret values could be read to check against", "")
	}
	return Pass(note)
}

func checkDataFiles(ctx context.Context, e *Env) Result {
	dir, err := e.export(ctx)
	if err != nil {
		return Stop(err.Error(), "")
	}
	data, large := DataFiles(dir)
	switch {
	case len(data) > 0:
		return Stop("data or credential files: "+strings.Join(data[:min(len(data), 3)], ", "), "remove them and add them to .gitignore")
	case len(large) > 0:
		return Stop("files over 1 MB: "+strings.Join(large[:min(len(large), 3)], ", "), "source repositories should not carry them")
	}
	return Pass("none")
}

func checkCleanBuild(ctx context.Context, e *Env) Result {
	dir, err := e.export(ctx)
	if err != nil {
		return Stop(err.Error(), "")
	}
	if out, err := e.Run.Run(ctx, dir, nil, "go", "build", "-o", os.DevNull, "./cmd/wms"); err != nil {
		return Stop("build failed: "+firstLine(out), "a fresh clone would not build")
	}
	if out, err := e.Run.Run(ctx, dir, nil, "go", "vet", "./..."); err != nil {
		return Stop("vet failed: "+firstLine(out), "")
	}
	return Pass("go build and go vet on the committed files only")
}

func checkWorkflows(ctx context.Context, e *Env) Result {
	files, _ := filepath.Glob(filepath.Join(e.Repo, ".github", "workflows", "*.yml"))
	if len(files) == 0 {
		return Caution("no workflow files", "there will be no CI or release build")
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return Stop(err.Error(), "")
		}
		s := string(b)
		switch {
		case !strings.Contains(s, "\non:") && !strings.HasPrefix(s, "on:"):
			return Stop(filepath.Base(f)+" has no trigger (on:)", "")
		case !strings.Contains(s, "jobs:"):
			return Stop(filepath.Base(f)+" has no jobs", "")
		case strings.Contains(s, "\n\t"):
			return Stop(filepath.Base(f)+" is indented with tabs (YAML forbids them)", "")
		}
	}
	return Pass(fmt.Sprintf("%d workflow file(s)", len(files)))
}

func checkDocs(ctx context.Context, e *Env) Result {
	mk := os.Getenv("WMS_MKDOCS")
	if mk == "" {
		p, err := exec.LookPath("mkdocs")
		if err != nil {
			return Caution("mkdocs is not installed here", "set WMS_MKDOCS to its path (the docs workflow builds it on GitHub anyway)")
		}
		mk = p
	}
	dir, err := e.export(ctx)
	if err != nil {
		return Stop(err.Error(), "")
	}
	steps := [][]string{
		{"go", "run", "./cmd/wms", "docs-gen", "docs/reference/cli"},
		{"go", "test", "./internal/uiapp", "-count=1", "-run", "TestDocScreensRender"},
	}
	for i, s := range steps {
		env := []string{}
		if i == 1 {
			env = []string{"WMS_DOCS_OUT=" + filepath.Join(dir, "docs", "assets", "screens")}
		}
		if out, err := e.Run.Run(ctx, dir, env, s[0], s[1:]...); err != nil {
			return Stop("generating the docs failed: "+lastLine(out), "run: make docs-generate")
		}
	}
	if out, err := e.Run.Run(ctx, dir, nil, mk, "build", "--strict", "-q"); err != nil {
		return Stop("mkdocs --strict failed: "+lastLine(out), "run: make docs-build")
	}
	return Pass("built with --strict")
}

// ---- deploy ----

func checkLiveDB(ctx context.Context, e *Env) Result {
	if _, err := os.Stat(e.LegoDB); err != nil {
		return Caution("no LEGO database yet ("+e.LegoDB+")", "it is created on first use")
	}
	out, err := quickCheck(ctx, e.LegoDB)
	if err != nil {
		return Stop("could not check "+e.LegoDB+": "+err.Error(), "")
	}
	if out != "ok" {
		return Stop("integrity check says: "+out, "restore the newest backup before deploying")
	}
	return Pass("ok")
}

func checkSettings(ctx context.Context, e *Env) Result {
	if fi, err := os.Stat(e.Settings); err == nil && fi.Mode().Perm()&0o077 != 0 {
		return Stop(fmt.Sprintf("%s is mode %o", e.Settings, fi.Mode().Perm()), "run: chmod 600 "+e.Settings)
	}
	b, err := os.ReadFile(e.DeployValues)
	if err != nil {
		return Stop("the deploy values file "+e.DeployValues+" is missing", "it holds PARTDB_URL, MODERNWMS_URL, SYNC_VIEWONLY_AUTH and SYNC_VIEWONLY_EMAIL (mode 600)")
	}
	if fi, err := os.Stat(e.DeployValues); err == nil && fi.Mode().Perm()&0o077 != 0 {
		return Stop(fmt.Sprintf("%s is mode %o", e.DeployValues, fi.Mode().Perm()), "run: chmod 600 "+e.DeployValues)
	}
	var missing []string
	for _, k := range []string{"PARTDB_URL", "MODERNWMS_URL", "SYNC_VIEWONLY_AUTH", "SYNC_VIEWONLY_EMAIL"} {
		found := false
		for _, l := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(strings.TrimSpace(l), k+"=") && len(strings.TrimSpace(l)) > len(k)+1 {
				found = true
			}
		}
		if !found {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return Stop("missing from the deploy values: "+strings.Join(missing, ", "), "without them the sync and the public links would change behaviour")
	}
	return Pass("settings are private; all four deploy values present")
}

func checkBuildSmoke(ctx context.Context, e *Env) Result {
	bin := filepath.Join(e.Scratch, "wms-new")
	if out, err := e.run(ctx, "go", "build", "-trimpath", "-o", bin, "./cmd/wms"); err != nil {
		return Stop("build failed: "+firstLine(out), "")
	}
	scratch := filepath.Join(e.Scratch, "smoke")
	_ = os.MkdirAll(scratch, 0o700)
	env := []string{
		"LEGO_DB_PATH=" + filepath.Join(scratch, "lego.db"),
		"WMS_SETTINGS_FILE=" + filepath.Join(scratch, "settings.json"),
		"AUDIT_LOG_FILE=" + filepath.Join(scratch, "audit.log"),
		"TWOFA_FILE=" + filepath.Join(scratch, "2fa.json"),
		"WMS_PLUGIN_DIR=" + filepath.Join(scratch, "plugins"),
		"WMS_IMAGE_DIR=" + filepath.Join(scratch, "img"),
		"MODERNWMS_BACKUP_DIR=" + filepath.Join(scratch, "backups"),
		"PARTDB_DB_PATH=" + filepath.Join(scratch, "partdb.db"),
		"MODERNWMS_CONTAINER=no-such-container",
		"PARTDB_API_URL=http://127.0.0.1:1",
		"REBRICKABLE_BASE_URL=http://127.0.0.1:1",
	}
	for _, args := range [][]string{{"version"}, {"lego", "stats"}, {"plugin", "list"}, {"lego", "search", "parts", "brick"}} {
		out, err := e.Run.Run(ctx, e.Repo, env, bin, args...)
		if err != nil && !(len(args) > 2 && args[1] == "search") { // an empty catalog answers "no match" with a non-zero exit
			return Stop("`wms "+strings.Join(args, " ")+"` failed: "+firstLine(out), "")
		}
	}
	return Pass("builds; version, stats, plugin list and search run on scratch data")
}

func checkScripts(ctx context.Context, e *Env) Result {
	if len(e.Scripts) == 0 {
		return Caution("no scripts configured", "")
	}
	for _, s := range e.Scripts {
		b, err := os.ReadFile(s)
		if err != nil {
			return Stop(filepath.Base(s)+" is missing", "")
		}
		if out, err := e.run(ctx, "bash", "-n", s); err != nil {
			return Stop(filepath.Base(s)+": "+firstLine(out), "")
		}
		if !strings.Contains(string(b), "preflight") {
			return Stop(filepath.Base(s)+" does not require a preflight", "it must call `wms preflight --require`")
		}
	}
	if _, err := exec.LookPath("shellcheck"); err == nil {
		for _, s := range e.Scripts {
			if out, err := e.run(ctx, "shellcheck", "-S", "warning", s); err != nil {
				return Caution(filepath.Base(s)+": "+firstLine(out), "run: shellcheck "+s)
			}
		}
		return Pass(fmt.Sprintf("%d script(s): syntax, shellcheck and the preflight gate", len(e.Scripts)))
	}
	return Pass(fmt.Sprintf("%d script(s): syntax and the preflight gate (shellcheck is not installed)", len(e.Scripts)))
}

// tool finds an analysis tool: on $PATH, else in the tool cache, installing it
// there (go install …@latest) when missing or older than a week. Installing once
// instead of `go run …@latest` on every preflight is most of what made it slow.
func (e *Env) tool(ctx context.Context, name, pkg string) (string, error) {
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	dir := e.ToolDir
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".cache", "wms-preflight", "bin")
	}
	bin := filepath.Join(dir, name)
	if fi, err := os.Stat(bin); err == nil && time.Since(fi.ModTime()) < 7*24*time.Hour {
		return bin, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if out, err := e.Run.Run(ctx, e.Repo, []string{"GOBIN=" + dir}, "go", "install", pkg+"@latest"); err != nil {
		if _, serr := os.Stat(bin); serr == nil {
			return bin, nil // offline: an older copy will do
		}
		return "", fmt.Errorf("%s", firstLine(out))
	}
	now := time.Now()
	_ = os.Chtimes(bin, now, now)
	return bin, nil
}
