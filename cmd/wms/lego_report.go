package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

// writeReport renders data as the clean printable report (default) or, with an
// explicit --format, whatever lego.Encode supports (csv/xlsx/json/plain html/...).
func writeReport(t ui.Theme, data *lego.ExportData, format, outPath string, force bool, discord discordFlags) error {
	if format == "" {
		format = "report"
	}
	body, _, _, err := lego.Encode(format, data)
	if err != nil {
		return usageError("%v", err)
	}
	if outPath == "" {
		if discord.send {
			return usageError("--discord needs -o <file>: there's nothing saved to send otherwise")
		}
		_, _ = os.Stdout.Write(body)
		return nil
	}
	abs, err := writeExportFile(outPath, body, force)
	if err != nil {
		return err
	}
	n, unit := len(data.Rows), "row(s)"
	if n == 0 && len(data.Sets) > 0 {
		n, unit = len(data.Sets), "set(s)"
	}
	say(ui.Status(t, true, fmt.Sprintf("Wrote %s (%d %s)", abs, n, unit)))
	if err := sendToDiscord(t, discord, abs, data.Title); err != nil {
		return err
	}
	return emit(map[string]any{"file": abs, "rows": len(data.Rows), "sets": len(data.Sets), "facts": data.Facts})
}

func newLegoReportCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "report", Short: "Printable reports: stock-take, parts lists, missing/extra parts, sets, orders, check history"}
	cmd.AddCommand(newLegoReportStocktakeCmd(), newLegoReportSetPartsCmd(), newLegoReportSetListCmd(),
		newLegoReportMissingCmd(), newLegoReportExtraCmd(), newLegoReportOrdersCmd(), newLegoReportHistoryCmd())
	return cmd
}

func newLegoReportMissingCmd() *cobra.Command {
	var format, outPath string
	var force bool
	var discord discordFlags
	cmd := &cobra.Command{
		Use:     "missing [set...]",
		Short:   "Missing Parts: what's still missing, for the given sets or every incomplete set",
		Example: "  wms lego report missing -o missing.html\n  wms lego report missing 75192 -o falcon-missing.pdf --format csv",
		Args:    cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			t := ui.New()
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			data, err := db.MissingPartsReport(args)
			if err != nil {
				return err
			}
			return writeReport(t, data, format, outPath, force, discord)
		},
	}
	cmd.Flags().StringVar(&format, "format", "", "output format (default: the printable report; also csv/xlsx/json/html/sorting-html)")
	cmd.Flags().StringVarP(&outPath, "out", "o", "", "write to this file (default: print it)")
	cmd.Flags().BoolVar(&force, "force", false, "replace the output file if it exists")
	discord.register(cmd.Flags())
	return cmd
}

// newLegoReportSetListCmd is "List of Sets on Collection": sets only, no loose parts
// (for sets + loose parts together, `wms lego export` already covers that). It has its
// own SetsListHTML by default, since the generic ReportHTML only ever renders parts rows.
func newLegoReportSetListCmd() *cobra.Command {
	var format, outPath string
	var force bool
	var discord discordFlags
	cmd := &cobra.Command{
		Use:     "setlist",
		Short:   "List of Sets on Collection: every set you own, no loose parts",
		Example: "  wms lego report setlist -o sets.html",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			t := ui.New()
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			data, err := db.SetsListReport()
			if err != nil {
				return err
			}
			return writeReport(t, data, format, outPath, force, discord)
		},
	}
	cmd.Flags().StringVar(&format, "format", "", "output format (default: the printable report; also sets-csv/xlsx/json — plain csv is empty, it's parts-only)")
	cmd.Flags().StringVarP(&outPath, "out", "o", "", "write to this file (default: print it)")
	cmd.Flags().BoolVar(&force, "force", false, "replace the output file if it exists")
	discord.register(cmd.Flags())
	return cmd
}

// newLegoReportSetPartsCmd is "Set (ID) Parts Lists".
func newLegoReportSetPartsCmd() *cobra.Command {
	var format, outPath string
	var force bool
	var discord discordFlags
	cmd := &cobra.Command{
		Use:     "setparts <set...>",
		Short:   "Set (ID) Parts Lists: one or more sets' full parts lists, side by side",
		Example: "  wms lego report setparts 75192 -o falcon.html\n  wms lego report setparts 75192 10230 -o both.html",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			t := ui.New()
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			data, err := db.SetPartsReport(args)
			if err != nil {
				return err
			}
			return writeReport(t, data, format, outPath, force, discord)
		},
	}
	cmd.Flags().StringVar(&format, "format", "", "output format (default: the printable report; also csv/xlsx/json/html/sorting-html)")
	cmd.Flags().StringVarP(&outPath, "out", "o", "", "write to this file (default: print it)")
	cmd.Flags().BoolVar(&force, "force", false, "replace the output file if it exists")
	discord.register(cmd.Flags())
	return cmd
}

func newLegoReportHistoryCmd() *cobra.Command {
	var outPath string
	var force bool
	var limit int
	cmd := &cobra.Command{
		Use:     "history [set]",
		Short:   "Past stock checks, newest first — for one set, or across the collection",
		Example: "  wms lego report history\n  wms lego report history 75192 -o falcon-history.html",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			t := ui.New()
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			set := ""
			if len(args) == 1 {
				set = args[0]
			}
			hist, err := db.CheckHistory(set, limit)
			if err != nil {
				return err
			}
			if outPath != "" {
				body, err := lego.CheckHistoryHTML("Check history", hist)
				if err != nil {
					return err
				}
				abs, err := writeExportFile(outPath, body, force)
				if err != nil {
					return err
				}
				say(ui.Status(t, true, fmt.Sprintf("Wrote %s (%d check(s))", abs, len(hist))))
				return emit(map[string]any{"file": abs, "checks": len(hist)})
			}
			rows := make([][]string, len(hist))
			for i, c := range hist {
				result := fmt.Sprintf("%d missing", c.Missing)
				if c.Missing == 0 {
					result = "COMPLETE"
				}
				rows[i] = []string{c.FinishedAt.Format("2 Jan 2006 15:04"), c.SetNum, c.Kind, c.CheckedBy, strconv.Itoa(c.Pieces), strconv.Itoa(c.Have), result}
			}
			if len(rows) > 0 {
				say(ui.RenderColumns(t, []string{"Date", "Set", "Kind", "By", "Pieces", "Have", "Result"}, rows, fmt.Sprintf("%d check(s)", len(hist))))
			}
			summaries := make([]map[string]any, len(hist))
			for i, c := range hist {
				summaries[i] = map[string]any{"set": c.SetNum, "kind": c.Kind, "by": c.CheckedBy, "finished_at": c.FinishedAt, "pieces": c.Pieces, "have": c.Have, "missing": c.Missing, "extra": c.Extra}
			}
			return emit(map[string]any{"checks": summaries})
		},
	}
	cmd.Flags().StringVarP(&outPath, "out", "o", "", "write a printable page instead of a terminal table")
	cmd.Flags().BoolVar(&force, "force", false, "replace the output file if it exists")
	cmd.Flags().IntVar(&limit, "limit", 50, "how many checks to include")
	return cmd
}

// newLegoReportExtraCmd is "Extra Parts": spares from completed checks.
func newLegoReportExtraCmd() *cobra.Command {
	var format, outPath string
	var force bool
	var discord discordFlags
	cmd := &cobra.Command{
		Use:     "extra [set...]",
		Short:   "Extra Parts: spares left over from completed set checks",
		Example: "  wms lego report extra -o extras.html\n  wms lego report extra 75192 -o falcon-extras.html",
		Args:    cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			t := ui.New()
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			data, err := db.ExtraPartsReport(args)
			if err != nil {
				return err
			}
			return writeReport(t, data, format, outPath, force, discord)
		},
	}
	cmd.Flags().StringVar(&format, "format", "", "output format (default: the printable report; also csv/xlsx/json/html/sorting-html)")
	cmd.Flags().StringVarP(&outPath, "out", "o", "", "write to this file (default: print it)")
	cmd.Flags().BoolVar(&force, "force", false, "replace the output file if it exists")
	discord.register(cmd.Flags())
	return cmd
}

// newLegoReportOrdersCmd is "Order List".
func newLegoReportOrdersCmd() *cobra.Command {
	var format, outPath string
	var force, openOnly bool
	var discord discordFlags
	cmd := &cobra.Command{
		Use:     "orders",
		Short:   "Order List: every parts order, most recent first",
		Example: "  wms lego report orders -o orders.html\n  wms lego report orders --open -o open-orders.html",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			t := ui.New()
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			status := ""
			if openOnly {
				status = "open"
			}
			data, err := db.OrderListReport(status)
			if err != nil {
				return err
			}
			return writeReport(t, data, format, outPath, force, discord)
		},
	}
	cmd.Flags().StringVar(&format, "format", "", "output format (default: the printable report; also csv/xlsx/json/html/sorting-html)")
	cmd.Flags().StringVarP(&outPath, "out", "o", "", "write to this file (default: print it)")
	cmd.Flags().BoolVar(&force, "force", false, "replace the output file if it exists")
	cmd.Flags().BoolVar(&openOnly, "open", false, "only orders not yet received or cancelled")
	discord.register(cmd.Flags())
	return cmd
}

// newLegoReportStocktakeCmd is "Parts Stock-take": a blank printable checklist.
func newLegoReportStocktakeCmd() *cobra.Command {
	var outPath string
	var force bool
	var discord discordFlags
	cmd := &cobra.Command{
		Use:     "stocktake <set...>",
		Short:   "Parts Stock-take: a blank printable checklist for counting a set by hand",
		Example: "  wms lego report stocktake 75192 -o falcon-checklist.html",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			t := ui.New()
			db, rb, err := openLegoWithClient()
			if err != nil {
				return err
			}
			defer db.Close()
			for _, set := range args {
				title, lines, err := db.StockSheet(cmd.Context(), rb, set)
				if err != nil {
					return err
				}
				body, err := lego.StockSheetHTML(title, lines)
				if err != nil {
					return err
				}
				out := outPath
				if out == "" {
					out = set + "-stocksheet.html"
				} else if len(args) > 1 {
					return usageError("-o only works with one set at a time; pass sets one by one, or omit -o to name files after the set")
				}
				abs, err := writeExportFile(out, body, force)
				if err != nil {
					return err
				}
				say(ui.Status(t, true, fmt.Sprintf("Wrote %s (%d line(s))", abs, len(lines))))
				if err := sendToDiscord(t, discord, abs, "stock sheet for "+title); err != nil {
					return err
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&outPath, "out", "o", "", "output file (default: <set>-stocksheet.html; only with one set)")
	cmd.Flags().BoolVar(&force, "force", false, "replace the output file if it exists")
	discord.register(cmd.Flags())
	return cmd
}
