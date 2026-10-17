package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/labels"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

func newLegoBagSizeCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "bag-size <part_num> [small|main]",
		Short:             "Which bag a part goes in while picking/checking: easy-to-lose small parts, or the main bag (default)",
		Example:           "  wms lego bag-size 3024\n  wms lego bag-size 3024 small\n  wms lego bag-size 3024 main",
		Args:              cobra.RangeArgs(1, 2),
		ValidArgsFunction: completePartNum,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			partNum := args[0]
			if len(args) == 1 {
				size, err := db.PartBagSize(partNum)
				if err != nil {
					return err
				}
				say(fmt.Sprintf("%s is currently in the %s bag.", partNum, size))
				return emit(map[string]any{"part": partNum, "size": size})
			}
			size := args[1]
			if size != lego.BagSmall && size != lego.BagMain {
				return usageError("say `small` or `main`")
			}
			if err := db.SetBagSize(partNum, size); err != nil {
				return err
			}
			say(ui.Status(ui.New(), true, fmt.Sprintf("%s is now in the %s bag.", partNum, size)))
			return emit(map[string]any{"part": partNum, "size": size})
		},
	}
}

func newLegoSmallBagLabelCmd() *cobra.Command {
	var size, format, outPath string
	cmd := &cobra.Command{
		Use:   "small-bag-label <set> [bag-code]",
		Short: "A label for the set's small-parts bag — the set number, name, and (if given) the bag's own barcode",
		Long: "Reuses the same label layout as `wms lego labels`, with the location line replaced by \"SMALL PARTS\".\n" +
			"Giving a bag code encodes it as the label's own barcode, so the exact same code this label carries is what\n" +
			"gets scanned back in at `wms lego check` finish time to confirm the bag (see docs/guides/set-checks.md).\n" +
			"Defaults to a small sticky-label size (50x30), not the full set-label sheet size.",
		Example: "  wms lego small-bag-label 75192\n  wms lego small-bag-label 75192 BAG-0042 -o bag.pdf",
		Args:    cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			s, err := labels.SizeByID(size)
			if err != nil {
				return usageError("%v", err)
			}
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			d := db.LabelData(args[0])
			d.Location = "SMALL PARTS"
			if len(args) == 2 {
				d.Barcode = args[1]
			}
			var body []byte
			switch strings.ToLower(format) {
			case "pdf":
				body = labels.PDF([]labels.Data{d}, s)
			case "html":
				body = labels.HTML([]labels.Data{d}, s)
			case "png":
				if body, err = labels.PNG([]labels.Data{d}, s); err != nil {
					return usageError("%v", err)
				}
			case "zpl":
				if body, err = labels.ZPL([]labels.Data{d}, s); err != nil {
					return usageError("%v", err)
				}
			default:
				return usageError("--format must be pdf, html, png or zpl")
			}
			if outPath == "" {
				_, _ = cmd.OutOrStdout().Write(body)
				return nil
			}
			abs, err := writeExportFile(outPath, body, true)
			if err != nil {
				return err
			}
			say(ui.Status(ui.New(), true, "Wrote the small-bag label to "+abs))
			return emit(map[string]any{"file": abs, "set": d.SetNum})
		},
	}
	cmd.Flags().StringVarP(&outPath, "out", "o", "", "output file (default: print it)")
	cmd.Flags().StringVar(&size, "size", "50x30", "label size — see wms lego labels --help")
	cmd.Flags().StringVar(&format, "format", "pdf", "pdf, html, png or zpl")
	return cmd
}

// newLegoBagLabelsCmd prints every bag label a set currently has in one go —
// the small and main bags' codes are deterministic (lego.BagCode), so they
// print even before a check has started; the extra bag isn't printed up
// front (a checker doesn't know ahead of time if they'll find a spare) and
// only appears here once a check has actually created its instance. Backed
// by DB.PrintableBags, which reads every bag's own true code and kind (a
// pool-assigned bag's chosen code, a second instance's "-A2", a custom kind)
// rather than recomputing one — so rerunning this is also how you reprint a
// damaged or unscannable label: same set, same output, identical every time.
func newLegoBagLabelsCmd() *cobra.Command {
	var size, format, outDir string
	var force bool
	cmd := &cobra.Command{
		Use:   "bag-labels <set>",
		Short: "Print (or reprint) every bag label a set currently has — small, main, and extra once one exists",
		Long: "The small and main bags' codes are deterministic (\"<set>-A\"/\"<set>-B\"), so they print even before a\n" +
			"check has started. The extra bag isn't printed up front — it's created the first time a check finds a\n" +
			"spare part, and only then shows up here too. Safe to run again any time — a damaged or unscannable\n" +
			"label reprints identically, pool-assigned and additional-instance bags included.",
		Example: "  wms lego bag-labels 75192\n  wms lego bag-labels 75192 -o ./labels --format zpl",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			s, err := labels.SizeByID(size)
			if err != nil {
				return usageError("%v", err)
			}
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			num := catalogNum(args[0])
			bags, err := db.PrintableBags(num)
			if err != nil {
				return err
			}
			t := ui.New()
			stemReplacer := strings.NewReplacer("/", "-", "\\", "-", ":", "-")
			for _, b := range bags {
				k, err := db.BagKindByKey(b.BagKindKey)
				if err != nil || k == nil {
					continue
				}
				d := db.LabelData(num)
				d.Location = strings.ToUpper(k.Name)
				d.Barcode = b.Code
				var body []byte
				switch strings.ToLower(format) {
				case "pdf":
					body = labels.PDF([]labels.Data{d}, s)
				case "html":
					body = labels.HTML([]labels.Data{d}, s)
				case "png":
					if body, err = labels.PNG([]labels.Data{d}, s); err != nil {
						return usageError("%v", err)
					}
				case "zpl":
					if body, err = labels.ZPL([]labels.Data{d}, s); err != nil {
						return usageError("%v", err)
					}
				default:
					return usageError("--format must be pdf, html, png or zpl")
				}
				name := fmt.Sprintf("%s.%s", stemReplacer.Replace(b.Code), strings.ToLower(format))
				out := name
				if outDir != "" {
					out = filepath.Join(outDir, name)
				}
				abs, err := writeExportFile(out, body, force)
				if err != nil {
					return err
				}
				say(ui.Status(t, true, fmt.Sprintf("Wrote the %s bag label (%s) to %s", k.Name, d.Barcode, abs)))
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&outDir, "out", "o", "", "directory to write the label files into (default: current directory)")
	cmd.Flags().StringVar(&size, "size", "50x30", "label size — see wms lego labels --help")
	cmd.Flags().StringVar(&format, "format", "pdf", "pdf, html, png or zpl")
	cmd.Flags().BoolVar(&force, "force", false, "replace output files if they exist")
	return cmd
}

// newLegoBagAddCmd registers and prints another instance of a bag kind for
// a set that's past its default one-per-kind limit (a huge set needing two
// small-parts bags, say) -- raise the limit first (`set-info --max-bags`),
// then this is what actually produces the second ("<set>-A2") label; without
// it, AddBagInstance's cap-raising had nothing that could ever call it.
func newLegoBagAddCmd() *cobra.Command {
	var size, format, outPath string
	cmd := &cobra.Command{
		Use:   "bag-add <set> <kind>",
		Short: "Register and print another instance of a bag kind, once a set's default limit has been raised",
		Long: "A set normally gets one instance of each bag kind (small, main, ...). Raise its limit first\n" +
			"(`wms lego set-info <set> --max-bags N`), then this registers and prints the next instance's\n" +
			"label (\"<set>-A2\", \"<set>-A3\", ...) against the set's current or last check.",
		Example: "  wms lego set-info 75192 --max-bags 3\n  wms lego bag-add 75192 small",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			s, err := labels.SizeByID(size)
			if err != nil {
				return usageError("%v", err)
			}
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			num := catalogNum(args[0])
			c, err := db.CurrentOrLastCheck(num)
			if err != nil {
				return err
			}
			if c == nil {
				return usageError("set %s has never been checked — start a check first", num)
			}
			bag, err := db.AddBagInstance(c.ID, num, args[1], cliActor())
			if err != nil {
				return usageError("%v", err)
			}
			k, err := db.BagKindByKey(bag.BagKindKey)
			if err != nil || k == nil {
				return usageError("%q isn't a registered bag kind", args[1])
			}
			d := db.LabelData(num)
			d.Location = strings.ToUpper(k.Name)
			d.Barcode = bag.Code
			var body []byte
			switch strings.ToLower(format) {
			case "pdf":
				body = labels.PDF([]labels.Data{d}, s)
			case "html":
				body = labels.HTML([]labels.Data{d}, s)
			case "png":
				if body, err = labels.PNG([]labels.Data{d}, s); err != nil {
					return usageError("%v", err)
				}
			case "zpl":
				if body, err = labels.ZPL([]labels.Data{d}, s); err != nil {
					return usageError("%v", err)
				}
			default:
				return usageError("--format must be pdf, html, png or zpl")
			}
			if outPath == "" {
				_, _ = cmd.OutOrStdout().Write(body)
				return nil
			}
			abs, err := writeExportFile(outPath, body, true)
			if err != nil {
				return err
			}
			say(ui.Status(ui.New(), true, fmt.Sprintf("Wrote the %s bag label (%s) to %s", k.Name, bag.Code, abs)))
			return emit(map[string]any{"file": abs, "set": num, "code": bag.Code})
		},
	}
	cmd.Flags().StringVarP(&outPath, "out", "o", "", "output file (default: print it)")
	cmd.Flags().StringVar(&size, "size", "50x30", "label size — see wms lego labels --help")
	cmd.Flags().StringVar(&format, "format", "pdf", "pdf, html, png or zpl")
	return cmd
}

// newLegoBagKindCmd is the global bag_kinds registry — small/main/extra are
// built in, an admin can add more (e.g. "wheels"). Shell access is already
// the authorization boundary for every CLI command in this suite, same as
// everything else under `wms lego`.
func newLegoBagKindCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "bag-kind", Short: "The registered bag kinds (small, main, extra, and any added beyond those)"}
	cmd.AddCommand(newLegoBagKindListCmd(), newLegoBagKindAddCmd())
	return cmd
}

func newLegoBagKindListCmd() *cobra.Command {
	return &cobra.Command{
		Use: "list", Short: "List every registered bag kind", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			kinds, err := db.BagKinds()
			if err != nil {
				return err
			}
			t := ui.New()
			for _, k := range kinds {
				say(ui.Fact(t, k.Key, k.Name))
			}
			return emit(kinds)
		},
	}
}

func newLegoBagKindAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "add <key> <name>",
		Short:   "Register a new bag kind beyond small/main/extra",
		Example: "  wms lego bag-kind add wheels Wheels",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			k, err := db.AddBagKind(args[0], args[1], cliActor())
			if err != nil {
				return usageError("%v", err)
			}
			say(ui.Status(ui.New(), true, fmt.Sprintf("Registered bag kind %q (%s).", k.Key, k.Name)))
			return emit(k)
		},
	}
}

// newLegoBagPoolCmd is the reusable bag pool: real, permanent containers with
// their own stable code, assigned to whichever check currently needs one
// instead of a fresh auto-generated "<set>-A" per check — freed back to
// available automatically once that check finishes (see bags.go's
// releasePoolBagsForCheck).
func newLegoBagPoolCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "bag-pool", Short: "Reusable physical bags with their own stable code, assigned to whichever set currently needs one"}
	cmd.AddCommand(newLegoBagPoolAddCmd(), newLegoBagPoolListCmd(), newLegoBagPoolAssignCmd(), newLegoBagPoolReleaseCmd())
	return cmd
}

func newLegoBagPoolAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "add <code> <kind>",
		Short:   "Register a reusable bag under its own stable code",
		Example: "  wms lego bag-pool add BIN-07 small",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			p, err := db.AddPoolBag(args[0], args[1], cliActor())
			if err != nil {
				return usageError("%v", err)
			}
			say(ui.Status(ui.New(), true, fmt.Sprintf("Registered pool bag %q (%s).", p.Code, p.BagKindKey)))
			return emit(p)
		},
	}
}

func newLegoBagPoolListCmd() *cobra.Command {
	return &cobra.Command{
		Use: "list", Short: "List every reusable bag and what it's currently doing", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			bags, err := db.PoolBags()
			if err != nil {
				return err
			}
			t := ui.New()
			for _, b := range bags {
				status := b.Status
				if b.CurrentCheckID > 0 {
					status = fmt.Sprintf("%s (check #%d)", status, b.CurrentCheckID)
				}
				say(ui.Fact(t, b.Code, fmt.Sprintf("%s — %s", b.BagKindKey, status)))
			}
			return emit(bags)
		},
	}
}

func newLegoBagPoolAssignCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "assign <code> <set>",
		Short:   "Hand a reusable bag to a set's current check instead of printing a fresh code",
		Example: "  wms lego bag-pool assign BIN-07 75192",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			num := catalogNum(args[1])
			c, err := db.CurrentOrLastCheck(num)
			if err != nil {
				return err
			}
			if c == nil {
				return usageError("set %s has never been checked — start a check first", num)
			}
			b, err := db.AssignPoolBag(c.ID, cliActor(), args[0])
			if err != nil {
				return usageError("%v", err)
			}
			say(ui.Status(ui.New(), true, fmt.Sprintf("Bag %q assigned to %s's check (#%d).", b.Code, num, c.ID)))
			return emit(b)
		},
	}
}

func newLegoBagPoolReleaseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "release <code>",
		Short: "Free a reusable bag back to available by hand (normally happens automatically when its check finishes)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			if err := db.ReleasePoolBag(args[0]); err != nil {
				return err
			}
			say(ui.Status(ui.New(), true, fmt.Sprintf("Bag %q is available again.", args[0])))
			return emit(map[string]any{"code": args[0], "status": "available"})
		},
	}
}

// newLegoBagFindCmd is the lost-bag lookup: what a code belongs to,
// independent of any active checking session -- a bag found without its
// checker, or double-checking a label before it goes on.
func newLegoBagFindCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "bag-find <code>",
		Short:   "What a bag code belongs to — a lost bag found without its checker, or double-checking a label",
		Example: "  wms lego bag-find 75192-A\n  wms lego bag-find BIN-07",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			r, err := db.FindBag(args[0])
			if err != nil {
				return err
			}
			if !r.Found {
				say(ui.Status(ui.New(), false, fmt.Sprintf("Nothing known about bag code %q.", args[0])))
				return emit(r)
			}
			t := ui.New()
			if r.Pool {
				say(ui.Fact(t, "Pool bag", fmt.Sprintf("%s (%s)", r.BagKindName, r.PoolStatus)))
			} else {
				say(ui.Fact(t, "Bag kind", r.BagKindName))
			}
			if r.SetNum != "" {
				status := "in progress"
				if r.CheckDone {
					status = "finished"
				}
				say(ui.Fact(t, "Belongs to", fmt.Sprintf("set %s (check %s)", r.SetNum, status)))
			}
			if !r.ScannedAt.IsZero() {
				say(ui.Fact(t, "Last scanned", r.ScannedAt.Format("2 Jan 2006 15:04")))
			}
			if r.CreatedBy != "" {
				by := r.CreatedBy
				if !r.CreatedAt.IsZero() {
					by += " (" + r.CreatedAt.Format("2 Jan 2006 15:04") + ")"
				}
				say(ui.Fact(t, "Printed by", by))
			}
			if r.CheckDone {
				result := fmt.Sprintf("%d missing", r.Missing)
				if r.Missing == 0 {
					result = "COMPLETE"
				}
				if r.Extra > 0 {
					result += fmt.Sprintf(", %d extra", r.Extra)
				}
				say(ui.Fact(t, "Result", result))
			}
			if r.Disputed {
				say(ui.Fact(t, "Disputed", orDash(r.DisputeNote)))
			}
			return emit(r)
		},
	}
}
