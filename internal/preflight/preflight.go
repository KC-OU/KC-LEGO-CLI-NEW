// Package preflight is the "is it safe to ship?" audit: a list of checks about the code, the files that
// would be published and the live machine that would be deployed to, run one after another, ending in
// a GO or NO-GO verdict. It never changes anything except its own scratch files and the verdict file.
package preflight

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type Status string

const (
	OK   Status = "ok"
	Warn Status = "warn"
	Fail Status = "fail"
	Skip Status = "skip"
)

// Result is what one check found. Detail is one line; Hint says what to do about a Warn or Fail.
type Result struct {
	Status Status `json:"status"`
	Detail string `json:"detail"`
	Hint   string `json:"hint,omitempty"`
}

func Pass(detail string) Result          { return Result{Status: OK, Detail: detail} }
func Caution(detail, hint string) Result { return Result{Status: Warn, Detail: detail, Hint: hint} }
func Stop(detail, hint string) Result    { return Result{Status: Fail, Detail: detail, Hint: hint} }
func Skipped(why string) Result          { return Result{Status: Skip, Detail: why} }

const (
	TargetDeploy  = "deploy"
	TargetPublish = "publish"
	TargetAll     = "all"
)

// Check is one thing to verify. Targets lists which of "deploy" and "publish" it matters for; Slow
// checks (the race tests, the docs build) are left out of a --quick run.
type Check struct {
	Group   string
	Name    string
	Targets []string
	Slow    bool
	Run     func(ctx context.Context, e *Env) Result
}

// Runner runs an external program; tests replace it with a fake.
type Runner interface {
	Run(ctx context.Context, dir string, env []string, name string, args ...string) (string, error)
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, dir string, env []string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	return buf.String(), err
}

// Env is everything a check may look at.
type Env struct {
	Repo   string // the git checkout being audited
	Run    Runner
	Now    func() time.Time
	Quick  bool
	Target string

	LegoDB       string // live paths, only ever read
	Settings     string
	TwoFA        string
	BackupDir    string
	DeployValues string // KEY=value file with the private values deploy needs (outside the repo)
	PIIPatterns  string // one regular expression per line, private to this machine
	LiveBinary   string
	RollbackDir  string
	Repository   string // owner/name on GitHub
	Module       string // expected Go module path
	Scripts      []string
	Scratch      string // a private temp folder for the run

	// Trap is a folder handed to the tests as every writable path; it must still be empty afterwards.
	Trap string
}

func (e *Env) run(ctx context.Context, name string, args ...string) (string, error) {
	return e.Run.Run(ctx, e.Repo, nil, name, args...)
}

// Outcome is one finished check.
type Outcome struct {
	Check    Check
	Result   Result
	Duration time.Duration
}

// Event reports progress to a view: Start when a check begins, Done when it ends.
type Event struct {
	Index   int
	Total   int
	Check   Check
	Done    bool
	Outcome Outcome
}

// Select returns the checks that apply to a target, in order.
func Select(all []Check, target string, quick bool) []Check {
	var out []Check
	for _, c := range all {
		if quick && c.Slow {
			continue
		}
		if target == TargetAll || contains(c.Targets, target) {
			out = append(out, c)
		}
	}
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// Execute runs the checks in order. A check that panics is a failure, not a crash. When ctx ends the
// remaining checks are skipped.
func Execute(ctx context.Context, e *Env, checks []Check, on func(Event)) []Outcome {
	out := make([]Outcome, 0, len(checks))
	for i, c := range checks {
		if on != nil {
			on(Event{Index: i, Total: len(checks), Check: c})
		}
		start := time.Now()
		var r Result
		if ctx.Err() != nil {
			r = Skipped("interrupted")
		} else {
			r = safeRun(ctx, e, c)
		}
		o := Outcome{Check: c, Result: r, Duration: time.Since(start)}
		out = append(out, o)
		if on != nil {
			on(Event{Index: i, Total: len(checks), Check: c, Done: true, Outcome: o})
		}
	}
	return out
}

func safeRun(ctx context.Context, e *Env, c Check) (r Result) {
	defer func() {
		if p := recover(); p != nil {
			r = Stop("the check crashed", "this is a bug in the audit itself")
		}
	}()
	return c.Run(ctx, e)
}

// Verdict summarises a run.
type Verdict struct {
	Target   string    `json:"target"`
	Head     string    `json:"head"`
	At       time.Time `json:"at"`
	Quick    bool      `json:"quick"`
	Go       bool      `json:"go"`
	Passed   int       `json:"passed"`
	Warnings int       `json:"warnings"`
	Failures int       `json:"failures"`
	Skipped  int       `json:"skipped"`
	// Paths names the live files the run looked at (database and settings). A GO earned against scratch
	// data must not stand in for a GO against the real machine.
	Paths string `json:"paths,omitempty"`
}

func Summarise(target, head string, quick bool, at time.Time, outs []Outcome) Verdict {
	v := Verdict{Target: target, Head: head, At: at, Quick: quick}
	for _, o := range outs {
		switch o.Result.Status {
		case OK:
			v.Passed++
		case Warn:
			v.Warnings++
		case Fail:
			v.Failures++
		case Skip:
			v.Skipped++
		}
	}
	v.Go = v.Failures == 0
	return v
}

// VerdictPath is where the last verdict is kept.
func VerdictPath(home string) string { return filepath.Join(home, ".cache", "wms-preflight.json") }

func SaveVerdict(path string, v Verdict) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Fresh reports whether a saved verdict lets a deploy or publish script go ahead: a GO for the same
// target (or "all") and the same commit, from a full (not --quick) run, no older than maxAge.
func Fresh(path, target, head, paths string, maxAge time.Duration, now time.Time, allowQuick bool) (Verdict, string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Verdict{}, "no preflight has been run yet: run `wms preflight " + target + "`"
	}
	var v Verdict
	if json.Unmarshal(b, &v) != nil {
		return v, "the saved verdict is unreadable: run `wms preflight " + target + "` again"
	}
	switch {
	case !v.Go:
		return v, "the last preflight was a NO-GO: fix it and run `wms preflight " + target + "` again"
	case v.Target != target && v.Target != TargetAll:
		return v, "the last preflight was for " + v.Target + ", not " + target
	case v.Paths != paths:
		return v, "the last preflight looked at different files (" + v.Paths + "), not this machine's (" + paths + ")"
	case v.Head != head:
		return v, "the code has changed since the last preflight (" + short(v.Head) + " then, " + short(head) + " now)"
	case v.Quick && !allowQuick:
		return v, "the last preflight was --quick: run a full one"
	case now.Sub(v.At) > maxAge:
		return v, "the last preflight is " + now.Sub(v.At).Round(time.Minute).String() + " old (limit " + maxAge.String() + ")"
	}
	return v, ""
}

func short(h string) string {
	if len(h) > 8 {
		return h[:8]
	}
	return h
}
