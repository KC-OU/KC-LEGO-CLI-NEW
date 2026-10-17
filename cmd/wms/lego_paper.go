package main

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

// The paper-first workflow: check a set on paper, mark it done here, enter it into
// the CLI later (wms lego check, which resumes the very same check session), and
// optionally attach a photo/scan of the finished paper sheet as proof. See
// docs/guides/reports.md.

// legoAttachmentsDir is where proof scans/photos are stored: LEGO_ATTACHMENTS_DIR if
// set, else next to lego.db itself, so it travels with the same backups by default.
func legoAttachmentsDir() string {
	if dir := config.Get(config.LegoAttachmentsDir); dir != "" {
		return dir
	}
	return filepath.Join(filepath.Dir(config.Get(config.LegoDBPath)), "attachments")
}

func newLegoPaperDoneCmd() *cobra.Command {
	var by string
	var recount bool
	cmd := &cobra.Command{
		Use:   "paper-done <set>",
		Short: "Mark a set's paper checklist as finished — the CLI entry is still to come",
		Long: "Opens (or resumes) this set's check and stamps it paper-done, without touching any part counts.\n" +
			"It's the same check `wms lego check` resumes into later, so run that next whenever you're ready\n" +
			"to enter it. See `wms lego checks pending` for every set still waiting on its CLI entry.",
		Example: "  wms lego paper-done 75192\n  wms lego paper-done 10696 --recount",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			t := ui.New()
			db, rb, err := openLegoWithClient()
			if err != nil {
				return err
			}
			defer db.Close()
			num := catalogNum(args[0])
			if by == "" {
				by = cliActor()
			}
			kind := lego.CheckIntake
			if recount || db.GetSetState(num).Checked() {
				kind = lego.CheckRecount
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			c, err := db.MarkPaperDone(ctx, rb, num, kind, by, time.Now())
			if err != nil {
				return withCode(exitNotFound, err)
			}
			say(ui.Status(t, true, fmt.Sprintf("Paper done: %s (%s check by %s)", num, kind, by)))
			say(ui.Fact(t, "Next", "wms lego check "+num+map[bool]string{true: " --stocktake", false: ""}[kind == lego.CheckRecount]))
			return emit(map[string]any{"set": num, "kind": kind, "check_id": c.ID, "paper_checked_at": c.PaperCheckedAt})
		},
	}
	cmd.Flags().StringVar(&by, "by", "", "who checked the paper copy (default: your login)")
	cmd.Flags().BoolVar(&recount, "recount", false, "a stock recount, not the first check (default: auto-detected)")
	return cmd
}

func newLegoAttachCmd() *cobra.Command {
	var note, flag string
	cmd := &cobra.Command{
		Use:   "attach <set> <path>",
		Short: "Attach a scanned/photographed proof of the finished paper checklist",
		Long: "Verifies the file is a genuine, readable jpg, png or PDF (checking the actual bytes, not just the\n" +
			"file extension), then stores a copy and records it against this set's current (or most recent)\n" +
			"check — so if the CLI's own count is ever questioned, the proof is the record of what the paper\n" +
			"copy actually said. --flag additionally marks the check disputed, for an admin to review, without\n" +
			"changing any numbers itself.",
		Example: "  wms lego attach 75192 ./scans/75192-checklist.jpg\n  wms lego attach 75192 ./scans/75192.pdf --flag \"CLI shows 3 missing, paper shows 0\"",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			t := ui.New()
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			num := catalogNum(args[0])
			src := args[1]
			kind, err := lego.VerifyProofFile(src)
			if err != nil {
				return usageError("%s", err)
			}
			c, err := db.CurrentOrLastCheck(num)
			if err != nil {
				return err
			}
			if c == nil {
				return usageError("set %s has no check yet — run `wms lego paper-done %s` or `wms lego check %s` first", num, num, num)
			}
			dst, err := lego.StoreProof(legoAttachmentsDir(), num, kind, src)
			if err != nil {
				return err
			}
			by := cliActor()
			if _, err := db.AttachProof(c.ID, num, kind, dst, note, by); err != nil {
				return err
			}
			say(ui.Status(t, true, fmt.Sprintf("Attached %s proof for %s (check #%d)", kind, num, c.ID)))
			say(ui.Fact(t, "Stored", dst))
			if flag != "" {
				if err := db.FlagDispute(c.ID, flag); err != nil {
					return err
				}
				say(ui.Warn(t, "Flagged disputed: "+flag))
			}
			return emit(map[string]any{"set": num, "check_id": c.ID, "kind": kind, "file": dst, "flagged": flag != ""})
		},
	}
	cmd.Flags().StringVar(&note, "note", "", "a short note to keep with the proof")
	cmd.Flags().StringVar(&flag, "flag", "", "mark this check disputed, with a reason (e.g. a CLI/paper mismatch)")
	return cmd
}

func newLegoChecksCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "checks", Short: "Set check sessions across the collection"}
	cmd.AddCommand(newLegoChecksPendingCmd())
	return cmd
}

func newLegoChecksPendingCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pending",
		Short: "Sets whose paper checklist is done but the CLI entry still isn't",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			t := ui.New()
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			list, err := db.PendingPaperChecks()
			if err != nil {
				return err
			}
			if len(list) == 0 {
				say(ui.Status(t, true, "Nothing pending — every paper-done set has been entered"))
				return emit(list)
			}
			for _, c := range list {
				say(ui.Fact(t, c.SetNum, fmt.Sprintf("paper done %s by %s (%s) — wms lego check %s", c.PaperCheckedAt.Local().Format("2 Jan 15:04"), c.CheckedBy, c.Kind, c.SetNum)))
			}
			return emit(list)
		},
	}
	return cmd
}
