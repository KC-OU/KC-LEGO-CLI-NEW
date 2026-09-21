package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/audit"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/tools"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

func repoDir() string                  { return envOr("WMS_REPO", "/root/modernwms-partdb-go") }
func selfPath() string                 { p, _ := os.Executable(); return p }
func stateFile() string                { return tools.StatePath() }
func toolsFile() string                { return tools.FilePath() }
func loadTools() ([]tools.Tool, error) { return tools.Load() }
func operatorName() string {
	if u := os.Getenv("WMS_USER"); u != "" {
		return "cli:" + u
	}
	if u, err := user.Current(); err == nil {
		return "cli:" + u.Username
	}
	return "cli"
}

// runTool runs one tool with the terminal attached, audits it, and remembers it as recent.
func runTool(t tools.Tool, answers []string) (int, error) {
	argv, err := tools.Expand(t, selfPath(), repoDir(), answers)
	if err != nil {
		return exitUsage, err
	}
	c := exec.Command(argv[0], argv[1:]...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	runErr := c.Run()
	code := 0
	if runErr != nil {
		code = exitFailure
		if ee, ok := runErr.(*exec.ExitError); ok {
			code = ee.ExitCode()
		}
	}
	status := "SUCCESS"
	if code != 0 {
		status = "FAILED"
	}
	_ = audit.New().Log(operatorName(), "", "TOOL_RUN", status, fmt.Sprintf("tool=%s exit=%d", t.ID, code))
	st := tools.LoadState(stateFile())
	st.Record(t.ID, time.Now())
	_ = st.Save(stateFile())
	if runErr != nil {
		if _, ok := runErr.(*exec.ExitError); !ok {
			return code, runErr
		}
	}
	return code, nil
}

func statusLine() string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return bannerLine(collectStatus(ctx, false), ui.New())
}

func newMenuCmd() *cobra.Command {
	var loop bool
	cmd := &cobra.Command{
		Use:   "menu",
		Short: "Quick launcher: pick a tool from a short list and run it (alias: q)",
		Long: "A compact list of everything you do from the shell: status, logs, restarts, backups, users, LEGO, and shipping.\n" +
			"Type to filter, Enter runs, Ctrl-P pins a favourite (favourites and recent tools float to the top), digits 1-9 run a row.\n" +
			"Tools that restart or change something ask yes/no first, and every run is written to the audit log.\n" +
			"Your own entries go in tools.json next to settings.json (see `wms menu list`).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
				return usageError("wms menu needs a terminal: use `wms menu list` or `wms menu run <id>` in scripts")
			}
			for {
				all, err := loadTools()
				if err != nil {
					fmt.Fprintln(os.Stderr, ui.Warn(ui.New(), err.Error()))
				}
				var mine []tools.Tool
				for _, t := range all {
					if t.In(tools.Shell) {
						mine = append(mine, t)
					}
				}
				st := tools.LoadState(stateFile())
				p := &tools.Picker{All: mine, State: st, StatusFn: statusLine, Rows: 8, Preview: func(t tools.Tool, answers []string) string {
					argv, err := tools.Expand(t, "wms", repoDir(), answers)
					if err != nil {
						return t.ID
					}
					return strings.Join(argv, " ")
				}}
				if _, err := tea.NewProgram(p).Run(); err != nil {
					return err
				}
				_ = p.State.Save(stateFile()) // pins
				ch := p.Chosen()
				if ch.Quit || ch.Tool.ID == "" {
					return nil
				}
				fmt.Printf("\n%s %s\n\n", ui.New().Brand.Render("▶"), ch.Tool.Title)
				code, err := runTool(ch.Tool, ch.Answers)
				fmt.Println()
				switch {
				case err != nil:
					say(ui.Status(ui.New(), false, err.Error()))
				case code != 0:
					say(ui.Status(ui.New(), false, fmt.Sprintf("%s exited with status %d", ch.Tool.ID, code)))
				default:
					say(ui.Status(ui.New(), true, ch.Tool.Title+" finished"))
				}
				if !loop {
					return nil
				}
				fmt.Print(ui.New().Muted.Render("Press Enter for the list…"))
				fmt.Scanln()
			}
		},
	}
	cmd.Flags().BoolVar(&loop, "loop", false, "return to the list after each tool")

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List every tool with its id (for scripts and tools.json)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			all, err := loadTools()
			if err != nil {
				say(ui.Warn(ui.New(), err.Error()))
			}
			rows := make([][]string, 0, len(all))
			for _, t := range all {
				flags := ""
				if t.Risky {
					flags += "asks "
				}
				if t.Admin {
					flags += "admin "
				}
				rows = append(rows, []string{t.ID, t.Group, t.Title, string(orWhere(t.Where)), strings.TrimSpace(flags)})
			}
			say(ui.RenderColumns(ui.New(), []string{"Id", "Group", "Title", "Where", "Flags"}, rows, fmt.Sprintf("%d tool(s); extra ones come from %s", len(all), toolsFile())))
			return emit(map[string]any{"tools": all})
		},
	})

	var yes bool
	run := &cobra.Command{
		Use:   "run <id> [answers...]",
		Short: "Run one tool by id, without the list",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			all, err := loadTools()
			if err != nil {
				return withCode(exitUsage, err)
			}
			var found *tools.Tool
			for i := range all {
				if all[i].ID == args[0] {
					found = &all[i]
				}
			}
			if found == nil {
				return withCode(exitNotFound, fmt.Errorf("no tool %q: see `wms menu list`", args[0]))
			}
			if !found.In(tools.Shell) {
				return usageError("%s is not offered in the shell", found.ID)
			}
			answers := args[1:]
			for i := range answers {
				if answers[i], err = tools.Answer(answers[i]); err != nil {
					return usageError("answer %d: %v", i+1, err)
				}
			}
			if len(answers) < len(found.Ask) {
				return usageError("%s needs: %s", found.ID, strings.Join(found.Ask, ", "))
			}
			if found.Risky {
				if err := confirm(found.Title+"?", yes); err != nil {
					return err
				}
			}
			code, err := runTool(*found, answers)
			if err != nil {
				return err
			}
			if code != 0 {
				return withCode(code, fmt.Errorf("%s exited with status %d", found.ID, code))
			}
			return nil
		},
	}
	run.Flags().BoolVar(&yes, "yes", false, "don't ask before a risky tool")
	cmd.AddCommand(run)
	return cmd
}

func orWhere(w tools.Where) tools.Where {
	if w == "" {
		return tools.Both
	}
	return w
}
