package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/exports"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/uiapp"
)

// writeReport renders data as the clean printable report (default) or, with an
// explicit --format, whatever lego.Encode supports (csv/xlsx/json/plain html/...).
// Every report generated is also archived (see archiveReport) — best-effort, since
// it wasn't explicitly requested the way --discord was, so a failure only warns.
func writeReport(t ui.Theme, db *lego.DB, kind string, data *lego.ExportData, format, outPath string, force bool, discord discordFlags) error {
	if format == "" {
		format = "report"
	}
	body, ext, _, err := lego.Encode(format, data)
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
	archiveReport(t, db, kind, data.Title, ext, body)
	if err := sendToDiscord(t, discord, abs, data.Title); err != nil {
		return err
	}
	return emit(map[string]any{"file": abs, "rows": len(data.Rows), "sets": len(data.Sets), "facts": data.Facts})
}

// archiveReportMaxAge is how long an archived report survives — long compared to a
// normal export's 7-day default, since the whole point is outliving that.
const archiveReportMaxAge = 90 * 24 * time.Hour

// archiveReport saves a permanent-ish copy for `wms lego report archive` — best-effort,
// never fails the report itself — and opportunistically sweeps anything past
// archiveReportMaxAge (there's no scheduler for this; piggybacking on normal use is
// enough for something with a 90-day horizon).
func archiveReport(t ui.Theme, db *lego.DB, kind, title, ext string, body []byte) {
	dir := config.Get(config.ArchiveDir)
	if _, err := db.ArchiveReport(dir, kind, title, cliActor(), ext, body); err != nil {
		say(ui.Warn(t, "could not archive this report: "+err.Error()))
	}
	_, _ = db.CleanupArchive(dir, archiveReportMaxAge)
}

func newLegoReportCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "report", Short: "Printable reports: stock-take, parts lists, missing/extra parts, sets, orders, check history"}
	cmd.AddCommand(newLegoReportStocktakeCmd(), newLegoReportSetPartsCmd(), newLegoReportSetListCmd(),
		newLegoReportMissingCmd(), newLegoReportExtraCmd(), newLegoReportOrdersCmd(), newLegoReportHistoryCmd(),
		newLegoReportArchiveCmd())
	return cmd
}

// newLegoReportArchiveCmd browses reports every `report`/`stocktake` command already
// saved a permanent-ish copy of (see archiveReport) — for "the QR/link expired, can I
// still get that report" without having to regenerate it.
func newLegoReportArchiveCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "archive", Short: "Browse and re-fetch previously generated reports"}
	cmd.AddCommand(newLegoReportArchiveListCmd(), newLegoReportArchiveGetCmd())
	return cmd
}

func newLegoReportArchiveListCmd() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List archived reports (yours, unless --all)",
		Example: "  wms lego report archive list\n  wms lego report archive list --all",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			t := ui.New()
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			list, err := db.ListArchive(cliActor(), all)
			if err != nil {
				return err
			}
			rows := make([][]string, len(list))
			for i, e := range list {
				rows[i] = []string{strconv.FormatInt(e.ID, 10), e.Kind, e.Title, e.CreatedBy, e.CreatedAt.Format("2 Jan 2006 15:04")}
			}
			if len(rows) > 0 {
				say(ui.RenderColumns(t, []string{"ID", "Kind", "Title", "By", "Created"}, rows, fmt.Sprintf("%d archived report(s)", len(list))))
			} else {
				say(ui.Warn(t, "Nothing archived yet — every report you generate is archived automatically."))
			}
			return emit(map[string]any{"archived": list})
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "everyone's archived reports, not just yours")
	return cmd
}

func newLegoReportArchiveGetCmd() *cobra.Command {
	var expires time.Duration
	cmd := &cobra.Command{
		Use:     "get <id>",
		Short:   "Mint a fresh share link (and QR) for an archived report",
		Example: "  wms lego report archive get 12",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			t := ui.New()
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return usageError("the id is the number in `wms lego report archive list`")
			}
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			e, err := db.GetArchiveEntry(id)
			if err != nil {
				return withCode(exitNotFound, fmt.Errorf("no archived report %d", id))
			}
			if e.CreatedBy != cliActor() {
				return withCode(exitAuth, fmt.Errorf("report %d belongs to %s, not you", id, e.CreatedBy))
			}
			if exports.URL("x") == "" {
				return usageError("this needs WMS_PUBLIC_URL set, so the link it makes is reachable")
			}
			// /share/ only ever looks in WMS_EXPORT_DIR, not the archive directory — a short-lived
			// copy there is what actually gets served; the archive entry itself is untouched.
			body, err := os.ReadFile(filepath.Join(config.Get(config.ArchiveDir), e.File))
			if err != nil {
				return err
			}
			ext := strings.TrimPrefix(filepath.Ext(e.File), ".")
			copyPath, err := exports.Save(exports.Dir(), cliActor(), e.Kind, "", ext, body)
			if err != nil {
				return err
			}
			token, err := exports.NewShareLink(exports.Dir(), copyPath, cliActor(), expires)
			if err != nil {
				return err
			}
			link := exports.ShareURL(token)
			say(ui.Status(t, true, fmt.Sprintf("%s — %s, expires %s", e.Title, e.Kind, time.Now().Add(expires).Format("2 Jan 2006 15:04"))))
			say(t.Accent.Render(link))
			return emit(map[string]any{"id": e.ID, "title": e.Title, "url": link})
		},
	}
	cmd.Flags().DurationVar(&expires, "expires", 24*time.Hour, "how long the link stays viewable")
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
			return writeReport(t, db, "missing-report", data, format, outPath, force, discord)
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
			return writeReport(t, db, "setlist-report", data, format, outPath, force, discord)
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
			return writeReport(t, db, "setparts-report", data, format, outPath, force, discord)
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
				archiveReport(t, db, "history-report", "Check history", "html", body)
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
			return writeReport(t, db, "extra-report", data, format, outPath, force, discord)
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
			return writeReport(t, db, "orders-report", data, format, outPath, force, discord)
		},
	}
	cmd.Flags().StringVar(&format, "format", "", "output format (default: the printable report; also csv/xlsx/json/html/sorting-html)")
	cmd.Flags().StringVarP(&outPath, "out", "o", "", "write to this file (default: print it)")
	cmd.Flags().BoolVar(&force, "force", false, "replace the output file if it exists")
	cmd.Flags().BoolVar(&openOnly, "open", false, "only orders not yet received or cancelled")
	discord.register(cmd.Flags())
	return cmd
}

// newLegoHelpSheetCmd is a printable cheat sheet of every key the TUI's F1 help
// already shows — same content (uiapp.CheatSheetSections), just on paper.
func newLegoHelpSheetCmd() *cobra.Command {
	var outPath string
	var force bool
	var discord discordFlags
	cmd := &cobra.Command{
		Use:     "help-sheet",
		Short:   "Printable cheat sheet of the TUI/telnet key bindings",
		Example: "  wms lego help-sheet -o keys.html",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			t := ui.New()
			body, err := lego.CheatSheetHTML("Key cheat sheet", uiapp.CheatSheetSections())
			if err != nil {
				return err
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
			say(ui.Status(t, true, "Wrote "+abs))
			db, err := openLego()
			if err == nil {
				defer db.Close()
				archiveReport(t, db, "help-sheet", "Key cheat sheet", "html", body)
			}
			if err := sendToDiscord(t, discord, abs, "key cheat sheet"); err != nil {
				return err
			}
			return emit(map[string]any{"file": abs})
		},
	}
	cmd.Flags().StringVarP(&outPath, "out", "o", "", "write to this file (default: print it)")
	cmd.Flags().BoolVar(&force, "force", false, "replace the output file if it exists")
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
					out = set + "-stocktake.html"
				} else if len(args) > 1 {
					return usageError("-o only works with one set at a time; pass sets one by one, or omit -o to name files after the set")
				}
				abs, err := writeExportFile(out, body, force)
				if err != nil {
					return err
				}
				say(ui.Status(t, true, fmt.Sprintf("Wrote %s (%d line(s))", abs, len(lines))))
				archiveReport(t, db, "stocktake", title, "html", body)
				if err := sendToDiscord(t, discord, abs, "stock-take checklist for "+title); err != nil {
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
