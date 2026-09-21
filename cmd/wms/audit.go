package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/audit"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

func newAuditCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "audit", Short: "Check the tamper-evident audit log"}

	cmd.AddCommand(&cobra.Command{
		Use:   "verify",
		Short: "Recompute the audit log's hash chain; exits non-zero at the first broken line",
		Long: "Every line the suite writes carries a hash of itself and the line before it, and the first one pins the\n" +
			"whole file as it was when the chain began. Editing, deleting or reordering a line is reported with its\n" +
			"line number. This does NOT catch someone with root recomputing the chain, or dropping the newest lines:\n" +
			"compare `wms audit head` against a copy kept somewhere this machine cannot write.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			return verifyAudit(audit.New())
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "head",
		Short: "Print the chain's newest hash, to keep a copy off this machine",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := audit.New().Verify()
			if err != nil {
				return err
			}
			if r.Head == "" {
				return fmt.Errorf("no chain yet")
			}
			if !r.OK() {
				return fmt.Errorf("the chain is broken at line %d; run `wms audit verify`", r.BrokenAt)
			}
			fmt.Printf("%s  %d chained lines\n", r.Head, r.Chained)
			return nil
		},
	})
	return cmd
}

// verifyAudit prints the chain report and fails (non-zero exit) on any break.
// A break is checked before "no chain yet": tampering with the lines from
// before the chain began is a break found at the CHAIN_START line.
func verifyAudit(l *audit.Logger) error {
	t := ui.New()
	r, err := l.Verify()
	if err != nil {
		return err
	}
	say(ui.Fact(t, "Log", l.Path))
	say(ui.Fact(t, "Lines", fmt.Sprint(r.Lines)))
	if !r.OK() {
		say(ui.Status(t, false, fmt.Sprintf("BROKEN at line %d: %s", r.BrokenAt, r.BrokenWhy)))
		_ = emit(map[string]any{"ok": false, "broken_at": r.BrokenAt, "reason": r.BrokenWhy, "lines": r.Lines})
		return fmt.Errorf("audit log failed verification at line %d", r.BrokenAt)
	}
	if r.Genesis == 0 {
		say(ui.Warn(t, "No chain yet: it starts with the next audited action, and covers this file from then on."))
		return emit(map[string]any{"ok": true, "lines": r.Lines, "chained": 0})
	}
	say(ui.Fact(t, "Chained lines", fmt.Sprintf("%d (chain starts at line %d)", r.Chained, r.Genesis)))
	if n := len(r.Foreign); n > 0 {
		say(ui.Warn(t, fmt.Sprintf("%d line(s) after the chain began carry no hash (written by something else, e.g. the old Python suite): first at line %d.", n, r.Foreign[0])))
	}
	say(ui.Status(t, true, "Chain intact. Head: "+r.Head))
	return emit(map[string]any{"ok": true, "lines": r.Lines, "chained": r.Chained, "genesis_line": r.Genesis, "foreign_lines": r.Foreign, "head": r.Head})
}
