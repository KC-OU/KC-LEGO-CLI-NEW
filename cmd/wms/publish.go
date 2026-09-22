package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

// `wms publish`: push what is committed here to the public repository as one new
// commit on top of its main branch. Your private history never leaves this
// machine: a persistent clone of the public repo (kept in ~/.cache, fetched rather
// than re-cloned) receives an export of HEAD, and only the difference is pushed.
// Nothing happens without a GO from `wms preflight publish` for this exact commit
// (it runs the preflight itself when there is no fresh verdict).

func newPublishCmd() *cobra.Command {
	var repo, message, tag string
	var wait, dryRun bool
	cmd := &cobra.Command{
		Use:   "publish",
		Short: "Push the committed code to GitHub as one new commit (after the preflight says GO)",
		Long: "Exports HEAD into a cached clone of the public repository, commits the difference on top of its main\n" +
			"branch with your GitHub noreply identity, and pushes. The docs are built and hosted by the repository's\n" +
			"own GitHub Pages workflow. --tag also tags a release; --wait waits for CI and the docs to finish.",
		Example: "  wms publish -m \"Set checks, orders and labels\" --wait\n  wms publish --tag v1.2.0",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			t := ui.New()
			step := func(s string) { say(t.Brand.Render("== ") + t.Strong.Render(s)) }
			ctx := context.Background()
			cwd, _ := os.Getwd()
			src, err := gitTop(cwd)
			if err != nil {
				return withCode(exitUsage, err)
			}
			if _, err := exec.LookPath("gh"); err != nil {
				return usageError("install GitHub's CLI (gh) and run: gh auth login")
			}
			self, _ := os.Executable()

			step("1/5 preflight")
			if out, err := runIn(ctx, src, self, "preflight", "--require", "publish"); err != nil {
				say(strings.TrimSpace(out))
				c := exec.CommandContext(ctx, self, "preflight", "publish", "--plain")
				c.Dir, c.Stdout, c.Stderr = src, os.Stdout, os.Stderr
				if err := c.Run(); err != nil {
					return withCode(exitFailure, errors.New("NO-GO: fix what the preflight lists, then run this again"))
				}
			} else {
				say(t.Muted.Render("   " + strings.TrimSpace(out)))
			}

			step("2/5 the public repository")
			home, _ := os.UserHomeDir()
			dir := filepath.Join(home, ".cache", "wms-publish", strings.ReplaceAll(repo, "/", "_"))
			if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
				_ = os.MkdirAll(filepath.Dir(dir), 0o700)
				if out, err := runIn(ctx, "", "gh", "repo", "clone", repo, dir); err != nil {
					return fmt.Errorf("cloning %s: %s", repo, lastLineOf(out))
				}
			}
			for _, a := range [][]string{{"fetch", "-q", "origin", "main"}, {"checkout", "-q", "-B", "main", "origin/main"}, {"reset", "-q", "--hard", "origin/main"}, {"clean", "-qfdx"}} {
				if out, err := runIn(ctx, dir, "git", a...); err != nil {
					return fmt.Errorf("git %s: %s", a[0], lastLineOf(out))
				}
			}

			step("3/5 this checkout's HEAD over it")
			entries, _ := os.ReadDir(dir)
			for _, e := range entries {
				if e.Name() != ".git" {
					_ = os.RemoveAll(filepath.Join(dir, e.Name()))
				}
			}
			archive := exec.CommandContext(ctx, "git", "archive", "HEAD")
			archive.Dir = src
			untar := exec.CommandContext(ctx, "tar", "-x", "-C", dir)
			pipe, _ := archive.StdoutPipe()
			untar.Stdin = pipe
			if err := untar.Start(); err != nil {
				return err
			}
			if err := archive.Run(); err != nil {
				return fmt.Errorf("git archive: %w", err)
			}
			if err := untar.Wait(); err != nil {
				return fmt.Errorf("tar: %w", err)
			}
			if out, err := runIn(ctx, dir, "git", "add", "-A"); err != nil {
				return errors.New(lastLineOf(out))
			}
			stat, _ := runIn(ctx, dir, "git", "diff", "--cached", "--shortstat")
			if strings.TrimSpace(stat) == "" {
				say(ui.Status(t, true, "Nothing to publish: the public repository already matches HEAD."))
				return emit(map[string]any{"published": false})
			}
			say(t.Muted.Render("   " + strings.TrimSpace(stat)))

			step("4/5 commit and push")
			name, _ := runIn(ctx, "", "gh", "api", "user", "--jq", ".login")
			mail, _ := runIn(ctx, "", "gh", "api", "user", "--jq", `"\(.id)+\(.login)@users.noreply.github.com"`)
			name, mail = strings.TrimSpace(name), strings.TrimSpace(mail)
			if message == "" {
				subject, _ := runIn(ctx, src, "git", "log", "-1", "--format=%s")
				message = strings.TrimSpace(subject)
			}
			if dryRun {
				say(ui.Status(t, true, "--dry-run: would commit \""+message+"\" as "+name+" and push to "+repo))
				return emit(map[string]any{"published": false, "dry_run": true})
			}
			c := exec.CommandContext(ctx, "git", "-c", "user.name="+name, "-c", "user.email="+mail, "commit", "-q", "-F", "-")
			c.Dir, c.Stdin = dir, strings.NewReader(message+"\n")
			if out, err := c.CombinedOutput(); err != nil {
				return fmt.Errorf("commit: %s", lastLineOf(string(out)))
			}
			if out, err := runIn(ctx, dir, "git", "push", "-q", "origin", "HEAD:main"); err != nil {
				return fmt.Errorf("push: %s", lastLineOf(out))
			}
			sha, _ := runIn(ctx, dir, "git", "rev-parse", "--short", "HEAD")
			say(ui.Status(t, true, fmt.Sprintf("Pushed %s to %s", strings.TrimSpace(sha), repo)))
			if tag != "" {
				if out, err := runIn(ctx, dir, "git", "-c", "user.name="+name, "-c", "user.email="+mail, "tag", "-a", tag, "-m", "wms-go "+strings.TrimPrefix(tag, "v")); err != nil {
					return fmt.Errorf("tag: %s", lastLineOf(out))
				}
				if out, err := runIn(ctx, dir, "git", "push", "-q", "origin", tag); err != nil {
					return fmt.Errorf("push tag: %s", lastLineOf(out))
				}
				say(ui.Status(t, true, "Tagged "+tag+" (the release workflow builds it)"))
			}

			step("5/5 CI and docs")
			owner, rname, _ := strings.Cut(repo, "/")
			site := fmt.Sprintf("https://%s.github.io/%s/", strings.ToLower(owner), rname)
			if wait {
				time.Sleep(8 * time.Second)
				for _, wf := range []string{"CI", "Docs"} {
					id, _ := runIn(ctx, "", "gh", "run", "list", "-R", repo, "--branch", "main", "--workflow", wf, "--limit", "1", "--json", "databaseId", "-q", ".[0].databaseId")
					if id = strings.TrimSpace(id); id == "" {
						continue
					}
					w := exec.CommandContext(ctx, "gh", "run", "watch", "-R", repo, id, "--exit-status")
					w.Stdout, w.Stderr = os.Stdout, os.Stderr
					if err := w.Run(); err != nil {
						say(ui.Warn(t, wf+" failed: gh run view -R "+repo+" "+id+" --log-failed"))
					} else {
						say(ui.Status(t, true, wf+" passed"))
					}
				}
			}
			say(ui.Fact(t, "Repository", "https://github.com/"+repo))
			say(ui.Fact(t, "Docs", site+" (GitHub Pages)"))
			return emit(map[string]any{"published": true, "commit": strings.TrimSpace(sha), "repo": repo, "docs": site})
		},
	}
	cmd.Flags().StringVar(&repo, "repo", envOr("WMS_PUBLISH_REPO", "KC-OU/KC-LEGO-CLI-NEW"), "owner/name on GitHub")
	cmd.Flags().StringVarP(&message, "message", "m", "", "commit message (default: HEAD's subject)")
	cmd.Flags().StringVar(&tag, "tag", "", "also tag a release, e.g. v1.2.0")
	cmd.Flags().BoolVar(&wait, "wait", false, "wait for the CI and Docs workflows")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "prepare everything but do not commit or push")
	return cmd
}

func runIn(ctx context.Context, dir, name string, args ...string) (string, error) {
	c := exec.CommandContext(ctx, name, args...)
	c.Dir = dir
	out, err := c.CombinedOutput()
	return string(out), err
}

func lastLineOf(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}
