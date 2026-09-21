package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/preflight"
)

// gitTop is the checkout containing dir.
func gitTop(dir string) (string, error) {
	out, err := preflight.ExecRunner{}.Run(context.Background(), dir, nil, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("%s is not inside a git checkout", dir)
	}
	return strings.TrimSpace(out), nil
}

func gitHead(repo string) string {
	out, _ := preflight.ExecRunner{}.Run(context.Background(), repo, nil, "git", "rev-parse", "HEAD")
	return strings.TrimSpace(out)
}

// doctorAsCheck lets the audit reuse a doctor check unchanged. Backups are a warning here, not a
// stop: the deploy script takes its own backup of everything it touches.
func doctorAsCheck(group, name string, targets []string, softFail bool, f func() check) preflight.Check {
	return preflight.Check{Group: group, Name: name, Targets: targets, Run: func(ctx context.Context, e *preflight.Env) preflight.Result {
		c := f()
		switch c.Status {
		case stOK:
			return preflight.Pass(c.Detail)
		case stWarn:
			return preflight.Caution(c.Detail, c.Hint)
		}
		if softFail {
			return preflight.Caution(c.Detail, c.Hint)
		}
		return preflight.Stop(c.Detail, c.Hint)
	}}
}

func newPreflightCmd() *cobra.Command {
	var quick, plain, require, allowQuick bool
	var repoFlag, values string
	var maxAge time.Duration
	cmd := &cobra.Command{
		Use:   "preflight [deploy|publish|all]",
		Short: "Audit the code, the files to be published and the live machine, and say GO or NO-GO",
		Long: "Runs every check that matters before shipping and ends in a GO or NO-GO. It changes nothing (it reads the\n" +
			"live files and builds in a scratch folder), and it saves the verdict so deploy.sh and publish.sh can refuse to\n" +
			"run without a fresh GO for the same commit.\n\n" +
			"  deploy    the code, the live machine, the new binary on scratch data, the ship scripts\n" +
			"  publish   the code, secrets and personal data in what would be published, a clean build, the docs, GitHub\n" +
			"  all       both (the default)\n\n" +
			"--quick skips the slow checks (race detector, staticcheck, govulncheck, gosec, docs); a quick GO is not enough\n" +
			"for the ship scripts. --require does not run anything: it only checks that a fresh full GO is on file.",
		Example:   "  wms preflight deploy\n  wms preflight all --quick\n  wms preflight --require publish",
		Args:      cobra.MaximumNArgs(1),
		ValidArgs: []string{"deploy", "publish", "all"},
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			target := preflight.TargetAll
			if len(args) == 1 {
				target = args[0]
			}
			if target != preflight.TargetAll && target != preflight.TargetDeploy && target != preflight.TargetPublish {
				return usageError("the target must be deploy, publish or all, not %q", target)
			}
			loadToolEnv()
			cwd, _ := os.Getwd()
			if repoFlag != "" {
				cwd = repoFlag
			}
			repo, err := gitTop(cwd)
			if err != nil {
				return withCode(exitUsage, err)
			}
			home, _ := os.UserHomeDir()
			vpath := preflight.VerdictPath(home)
			head := gitHead(repo)

			if require {
				v, why := preflight.Fresh(vpath, target, head, livePaths(), maxAge, time.Now(), allowQuick)
				if why != "" {
					say(why)
					return withCode(exitFailure, fmt.Errorf("no fresh GO for %s", target))
				}
				say(fmt.Sprintf("preflight GO for %s at %s (%s ago)", v.Target, head[:min(len(head), 8)], time.Since(v.At).Round(time.Second)))
				return emit(map[string]any{"go": true, "verdict": v})
			}

			scratch, err := os.MkdirTemp("", "wms-preflight-")
			if err != nil {
				return err
			}
			defer os.RemoveAll(scratch)
			e := &preflight.Env{
				Repo: repo, Run: preflight.ExecRunner{}, Now: time.Now, Quick: quick, Target: target,
				LegoDB: config.Get(config.LegoDBPath), Settings: config.Get(config.SettingsFile), TwoFA: config.Get(config.TwoFAFile),
				BackupDir:    config.Get(config.ModernWMSBackupDir),
				DeployValues: envOr("WMS_DEPLOY_VALUES", values),
				PIIPatterns:  envOr("WMS_PII_PATTERNS", filepath.Join(home, ".config", "wms-go", "pii-patterns.txt")),
				LiveBinary:   envOr("WMS_LIVE_BINARY", "/usr/local/bin/wms-go"),
				RollbackDir:  envOr("WMS_ROLLBACK_DIR", "/root/backups"),
				Repository:   envOr("WMS_PUBLISH_REPO", "KC-OU/KC-LEGO-CLI-NEW"),
				Module:       envOr("WMS_MODULE", "github.com/KC-OU/KC-LEGO-CLI-NEW"),
				Scratch:      scratch,
				Trap:         filepath.Join(scratch, "trap"),
			}
			for _, s := range []string{"deploy.sh", "publish.sh"} {
				if p := filepath.Join(repo, "scripts", s); fileExists(p) {
					e.Scripts = append(e.Scripts, p)
				}
			}
			all := preflight.Standard()
			all = append(all,
				doctorAsCheck("Deploy", "ModernWMS backup is recent", []string{preflight.TargetDeploy}, true, func() check { return checkBackups(e.BackupDir, time.Now()) }),
				doctorAsCheck("Deploy", "LEGO collection has a recent backup", []string{preflight.TargetDeploy}, true, func() check { return checkLegoBackup(time.Now()) }),
			)
			checks := preflight.Select(all, target, quick)

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			width := 80
			isTTY := term.IsTerminal(int(os.Stdout.Fd()))
			if isTTY {
				if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
					width = w
				}
			}
			header := fmt.Sprintf("%s · %s", target, head[:min(len(head), 8)])
			if quick {
				header += " · quick"
			}
			var outs []preflight.Outcome
			animated := isTTY && !plain && !out.JSON && !out.Quiet && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
			switch {
			case animated:
				outs, err = preflight.RunAnimated(ctx, e, checks, header)
				if err != nil {
					return err
				}
			case out.JSON || out.Quiet:
				outs = preflight.Execute(ctx, e, checks, nil)
			default:
				fmt.Printf(" wms preflight · %s\n\n", header)
				outs = preflight.Execute(ctx, e, checks, preflight.PlainProgress(os.Stdout, width))
			}
			v := preflight.Summarise(target, head, quick, time.Now(), outs)
			v.Paths = livePaths()
			if len(outs) < len(checks) {
				v.Go = false // stopped early
			}
			if err := preflight.SaveVerdict(vpath, v); err != nil {
				say(fmt.Sprintf("could not save the verdict: %v", err))
			}
			if !out.JSON && !out.Quiet {
				preflight.Report(os.Stdout, outs, v, width)
			}
			if err := emit(map[string]any{"verdict": v, "checks": jsonOutcomes(outs)}); err != nil {
				return err
			}
			if !v.Go {
				return withCode(exitFailure, fmt.Errorf("NO-GO: %d failure(s)", v.Failures))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&quick, "quick", false, "skip the slow checks (a quick GO does not satisfy the ship scripts)")
	cmd.Flags().BoolVar(&plain, "plain", false, "no animation: one line per check")
	cmd.Flags().BoolVar(&require, "require", false, "run nothing: succeed only if a fresh full GO for this commit is on file")
	cmd.Flags().BoolVar(&allowQuick, "allow-quick", false, "with --require, accept a --quick GO")
	cmd.Flags().DurationVar(&maxAge, "max-age", 15*time.Minute, "with --require, how old a GO may be")
	cmd.Flags().StringVar(&repoFlag, "repo", "", "the checkout to audit (default: the one you are in)")
	home, _ := os.UserHomeDir()
	cmd.Flags().StringVar(&values, "values", filepath.Join(home, ".config", "wms-go", "deploy.env"), "the private deploy values file (KEY=value lines, mode 600)")
	return cmd
}

func jsonOutcomes(outs []preflight.Outcome) []map[string]any {
	res := make([]map[string]any, len(outs))
	for i, o := range outs {
		res[i] = map[string]any{"group": o.Check.Group, "check": o.Check.Name, "status": o.Result.Status, "detail": o.Result.Detail, "hint": o.Result.Hint, "seconds": o.Duration.Seconds()}
	}
	return res
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular()
}

// loadToolEnv reads ~/.config/wms-go/tools.env (KEY=value lines) for settings the audit needs that are
// specific to this machine, such as WMS_MKDOCS. Values already in the environment win.
func loadToolEnv() {
	home, _ := os.UserHomeDir()
	b, err := os.ReadFile(filepath.Join(home, ".config", "wms-go", "tools.env"))
	if err != nil {
		return
	}
	for _, l := range strings.Split(string(b), "\n") {
		l = strings.TrimSpace(l)
		if k, v, ok := strings.Cut(l, "="); ok && !strings.HasPrefix(l, "#") && os.Getenv(strings.TrimSpace(k)) == "" {
			os.Setenv(strings.TrimSpace(k), strings.Trim(strings.TrimSpace(v), `"'`))
		}
	}
}

// livePaths identifies the machine a verdict was earned on: the database and settings it looked at.
func livePaths() string {
	return config.Get(config.LegoDBPath) + "|" + config.Get(config.SettingsFile)
}
