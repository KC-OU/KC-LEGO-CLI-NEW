package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/audit"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/plugin"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

func pluginManager() *plugin.Manager {
	l := audit.New()
	return plugin.Default(func(action, status, details string) { _ = l.Log(envOr("WMS_USER", "cli"), "", action, status, details) })
}

// emitHook tells enabled plugins about an event, waiting for them (the CLI is short-lived,
// so an asynchronous hook would be cut off when the command ends).
func emitHook(event string, payload any) {
	m := pluginManager()
	for _, r := range m.Emit(context.Background(), event, payload) {
		if r.ExitCode != 0 || r.TimedOut {
			say(ui.Warn(ui.New(), fmt.Sprintf("plugin %s did not finish cleanly for %s (exit %d%s)", r.Plugin, event, r.ExitCode, map[bool]string{true: ", timed out", false: ""}[r.TimedOut])))
		}
	}
}

func newPluginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plugin",
		Short: "Your own programs: `wms foo` runs wms-foo, and enabled plugins can react to events",
		Long: "Put an executable named wms-<name> in the plugin folder (WMS_PLUGIN_DIR, default\n" +
			"/root/docker-server/wms/plugins). It does nothing until you review and enable it, which pins its checksum;\n" +
			"if the file changes it refuses to run until you enable it again. Plugins get a scrubbed environment (no keys\n" +
			"or settings), a 10-second limit for hooks, and run as an unprivileged user when wms runs as root.\n" +
			"Events: " + strings.Join(plugin.Events, ", ") + ". A hook is run as `wms-<name> hook <event>` with the event as JSON on stdin.",
	}
	cmd.AddCommand(newPluginListCmd(), newPluginEnableCmd(), newPluginDisableCmd(), newPluginNewCmd(), newPluginRunCmd())
	return cmd
}

func newPluginListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Show the plugins in the folder and their state",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			t := ui.New()
			m := pluginManager()
			infos, err := m.List()
			if err != nil {
				if os.IsNotExist(err) {
					say(ui.Warn(t, "No plugin folder yet: `wms plugin new hello` creates "+m.Dir+" and an example."))
					return emit(map[string]any{"plugins": []any{}, "dir": m.Dir})
				}
				return withCode(exitFailure, err)
			}
			var rows [][]string
			for _, i := range infos {
				state := "not enabled"
				switch {
				case i.Problem != "":
					state = "PROBLEM: " + i.Problem
				case i.Modified:
					state = "CHANGED since enabled — refused"
				case i.Entry.Enabled:
					state = "enabled"
				case i.Known:
					state = "disabled"
				}
				sum := ""
				if len(i.SHA256) >= 12 {
					sum = i.SHA256[:12]
				}
				rows = append(rows, []string{i.Name, state, orDash(strings.Join(i.Entry.Hooks, ",")), orDash(i.Entry.User), sum})
			}
			if len(rows) == 0 {
				say(ui.Warn(t, "No plugins in "+m.Dir+". `wms plugin new hello` makes an example."))
			} else {
				say(ui.RenderColumns(t, []string{"Plugin", "State", "Hooks", "Runs as", "SHA-256"}, rows, m.Dir))
			}
			return emit(map[string]any{"plugins": infos, "dir": m.Dir})
		},
	}
}

func newPluginEnableCmd() *cobra.Command {
	var hooks []string
	var asUser string
	var yes bool
	cmd := &cobra.Command{
		Use:   "enable <name> [--hooks part_added,low_stock] [--as-user root]",
		Short: "Review a plugin's checksum and turn it on",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			t := ui.New()
			m := pluginManager()
			sum, err := m.Inspect(args[0])
			if err != nil {
				return withCode(exitNotFound, err)
			}
			who := asUser
			if who == "" {
				who = m.DefaultUser
			}
			say(ui.Fact(t, "Plugin", args[0]))
			say(ui.Fact(t, "File", m.Dir+"/wms-"+args[0]))
			say(ui.Fact(t, "SHA-256", sum))
			say(ui.Fact(t, "Runs as", who))
			say(ui.Fact(t, "Events", orDash(strings.Join(hooks, ", "))))
			if asUser == "root" {
				say(ui.Warn(t, "--as-user root: this plugin will have full control of this machine. Only do this for a program you wrote or fully read."))
			}
			if err := confirm(fmt.Sprintf("You have read %s and it may run", "wms-"+args[0])+"?", yes); err != nil {
				return err
			}
			hookList := splitCSV(hooks)
			if _, err := m.Enable(args[0], hookList, asUser); err != nil {
				return usageError("%v", err)
			}
			say(ui.Status(t, true, "Enabled. Its checksum is pinned: if the file changes, it will refuse to run until you enable it again."))
			return emit(map[string]any{"enabled": args[0], "sha256": sum, "hooks": hookList, "user": who})
		},
	}
	cmd.Flags().StringSliceVar(&hooks, "hooks", nil, "events to tell it about: "+strings.Join(plugin.Events, ", "))
	cmd.Flags().StringVar(&asUser, "as-user", "", "run as this user instead of the unprivileged default")
	cmd.Flags().BoolVar(&yes, "yes", false, "don't ask for confirmation")
	return cmd
}

func splitCSV(in []string) []string {
	var out []string
	for _, s := range in {
		for _, p := range strings.Split(s, ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

func newPluginDisableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "disable <name>",
		Short: "Turn a plugin off",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := pluginManager().Disable(args[0]); err != nil {
				return withCode(exitNotFound, err)
			}
			say(ui.Status(ui.New(), true, "Disabled."))
			return emit(map[string]any{"disabled": args[0]})
		},
	}
}

func newPluginNewCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "new <name>",
		Short: "Create an example plugin (not enabled) to start from",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			t := ui.New()
			p, err := pluginManager().Scaffold(args[0])
			if err != nil {
				return usageError("%v", err)
			}
			say(ui.Status(t, true, "Created "+p))
			say(t.Muted.Render("   Read it, then: wms plugin enable " + args[0] + " --hooks part_added"))
			return emit(map[string]any{"created": p})
		},
	}
}

func newPluginRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "run <name> [args...]",
		Short:              "Run an enabled plugin (same as `wms <name> [args...]`)",
		Args:               cobra.MinimumNArgs(1),
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if args[0] == "-h" || args[0] == "--help" { // flag parsing is off so the plugin gets its own flags
				return cmd.Help()
			}
			return runPlugin(args[0], args[1:])
		},
	}
}

// runPlugin runs an enabled plugin with the terminal attached and exits with its status.
func runPlugin(name string, args []string) error {
	code, err := pluginManager().Run(context.Background(), name, args, os.Stdin, os.Stdout, os.Stderr)
	if err != nil {
		return withCode(exitFailure, err)
	}
	if code != 0 {
		return withCode(code, fmt.Errorf("plugin %s exited with status %d", name, code))
	}
	return nil
}

// dispatchPlugin handles `wms <name> ...` when <name> is not a built-in command but is a
// plugin in the plugin folder. It reports whether it handled the command line.
func dispatchPlugin(root *cobra.Command, argv []string) (handled bool, err error) {
	if len(argv) == 0 || strings.HasPrefix(argv[0], "-") || !plugin.ValidName(argv[0]) {
		return false, nil
	}
	if c, _, ferr := root.Find(argv); ferr == nil && c != root {
		return false, nil // a real subcommand
	}
	for _, c := range root.Commands() {
		if c.Name() == argv[0] || c.HasAlias(argv[0]) {
			return false, nil
		}
	}
	m := pluginManager()
	if _, statErr := os.Lstat(m.Dir + "/wms-" + argv[0]); statErr != nil {
		return false, nil // not a plugin either: let cobra report the unknown command
	}
	return true, runPlugin(argv[0], argv[1:])
}
