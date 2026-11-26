package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/audit"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

// onboardingGate stops a checker (a numeric worker ID) starting a first check of a set before the paper check
// is verified. Anyone else on the command line (an admin or the machine's own login) is not gated.
func onboardingGate(db *lego.DB, set, kind, by string) error {
	if lego.WorkerID(by) == "" {
		return nil
	}
	return db.OnboardingGate(set, kind, by)
}

func newOnboardingCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "onboarding",
		Short: "The paper-check onboarding of a set: status, the verify queue, waivers and the final submit",
		Long: "A checker does the paper check of a set first (the sorting sheet, filled in or printed) and uploads it on the\n" +
			"dashboard's Onboarding page. An admin verifies it here or on the dashboard; only then can the check be started.\n" +
			"When the check is finished, submit completes the onboarding and prints the starting Markdown for the blog post.",
	}
	var user string
	status := &cobra.Command{
		Use: "status <set>", Short: "Where a set's onboarding stands for a checker", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			t := ui.New()
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			num, who := catalogNum(args[0]), orActor(user)
			f, err := db.FlowFor(num, who)
			if err != nil {
				return err
			}
			if f == nil {
				say(ui.Fact(t, "Onboarding", "not started for "+who+" — upload the paper check on the dashboard's Onboarding page"))
				return emit(map[string]any{"set": num, "user": who, "status": "none", "cleared": db.OnboardingCleared(num, who)})
			}
			say(ui.Fact(t, "Onboarding", fmt.Sprintf("#%d  %s  (%s)", f.ID, f.Status, f.SetNum)))
			if f.VerifyNote != "" {
				say(ui.Fact(t, "Admin note", f.VerifyNote))
			}
			return emit(map[string]any{"id": f.ID, "set": f.SetNum, "user": f.User, "status": f.Status, "note": f.VerifyNote, "cleared": db.OnboardingCleared(num, who)})
		},
	}
	status.Flags().StringVar(&user, "user", "", "the checker's ID (default: your login)")

	var statusFilter string
	list := &cobra.Command{
		Use: "list", Short: "Paper checks waiting for an admin (or --status to see others)", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			if statusFilter == "" {
				statusFilter = lego.FlowAwaitingVerify
			}
			rows, err := db.ListFlows(statusFilter)
			if err != nil {
				return err
			}
			for _, f := range rows {
				say(fmt.Sprintf("#%d  set %s  by %s  %s  %s", f.ID, f.SetNum, f.User, f.Status, f.UpdatedAt))
			}
			if len(rows) == 0 {
				say("   nothing " + strings.ReplaceAll(statusFilter, "_", " "))
			}
			return emit(map[string]any{"status": statusFilter, "flows": rows})
		},
	}
	list.Flags().StringVar(&statusFilter, "status", "", "awaiting_paper, awaiting_verify (default), changes_needed, verified or complete")

	var note, tags string
	var ok, changes bool
	verify := &cobra.Command{
		Use: "verify <id>", Short: "Verify a paper check (--ok), or send it back with a note (--changes --note ...)", Args: cobra.ExactArgs(1),
		Example: "  wms onboarding verify 12 --ok\n  wms onboarding verify 12 --changes --note \"IDs missing on pages 2-3\" --tags \"IDs missing\"",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			if ok == changes {
				return usageError("say --ok or --changes")
			}
			var id int64
			if _, err := fmt.Sscan(args[0], &id); err != nil {
				return usageError("%q is not an onboarding number", args[0])
			}
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			by := cliActor()
			f, err := db.VerifyFlow(id, by, ok, note, tags)
			if err != nil {
				return err
			}
			msg := "Your paper check for set " + f.SetNum + " is verified. You can start the check now."
			if !ok {
				msg = "Your paper check for set " + f.SetNum + " needs changes: " + strings.TrimSpace(note) + " Redo it and upload it again."
			}
			_ = db.SendMessage(by, f.User, msg)
			_ = audit.New().Log(by, "", "ONBOARDING_VERIFY", "SUCCESS", fmt.Sprintf("set=%s user=%s ok=%v via cli", f.SetNum, f.User, ok))
			say(ui.Fact(ui.New(), "Onboarding", fmt.Sprintf("#%d is now %s", f.ID, f.Status)))
			return emit(map[string]any{"id": f.ID, "status": f.Status})
		},
	}
	verify.Flags().BoolVar(&ok, "ok", false, "the paper check is right: the checker can carry on")
	verify.Flags().BoolVar(&changes, "changes", false, "send it back (needs --note)")
	verify.Flags().StringVar(&note, "note", "", "what has to change")
	verify.Flags().StringVar(&tags, "tags", "", "quick reasons: IDs missing, counts wrong, sheet unreadable, wrong set")

	var off bool
	waive := &cobra.Command{
		Use: "waive <set|*>", Short: "Let anyone check this set (or every set, with *) without the paper-check onboarding", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			target := args[0]
			if target != "*" {
				target = catalogNum(target)
			}
			if err := db.WaiveOnboarding(target, cliActor(), !off); err != nil {
				return err
			}
			_ = audit.New().Log(cliActor(), "", "ONBOARDING_WAIVER", "SUCCESS", fmt.Sprintf("%s off=%v via cli", target, off))
			return emit(map[string]any{"waived": db.Waivers()})
		},
	}
	waive.Flags().BoolVar(&off, "off", false, "remove the waiver")

	var yes bool
	submit := &cobra.Command{
		Use: "submit <set>", Short: "Complete the onboarding after the check and print the blog template", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			t := ui.New()
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			num, who := catalogNum(args[0]), orActor(user)
			f, err := db.FlowFor(num, who)
			if err != nil || f == nil {
				return usageError("no onboarding of %s for %s", num, who)
			}
			if !yes {
				fmt.Fprint(os.Stderr, "Complete the onboarding and generate the blog template? [y/N] (N = go back and recheck) ")
				line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
				if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "y") {
					say(ui.Warn(t, "Not submitted: recheck the set, then submit again."))
					return nil
				}
			}
			if _, err := db.CompleteFlow(f.ID, who, "{}"); err != nil {
				return err
			}
			label := num
			if cs, _ := db.CatalogSet(num); cs != nil {
				label = cs.Name
			}
			_ = db.EnsureDraftForSet(num, label, who)
			_ = audit.New().Log(who, "", "ONBOARDING_COMPLETE", "SUCCESS", "set="+num+" via cli")
			say(ui.Status(t, true, "Onboarding complete — now write the blog post:"))
			fmt.Print(db.BlogTemplate(num))
			return nil
		},
	}
	submit.Flags().StringVar(&user, "user", "", "the checker's ID (default: your login)")
	submit.Flags().BoolVar(&yes, "yes", false, "skip the question")
	cmd.AddCommand(status, list, verify, waive, submit)
	return cmd
}

func orActor(user string) string {
	if user != "" {
		return user
	}
	return cliActor()
}
