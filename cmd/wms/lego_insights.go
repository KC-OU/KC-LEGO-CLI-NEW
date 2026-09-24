package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/backup"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/bricklink"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui/img"
)

const maxImportBytes = 20 << 20

// openLego opens the LEGO store and records who is acting (WMS_USER, else "cli") in the journal.
func openLego() (*lego.DB, error) {
	db, err := lego.Open("")
	if err != nil {
		return nil, err
	}
	db.SetActor("cli:" + envOr("WMS_USER", "cli"))
	return db, nil
}

func openLegoWithClient() (*lego.DB, *lego.Client, error) {
	db, err := openLego()
	if err != nil {
		return nil, nil, err
	}
	return db, lego.NewClientFor(db), nil
}

func needKey(c *lego.Client, why string) error {
	if !c.Enabled() {
		return withCode(exitAuth, fmt.Errorf("%s needs a Rebrickable API key (Admin > Settings & API Keys)", why))
	}
	return nil
}

func lowRows(low []lego.OwnedPart) [][]string {
	rows := make([][]string, len(low))
	for i, p := range low {
		rows[i] = []string{p.PartNum, orDash(p.ColorName), p.Name, strconv.Itoa(p.Qty), strconv.Itoa(p.MinQty), strconv.Itoa(p.MinQty - p.Qty)}
	}
	return rows
}

// lowStockLines is what the alert monitor reports as low stock.
func lowStockLines() []string {
	db, err := openLego()
	if err != nil {
		return nil
	}
	defer db.Close()
	low, _ := db.LowStock()
	lines := make([]string, len(low))
	for i, p := range low {
		lines[i] = fmt.Sprintf("%s %s: %d (min %d)", p.PartNum, orDash(p.ColorName), p.Qty, p.MinQty)
	}
	return lines
}

func newLegoLowCmd() *cobra.Command {
	var fail bool
	cmd := &cobra.Command{
		Use:   "low",
		Short: "List the parts that are below the minimum you set",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			t := ui.New()
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			low, err := db.LowStock()
			if err != nil {
				return err
			}
			if len(low) == 0 {
				say(ui.Status(t, true, "Nothing is below its minimum."))
			} else {
				say(ui.RenderColumns(t, []string{"Part #", "Colour", "Name", "Have", "Min", "Short"}, lowRows(low), fmt.Sprintf("%d part(s) LOW", len(low))))
			}
			items := make([]map[string]any, len(low))
			for i, p := range low {
				items[i] = map[string]any{"part": p.PartNum, "colour": p.ColorName, "name": p.Name, "have": p.Qty, "min": p.MinQty, "short": p.MinQty - p.Qty}
			}
			if err := emit(map[string]any{"low": items}); err != nil {
				return err
			}
			if fail && len(low) > 0 {
				return withCode(exitFailure, fmt.Errorf("%d part(s) below their minimum", len(low)))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&fail, "fail", false, "exit 1 when anything is low (for cron and monitors)")
	return cmd
}

func newLegoSetMinCmd() *cobra.Command {
	var color string
	var min int
	cmd := &cobra.Command{
		Use:               "set-min <part_num>",
		Short:             "Set the stock level below which a part counts as low (0 stops tracking it)",
		Example:           "  wms lego set-min 3001 --color red --min 20",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completePartNum,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			if !cmd.Flags().Changed("min") {
				return usageError("--min is required (0 stops tracking)")
			}
			t := ui.New()
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			owned, err := db.OwnedPartsOf(args[0])
			if err != nil {
				return err
			}
			if len(owned) == 0 {
				return withCode(exitNotFound, fmt.Errorf("you do not hold part %s", args[0]))
			}
			row := owned[0]
			if len(owned) > 1 || color != "" {
				cols := make([]lego.Color, len(owned))
				for i, p := range owned {
					cols[i] = lego.Color{ID: p.ColorID, Name: p.ColorName}
				}
				c, ok := lego.MatchColor(cols, color)
				if !ok {
					names := make([]string, len(owned))
					for i, p := range owned {
						names[i] = orDash(p.ColorName)
					}
					return usageError("--color must be one of the colours you hold %s in: %s", args[0], strings.Join(names, ", "))
				}
				for _, p := range owned {
					if p.ColorID == c.ID && (p.ColorID >= 0 || p.ColorName == c.Name) {
						row = p
					}
				}
			}
			if err := db.SetMinQty(row.PartNum, row.ColorID, row.ColorName, min); err != nil {
				return err
			}
			say(ui.Status(t, true, fmt.Sprintf("%s %s: warn below %d (you hold %d)", row.PartNum, orDash(row.ColorName), min, row.Qty)))
			say(t.Muted.Render("   Part-DB's minimum follows the next `wms lego sync-parts`."))
			return emit(map[string]any{"part": row.PartNum, "colour": row.ColorName, "min": min, "have": row.Qty})
		},
	}
	cmd.Flags().StringVar(&color, "color", "", "the colour, if you hold the part in several")
	cmd.Flags().IntVar(&min, "min", 0, "warn when the quantity falls below this (0 = don't track)")
	return cmd
}

// newLegoOptionalCmd marks a part number as optional (or required again): it stops
// (or resumes) counting toward missing-parts totals and completion, in every set
// that uses it. Stickers default to optional already (see lego.IsOptional); this is
// for overriding that default, in either direction, for any part.
func newLegoOptionalCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "optional <part_num> [on|off]",
		Short:             "Mark a part optional, so it never counts as missing (default: on for stickers)",
		Example:           "  wms lego optional 78256\n  wms lego optional 78256 on\n  wms lego optional 78256 off",
		Args:              cobra.RangeArgs(1, 2),
		ValidArgsFunction: completePartNum,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			t := ui.New()
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			partNum := args[0]
			word := map[bool]string{true: "optional", false: "required"}
			if len(args) == 1 {
				cat := ""
				if cp, _ := db.CatalogPart(partNum); cp != nil {
					cat = cp.Category
				}
				optional, err := db.IsOptional(partNum, cat)
				if err != nil {
					return err
				}
				say(fmt.Sprintf("%s is currently %s.", partNum, word[optional]))
				return emit(map[string]any{"part": partNum, "optional": optional})
			}
			on := args[1] == "on"
			if !on && args[1] != "off" {
				return usageError("say `on` or `off`")
			}
			if err := db.SetOptional(partNum, on); err != nil {
				return err
			}
			say(ui.Status(t, true, fmt.Sprintf("%s marked %s.", partNum, word[on])))
			return emit(map[string]any{"part": partNum, "optional": on})
		},
	}
	return cmd
}

func newLegoStatsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stats",
		Short: "The collection at a glance: sets, pieces, colours, categories, low stock",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			t := ui.New()
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			s, err := db.Stats()
			if err != nil {
				return err
			}
			list := func(cs []lego.Count) string {
				parts := make([]string, len(cs))
				for i, c := range cs {
					parts[i] = fmt.Sprintf("%s %d", c.Name, c.N)
				}
				return orDash(strings.Join(parts, ", "))
			}
			say(ui.Fact(t, "Sets", fmt.Sprintf("%d title(s), %d cop(ies), %d parted out", s.SetTitles, s.SetCopies, s.PartedOut)))
			say(ui.Fact(t, "Pieces in sets", strconv.Itoa(s.SetPieces)))
			say(ui.Fact(t, "Loose parts", fmt.Sprintf("%d piece(s), %d part+colour line(s), %d distinct part(s)", s.LoosePieces, s.PartLines, s.DistinctParts)))
			say(ui.Fact(t, "Below minimum", strconv.Itoa(s.LowStock)))
			say(ui.Fact(t, "Sets by theme", list(s.Themes)))
			say(ui.Fact(t, "Pieces by colour", list(s.Colours)))
			say(ui.Fact(t, "Pieces by category", list(s.Categories)))
			return emit(s)
		},
	}
}

// inventoryReport compares a set's parts list (offline catalog first, live Rebrickable if it
// lacks the set) with what you hold. source says where the parts list came from.
func inventoryReport(ctx context.Context, db *lego.DB, rb *lego.Client, set string, copies int, eq lego.Equivalents) (rep *lego.MissingReport, source string, err error) {
	inv := db.LookupSetInventory(ctx, rb, set)
	if len(inv.Items) == 0 {
		msg := strings.Join(inv.Notes, " ")
		if strings.Contains(msg, "has no set") {
			return nil, "", withCode(exitNotFound, errors.New(msg))
		}
		if strings.Contains(msg, "no offline catalog") { // nothing to look in and no key: a setup problem
			return nil, "", withCode(exitAuth, errors.New(msg))
		}
		if strings.Contains(msg, "unreachable") {
			return nil, "", withCode(exitNetwork, errors.New(msg))
		}
		return nil, "", withCode(exitNotFound, errors.New(msg))
	}
	rep, err = db.MissingForWith(inv.Items, copies, eq)
	return rep, inv.Source, err
}

func newLegoMissingCmd() *cobra.Command {
	var copies int
	var all bool
	var equiv string
	cmd := &cobra.Command{
		Use:   "missing <set_num>",
		Short: "What a set still needs, compared with the loose parts you hold (part + colour)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			t := ui.New()
			db, rb, err := openLegoWithClient()
			if err != nil {
				return err
			}
			defer db.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			eq, err := equivFlag(equiv)
			if err != nil {
				return err
			}
			r, source, err := inventoryReport(ctx, db, rb, args[0], copies, eq)
			if err != nil {
				return err
			}
			say(ui.Fact(t, "Set", fmt.Sprintf("%s x%d (parts list: %s)", args[0], r.Copies, source)))
			say(ui.Fact(t, "Counting", eq.Label()))
			if r.SubstitutedPieces > 0 {
				say(ui.Warn(t, fmt.Sprintf("%d piece(s) are held as equivalent parts (alternates/moulds), not the exact number: check the 'Covered by' column.", r.SubstitutedPieces)))
			}
			say(ui.Fact(t, "You hold", fmt.Sprintf("%d%% (%d of %d pieces); %d of %d lines complete", r.Percent(), r.PiecesHeld, r.PiecesNeeded, r.Complete, r.Lines)))
			shown := r.Missing
			if !all && len(shown) > 40 {
				shown = shown[:40]
			}
			rows := make([][]string, len(shown))
			for i, m := range shown {
				rows[i] = []string{m.PartNum, orDash(m.ColorName), m.PartName, strconv.Itoa(m.Need), strconv.Itoa(m.Have), strconv.Itoa(m.Short), orDash(strings.Join(m.Substituted, ", "))}
			}
			if len(rows) > 0 {
				say(ui.RenderColumns(t, []string{"Part #", "Colour", "Name", "Need", "Have", "Short", "Covered by"}, rows, fmt.Sprintf("%d line(s) missing", len(r.Missing))))
				if len(shown) < len(r.Missing) {
					say(t.Muted.Render(fmt.Sprintf("   … and %d more (--all shows everything)", len(r.Missing)-len(shown))))
				}
			}
			missing := make([]map[string]any, len(r.Missing))
			for i, m := range r.Missing {
				missing[i] = map[string]any{"part": m.PartNum, "colour": m.ColorName, "colour_id": m.ColorID, "name": m.PartName, "need": m.Need, "have": m.Have, "short": m.Short, "covered_by": m.Substituted}
			}
			return emit(map[string]any{"set": args[0], "equivalents": string(eq), "substituted_pieces": r.SubstitutedPieces, "copies": r.Copies, "percent": r.Percent(), "pieces_needed": r.PiecesNeeded, "pieces_held": r.PiecesHeld,
				"lines": r.Lines, "lines_complete": r.Complete, "source": source, "missing": missing})
		},
	}
	cmd.Flags().IntVar(&copies, "copies", 1, "how many copies you want to build")
	cmd.Flags().BoolVar(&all, "all", false, "list every missing line, not just the first 40")
	cmd.Flags().StringVar(&equiv, "equivalents", "", "parts that count as the same: default (alternates+moulds), none, or alt,mold,print (setting: WMS_EQUIVALENTS)")
	return cmd
}

func newLegoWantedCmd() *cobra.Command {
	var set, outPath string
	var copies int
	var low, force bool
	cmd := &cobra.Command{
		Use:   "wanted (--set <num> | --low) [-o file.xml]",
		Short: "Write a BrickLink wanted list (XML) for a set's missing parts or your low-stock parts",
		Long: "Upload the file at BrickLink: Wanted > Upload Wanted List Items. BrickLink part and colour numbers come\n" +
			"from Rebrickable; where it lists none the Rebrickable part number is used (it usually matches) or the\n" +
			"colour is left out — the summary counts both so you know what to check.",
		Example: "  wms lego wanted --set 75192 -o falcon.xml\n  wms lego wanted --low -o restock.xml",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			t := ui.New()
			if (set == "") == !low {
				return usageError("give exactly one of --set <num> or --low")
			}
			if out.JSON && outPath == "" {
				return usageError("--json prints a summary, so the XML needs -o <file>")
			}
			db, rb, err := openLegoWithClient()
			if err != nil {
				return err
			}
			defer db.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()

			var items []lego.WantedItem
			var noPart, noColor int
			what := ""
			if set != "" {
				r, _, err := inventoryReport(ctx, db, rb, set, copies, lego.EquivalentsFromConfig())
				if err != nil {
					return err
				}
				items, noPart, noColor = lego.WantedFromMissing(r)
				what = fmt.Sprintf("set %s x%d", set, r.Copies)
			} else {
				lowParts, err := db.LowStock()
				if err != nil {
					return err
				}
				items, noPart, noColor = wantedFromLow(ctx, rb, lowParts)
				what = "low-stock parts"
			}
			if len(items) == 0 {
				say(ui.Status(t, true, "Nothing to want: no missing parts for "+what))
				return emit(map[string]any{"items": 0})
			}
			xmlBytes, err := lego.WantedXML(items)
			if err != nil {
				return err
			}
			file := outPath
			if file == "" {
				fmt.Print(string(xmlBytes))
			} else {
				flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
				if force {
					flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
				}
				f, err := os.OpenFile(file, flags, 0644)
				if err != nil {
					if errors.Is(err, os.ErrExist) {
						return usageError("%s already exists — use --force to replace it", file)
					}
					return err
				}
				_, werr := f.Write(xmlBytes)
				if cerr := f.Close(); werr == nil {
					werr = cerr
				}
				if werr != nil {
					return werr
				}
			}
			abs := file
			if file != "" {
				abs, _ = filepath.Abs(file)
			}
			say(ui.Status(t, true, fmt.Sprintf("Wanted list for %s: %d line(s)", what, len(items))))
			if abs != "" {
				say(ui.Fact(t, "File", abs))
			}
			if noPart > 0 {
				say(ui.Warn(t, fmt.Sprintf("%d line(s) have no BrickLink part number from Rebrickable; the Rebrickable number was used — check them.", noPart)))
			}
			if noColor > 0 {
				say(ui.Warn(t, fmt.Sprintf("%d line(s) have no BrickLink colour, so the colour is left out.", noColor)))
			}
			return emit(map[string]any{"items": len(items), "file": abs, "no_bricklink_part_id": noPart, "no_bricklink_colour": noColor})
		},
	}
	cmd.Flags().StringVar(&set, "set", "", "the set whose missing parts to list")
	cmd.Flags().IntVar(&copies, "copies", 1, "copies of the set you want to build")
	cmd.Flags().BoolVar(&low, "low", false, "list your low-stock parts instead")
	cmd.Flags().StringVarP(&outPath, "out", "o", "", "write the XML to this file (default: print it)")
	cmd.Flags().BoolVar(&force, "force", false, "replace the output file if it exists")
	return cmd
}

// wantedFromLow builds a wanted list for parts below their minimum. BrickLink
// numbers come from Rebrickable when a key is set (cached, so cheap after the
// first time); otherwise Rebrickable's own part number is used.
func wantedFromLow(ctx context.Context, rb *lego.Client, low []lego.OwnedPart) (items []lego.WantedItem, noPart, noColor int) {
	blColor := map[int]int{}
	if rb.Enabled() {
		if all, err := rb.AllColors(ctx); err == nil {
			for _, c := range all {
				blColor[c.ID] = c.BrickLinkID()
			}
		}
	}
	for _, p := range low {
		id := ""
		if rb.Enabled() {
			if pd, err := rb.GetPart(ctx, p.PartNum); err == nil {
				id = pd.ExternalID("BrickLink")
			}
		}
		if id == "" {
			id, noPart = p.PartNum, noPart+1
		}
		col := 0
		if p.ColorID >= 0 {
			col = blColor[p.ColorID]
		}
		if col == 0 {
			noColor++
		}
		items = append(items, lego.WantedItem{ItemID: id, Color: col, MinQty: p.MinQty - p.Qty})
	}
	return
}

func newLegoImportPartsCmd() *cobra.Command {
	var format, mode string
	var dryRun, yes bool
	cmd := &cobra.Command{
		Use:   "import-parts <file>",
		Short: "Import a parts list: a Rebrickable CSV or a BrickLink inventory XML (--dry-run shows the plan first)",
		Long: "Reads Part / Color / Quantity from a Rebrickable parts-list CSV, or ITEM entries from a BrickLink XML\n" +
			"(parts only; BrickLink colour numbers are translated through Rebrickable, which needs a key). Quantities\n" +
			"are added to what you hold (--mode add) or replace it (--mode set). Names and categories come from the\n" +
			"offline catalog. Everything is written in one transaction. Nothing goes to Part-DB until you run\n" +
			"`wms lego sync-parts`.",
		Example: "  wms lego import-parts my-parts.csv --dry-run\n  wms lego import-parts inventory.xml --mode set --yes",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			t := ui.New()
			fi, err := os.Stat(args[0])
			if err != nil {
				return withCode(exitNotFound, err)
			}
			if fi.Size() > maxImportBytes {
				return usageError("%s is larger than %d MB — split it", args[0], maxImportBytes>>20)
			}
			if format == "auto" {
				b, _ := os.ReadFile(args[0])
				if strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(string(b), "\ufeff")), "<") {
					format = "bricklink"
				} else {
					format = "rebrickable"
				}
			}
			if format != "rebrickable" && format != "bricklink" {
				return usageError("--format must be auto, rebrickable or bricklink")
			}
			db, rb, err := openLegoWithClient()
			if err != nil {
				return err
			}
			defer db.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()

			cols, _ := db.CatalogColors()
			table := lego.NewColorTable(cols)
			if format == "bricklink" || len(cols) == 0 {
				if err := needKey(rb, "translating colours"); err != nil {
					if format == "bricklink" {
						return err
					}
				} else if all, err := rb.AllColors(ctx); err == nil {
					if len(cols) == 0 {
						for _, c := range all {
							table.ByID[c.ID] = lego.Color{ID: c.ID, Name: c.Name, RGB: c.RGB, Trans: c.IsTrans}
							table.ByName[strings.ToLower(c.Name)] = table.ByID[c.ID]
						}
					}
					table.AddBrickLink(all)
				} else if format == "bricklink" {
					return withCode(exitNetwork, fmt.Errorf("could not get Rebrickable's colour table: %w", err))
				}
			}

			f, err := os.Open(args[0])
			if err != nil {
				return err
			}
			defer f.Close()
			var rows []lego.ImportRow
			var problems []string
			if format == "bricklink" {
				rows, problems, err = lego.ParseBrickLinkXML(f, table)
			} else {
				rows, problems, err = lego.ParseRebrickableCSV(f, table)
			}
			if err != nil {
				return usageError("%v", err)
			}
			plan, err := db.PlanImport(rows, mode)
			if err != nil {
				return usageError("%v", err)
			}

			say(ui.Fact(t, "File", fmt.Sprintf("%s (%s, %d line(s) read)", args[0], format, len(rows))))
			say(ui.Fact(t, "Plan", fmt.Sprintf("%d new, %d changed, %d unchanged (mode %s)", plan.New, plan.Changed, plan.Unchanged, plan.Mode)))
			if plan.Unknown > 0 {
				say(ui.Warn(t, fmt.Sprintf("%d part(s) are not in the offline catalog and keep their number as their name (`wms lego catalog refresh` may help).", plan.Unknown)))
			}
			for i, p := range problems {
				if i == 10 {
					say(t.Muted.Render(fmt.Sprintf("   … and %d more problem(s)", len(problems)-10)))
					break
				}
				say(ui.Warn(t, p))
			}
			preview := plan.Items
			if len(preview) > 15 {
				preview = preview[:15]
			}
			prows := make([][]string, len(preview))
			for i, it := range preview {
				prows[i] = []string{it.PartNum, orDash(it.ColorName), it.Name, strconv.Itoa(it.Was), strconv.Itoa(it.Becomes)}
			}
			if len(prows) > 0 {
				say(ui.RenderColumns(t, []string{"Part #", "Colour", "Name", "Now", "After"}, prows, "First lines of the plan"))
			}
			jsonPlan := map[string]any{"format": format, "mode": plan.Mode, "new": plan.New, "changed": plan.Changed, "unchanged": plan.Unchanged, "unknown_parts": plan.Unknown, "problems": problems}
			if dryRun {
				say(ui.Warn(t, "--dry-run: nothing was changed."))
				jsonPlan["dry_run"] = true
				return emit(jsonPlan)
			}
			if plan.New+plan.Changed == 0 {
				say(ui.Status(t, true, "Nothing to change."))
				return emit(jsonPlan)
			}
			if err := confirm(fmt.Sprintf("Write %d new and %d changed part line(s)?", plan.New, plan.Changed), yes); err != nil {
				return err
			}
			if err := db.ApplyImport(plan); err != nil {
				return err
			}
			say(ui.Status(t, true, fmt.Sprintf("Imported: %d new, %d changed. Run `wms lego sync-parts` to send them to Part-DB.", plan.New, plan.Changed)))
			jsonPlan["applied"] = true
			return emit(jsonPlan)
		},
	}
	cmd.Flags().StringVar(&format, "format", "auto", "auto, rebrickable (CSV) or bricklink (XML)")
	cmd.Flags().StringVar(&mode, "mode", "add", "add (add to what you hold) or set (replace it)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show the plan and stop")
	cmd.Flags().BoolVar(&yes, "yes", false, "don't ask for confirmation")
	return cmd
}

func newLegoSearchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search",
		Short: "Search parts, sets and minifigures — offline catalog first, live Rebrickable only if it has nothing",
		Long: "Search works with no internet and no API key once `wms lego catalog refresh` has run. Each result says\n" +
			"where it came from. Use quotes for phrases; words match in any order and by prefix.",
		Example: "  wms lego search sets falcon\n  wms lego search parts \"brick 2 x 4\"\n  wms lego search minifigs toy store",
	}
	find := func(kind string) *cobra.Command {
		return &cobra.Command{
			Use:   kind + " <words...>",
			Short: "Search " + kind,
			Args:  cobra.MinimumNArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				cmd.SilenceUsage = true
				t := ui.New()
				term := strings.Join(args, " ")
				db, rb, err := openLegoWithClient()
				if err != nil {
					return err
				}
				defer db.Close()
				ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
				defer cancel()
				var source string
				var notes []string
				var rows [][]string
				var list []map[string]any
				var cols []string
				switch kind {
				case "parts":
					r := db.FindParts(ctx, rb, term)
					source, notes, cols = r.Source, r.Notes, []string{"Part #", "Name", "Category"}
					for _, h := range r.Hits {
						rows = append(rows, []string{h.Num, h.Name, orDash(h.Category)})
						list = append(list, map[string]any{"part": h.Num, "name": h.Name, "category": h.Category})
					}
				case "sets":
					r := db.FindSets(ctx, rb, term)
					source, notes, cols = r.Source, r.Notes, []string{"Set #", "Name", "Theme", "Year", "Pieces"}
					for _, h := range r.Hits {
						rows = append(rows, []string{h.Num, h.Name, orDash(h.Theme), strconv.Itoa(h.Year), strconv.Itoa(h.Pieces)})
						list = append(list, map[string]any{"set": h.Num, "name": h.Name, "theme": h.Theme, "year": h.Year, "pieces": h.Pieces})
					}
				default:
					hits, err := db.SearchCatalogMinifigs(term, 50)
					if err != nil {
						return err
					}
					source, cols = db.CatalogLabel(), []string{"Figure #", "Name", "Pieces"}
					for _, h := range hits {
						rows = append(rows, []string{h.Num, h.Name, strconv.Itoa(h.Pieces)})
						list = append(list, map[string]any{"minifig": h.Num, "name": h.Name, "pieces": h.Pieces})
					}
					if len(hits) == 0 {
						source = "no match"
					}
				}
				for _, n := range notes {
					say(ui.Warn(t, n))
				}
				if len(rows) == 0 {
					say(ui.Status(t, false, fmt.Sprintf("Nothing matches %q (%s).", term, source)))
				} else {
					say(ui.RenderColumns(t, cols, rows, fmt.Sprintf("%d result(s) — %s", len(rows), source)))
				}
				if err := emit(map[string]any{"source": source, "notes": notes, "results": list}); err != nil {
					return err
				}
				if len(rows) == 0 {
					return withCode(exitNotFound, fmt.Errorf("no %s match %q", kind, term))
				}
				return nil
			},
		}
	}
	cmd.AddCommand(find("parts"), find("sets"), find("minifigs"))
	return cmd
}

func newLegoBackupCmd() *cobra.Command {
	var encrypt bool
	var passFile, dest string
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Back up your LEGO collection (sets, owned parts, history), without the re-downloadable catalog",
		Long: "Writes a consistent copy of lego.db using SQLite's VACUUM INTO, then drops the offline catalog, the API cache\n" +
			"and the image cache tables from the copy, so the file is small. The catalog comes back with\n" +
			"`wms lego catalog refresh`. --encrypt seals it with a passphrase exactly like `wms backup --encrypt`.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			t := ui.New()
			if dest == "" {
				dest = filepath.Join(config.Get(config.ModernWMSBackupDir), "lego")
			}
			var pass string
			if encrypt {
				var err error
				if pass, err = passphraseFor(passFile, true); err != nil {
					return err
				}
			}
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			path, err := db.BackupCollection(dest)
			if err != nil {
				return err
			}
			if encrypt {
				if path, err = backup.Seal(path, pass); err != nil {
					return err
				}
			}
			fi, _ := os.Stat(path)
			size := int64(0)
			if fi != nil {
				size = fi.Size()
			}
			say(ui.Status(t, true, "LEGO collection backed up"))
			say(ui.Fact(t, "File", path))
			say(ui.Fact(t, "Size", fmt.Sprintf("%.1f KB", float64(size)/1024)))
			emitHook("backup_done", map[string]any{"kind": "lego", "file": path, "bytes": size, "encrypted": encrypt})
			return emit(map[string]any{"file": path, "bytes": size, "encrypted": encrypt})
		},
	}
	cmd.Flags().BoolVar(&encrypt, "encrypt", false, "seal the backup with a passphrase (age)")
	cmd.Flags().StringVar(&passFile, "passphrase-file", "", "read the passphrase from this file")
	cmd.Flags().StringVar(&dest, "dir", "", "where to write it (default: <backup dir>/lego)")
	return cmd
}

func newLegoWatchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Be told (ntfy/webhook) when a part or set's BrickLink price falls to your limit",
		Long: "A watch is checked once a day by the gateway's alert monitor (set NOTIFY_URL) or by `wms lego watch check`.\n" +
			"It uses BrickLink's average SOLD price in your currency, within the daily call budget.",
	}
	var color, cond string
	var maxPrice float64
	add := &cobra.Command{
		Use:     "add <part|set> <number> --max <price>",
		Short:   "Watch an item",
		Example: "  wms lego watch add part 3001 --color red --max 0.05\n  wms lego watch add set 75192 --max 550 --condition N",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			t := ui.New()
			typ, err := parseItemType(args[0])
			if err != nil || typ == "MINIFIG" {
				return usageError("a watch is for a part or a set")
			}
			if maxPrice <= 0 {
				return usageError("--max <price> is required and must be more than zero")
			}
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			w := lego.Watch{ItemType: string(typ), ItemNo: args[1], ColorID: -1, Cond: strings.ToUpper(cond), MaxPrice: maxPrice}
			if typ == "PART" && color != "" {
				cols, _ := db.CatalogColors()
				c, ok := lego.MatchColor(cols, color)
				if !ok {
					return usageError("no colour %q in the catalog", color)
				}
				w.ColorID, w.ColorName = c.ID, c.Name
			}
			if err := db.AddWatch(w); err != nil {
				return err
			}
			say(ui.Status(t, true, fmt.Sprintf("Watching %s %s%s: alert at %.2f or less.", typ, args[1], map[bool]string{true: " (" + w.ColorName + ")", false: ""}[w.ColorName != ""], maxPrice)))
			return emit(map[string]any{"watching": args[1], "type": typ, "max": maxPrice})
		},
	}
	add.Flags().Float64Var(&maxPrice, "max", 0, "alert when the average price is at or below this")
	add.Flags().StringVar(&color, "color", "", "colour (parts): name or Rebrickable id")
	add.Flags().StringVar(&cond, "condition", "U", "N (new) or U (used)")

	list := &cobra.Command{
		Use:   "list",
		Short: "Show your watches and what they last saw",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			t := ui.New()
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			ws, err := db.ListWatches()
			if err != nil {
				return err
			}
			var rows [][]string
			for _, w := range ws {
				last := "not checked yet"
				if !w.LastChecked.IsZero() {
					last = fmt.Sprintf("%.4f (%s)", w.LastPrice, w.LastChecked.Format("2006-01-02"))
				}
				rows = append(rows, []string{strconv.FormatInt(w.ID, 10), w.ItemType, w.ItemNo, orDash(w.ColorName), w.Cond, fmt.Sprintf("%.4f", w.MaxPrice), last})
			}
			if len(rows) == 0 {
				say(ui.Warn(t, "No watches yet: wms lego watch add part 3001 --color red --max 0.05"))
			} else {
				say(ui.RenderColumns(t, []string{"ID", "Type", "Item", "Colour", "Cond", "Alert at", "Last price"}, rows, fmt.Sprintf("%d watch(es)", len(ws))))
			}
			return emit(map[string]any{"watches": ws})
		},
	}
	rm := &cobra.Command{
		Use:   "rm <id>",
		Short: "Stop watching",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return usageError("the id is the number in `wms lego watch list`")
			}
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			if err := db.RemoveWatch(id); err != nil {
				return withCode(exitNotFound, err)
			}
			say(ui.Status(ui.New(), true, "Removed."))
			return emit(map[string]any{"removed": id})
		},
	}
	check := &cobra.Command{
		Use:   "check",
		Short: "Check every watch now (uses BrickLink calls) and show the ones at your limit",
		Args:  cobra.NoArgs,
		RunE: withBL(func(ctx context.Context, db *lego.DB, c *bricklink.Client, _ []string) error {
			t := ui.New()
			hits, err := db.CheckWatches(ctx, blFetcher{c})
			for _, h := range hits {
				say(ui.Status(t, true, watchLine(h)))
			}
			if len(hits) == 0 && err == nil {
				say(ui.Status(t, true, "Nothing is at your limit."))
			}
			if len(hits) > 0 {
				lines := make([]string, len(hits))
				for i, h := range hits {
					lines[i] = watchLine(h)
				}
				emitHook("price_drop", map[string]any{"lines": lines})
			}
			if emitErr := emit(map[string]any{"hits": hits}); emitErr != nil {
				return emitErr
			}
			return err
		}),
	}
	cmd.AddCommand(add, list, rm, check)
	return cmd
}

func watchLine(h lego.WatchHit) string {
	item := h.ItemNo
	if h.ColorName != "" {
		item += " " + h.ColorName
	}
	return fmt.Sprintf("%s: %.4f %s (your limit %.4f)", item, h.Price, h.Currency, h.MaxPrice)
}

// priceDropLines is what the alert monitor reports: watches at or under their limit.
func priceDropLines(ctx context.Context) []string {
	db, err := openLego()
	if err != nil {
		return nil
	}
	defer db.Close()
	c := newBLClient(db)
	if !c.Enabled() {
		return nil
	}
	hits, _ := db.CheckWatches(ctx, blFetcher{c})
	lines := make([]string, len(hits))
	for i, h := range hits {
		lines[i] = watchLine(h)
	}
	return lines
}

func newLegoValueCmd() *cobra.Command {
	var refresh int
	var cond string
	cmd := &cobra.Command{
		Use:   "value",
		Short: "What your collection is worth by BrickLink's average sold prices (always 'N of M priced')",
		Long: "Prices are stored per item, so this works out the value without new calls. --refresh N first fetches up to N\n" +
			"missing or week-old prices (oldest first, within the daily call budget); run it daily to fill the picture in.\n" +
			"Lines BrickLink has no sales for are left out and counted as unpriced — never as zero.",
		Args: cobra.NoArgs,
		RunE: withBL(func(ctx context.Context, db *lego.DB, c *bricklink.Client, _ []string) error {
			t := ui.New()
			if cond == "" {
				cond = config.Get(config.BricklinkCondition)
			}
			cond = strings.ToUpper(cond)
			var refreshErr error
			var fetched, unknown int
			if refresh > 0 {
				if !c.Enabled() {
					return bricklink.ErrNotConfigured
				}
				refreshErr = withSpinner(fmt.Sprintf("fetching up to %d price(s)", refresh), func() error {
					var err error
					fetched, unknown, err = db.RefreshPrices(ctx, blFetcher{c}, cond, refresh)
					return err
				})
				say(ui.Fact(t, "Refreshed", fmt.Sprintf("%d price(s) fetched, %d item(s) BrickLink has no sales for", fetched, unknown)))
				if refreshErr != nil {
					say(ui.Warn(t, "Stopped early: "+refreshErr.Error()))
				}
			}
			v, err := db.CollectionValue(cond)
			if err != nil {
				return err
			}
			cur := orDash(v.Currency)
			say(ui.Fact(t, "Value", fmt.Sprintf("%.2f %s  (%d of %d lines priced)", v.Total, cur, v.Priced, v.Lines)))
			say(ui.Fact(t, "  of which", fmt.Sprintf("parts %.2f, sets %.2f", v.Parts, v.Sets)))
			if v.Priced < v.Lines {
				say(ui.Warn(t, fmt.Sprintf("%d line(s) have no price yet: run `wms lego value --refresh 200` (daily), or BrickLink has no sales for them. The total is a floor, not the full value.", v.Lines-v.Priced)))
			}
			var rows [][]string
			for _, l := range v.Top {
				rows = append(rows, []string{l.Label, strconv.Itoa(l.Qty), fmt.Sprintf("%.4f", l.Each), fmt.Sprintf("%.2f", l.Total)})
			}
			if len(rows) > 0 {
				say(ui.RenderColumns(t, []string{"Item", "Qty", "Each", "Total"}, rows, "Most valuable lines"))
			}
			if err := emit(map[string]any{"value": v, "condition": cond, "fetched": fetched, "unknown": unknown}); err != nil {
				return err
			}
			return refreshErr
		}),
	}
	cmd.Flags().IntVar(&refresh, "refresh", 0, "fetch up to this many missing/stale prices first")
	cmd.Flags().StringVar(&cond, "condition", "", "N (new) or U (used); default BRICKLINK_CONDITION (U)")
	return cmd
}

// equivFlag reads --equivalents; empty means the WMS_EQUIVALENTS setting.
func equivFlag(v string) (lego.Equivalents, error) {
	if strings.TrimSpace(v) == "" {
		return lego.EquivalentsFromConfig(), nil
	}
	eq, err := lego.ParseEquivalents(v)
	if err != nil {
		return "", usageError("--equivalents: %v", err)
	}
	return eq, nil
}

func newLegoBuildCmd() *cobra.Command {
	var o lego.BuildOptions
	cmd := &cobra.Command{
		Use:   "build",
		Short: "Which sets your loose parts could (nearly) build — from the offline catalog, no internet needed",
		Long: "Compares your loose parts (exact part and colour) with every set's parts list and ranks the sets by how much\n" +
			"of each you already hold. Use `wms lego missing <set>` for one set's shopping list (that also counts\n" +
			"alternate and mould parts) and `wms lego wanted --set <num>` for the BrickLink file.",
		Example: "  wms lego build\n  wms lego build --min-percent 85 --max-missing 40",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			t := ui.New()
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			res, err := db.CanBuild(o)
			if err != nil {
				return withCode(exitNotFound, err)
			}
			var rows [][]string
			for _, r := range res {
				rows = append(rows, []string{r.SetNum, r.Name, orDash(r.Theme), strconv.Itoa(r.Year), fmt.Sprintf("%d%%", r.Percent), strconv.Itoa(r.Have) + "/" + strconv.Itoa(r.Total), strconv.Itoa(r.Missing)})
			}
			if len(rows) == 0 {
				say(ui.Warn(t, "No set is covered that well by your loose parts. Lower --min-percent, or add more parts."))
			} else {
				say(ui.RenderColumns(t, []string{"Set #", "Name", "Theme", "Year", "Have", "Pieces", "Missing"}, rows, fmt.Sprintf("%d set(s) you could build or nearly build — %s", len(res), db.CatalogLabel())))
			}
			return emit(map[string]any{"sets": res})
		},
	}
	cmd.Flags().IntVar(&o.MinPercent, "min-percent", 60, "only sets where you hold at least this % of the pieces")
	cmd.Flags().IntVar(&o.MinPieces, "min-pieces", 20, "ignore sets smaller than this")
	cmd.Flags().IntVar(&o.MaxMissing, "max-missing", 0, "at most this many pieces missing (0 = no limit)")
	cmd.Flags().IntVar(&o.Limit, "limit", 30, "how many sets to show")
	return cmd
}

func newLegoHistoryCmd() *cobra.Command {
	var part string
	var limit int
	cmd := &cobra.Command{
		Use:   "history",
		Short: "What changed in your collection, who changed it, and from what to what",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			t := ui.New()
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			h, err := db.History(part, limit)
			if err != nil {
				return err
			}
			var rows [][]string
			for _, r := range h {
				rows = append(rows, []string{r.At.Local().Format("2006-01-02 15:04"), r.Actor, r.Action, r.ItemType + " " + r.Item, orDash(r.ColorName), fmt.Sprintf("%d -> %d", r.Before, r.After), r.Note})
			}
			if len(rows) == 0 {
				say(ui.Warn(t, "Nothing has been recorded yet."))
			} else {
				say(ui.RenderColumns(t, []string{"When", "Who", "What", "Item", "Colour", "Qty", "Note"}, rows, fmt.Sprintf("%d change(s), newest first", len(h))))
			}
			return emit(map[string]any{"changes": h})
		},
	}
	cmd.Flags().StringVar(&part, "part", "", "only changes to this part or set number")
	cmd.Flags().IntVar(&limit, "limit", 30, "how many changes")
	return cmd
}

func newLegoSnapshotsCmd() *cobra.Command {
	var label string
	cmd := &cobra.Command{
		Use:   "snapshots",
		Short: "List restore points (one is taken before the first change of each day)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			t := ui.New()
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			snaps, err := db.Snapshots(40)
			if err != nil {
				return err
			}
			var rows [][]string
			var series []float64
			for i := len(snaps) - 1; i >= 0; i-- {
				series = append(series, float64(snaps[i].Pieces))
			}
			for _, s := range snaps {
				rows = append(rows, []string{strconv.FormatInt(s.ID, 10), s.At.Local().Format("2006-01-02 15:04"), strconv.Itoa(s.Pieces), strconv.Itoa(s.Lines), strconv.Itoa(s.Sets), strconv.Itoa(s.Low), s.Label})
			}
			if len(rows) == 0 {
				say(ui.Warn(t, "No snapshots yet: one is taken before the first change of each day, or run `wms lego snapshots take`."))
			} else {
				say(ui.RenderColumns(t, []string{"ID", "Taken", "Pieces", "Lines", "Sets", "Low", "Label"}, rows, fmt.Sprintf("%d restore point(s)", len(snaps))))
				say(ui.Fact(t, "Pieces over time", lego.Sparkline(series)))
			}
			return emit(map[string]any{"snapshots": snaps})
		},
	}
	take := &cobra.Command{
		Use:   "take",
		Short: "Take a snapshot now",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			id, err := db.TakeSnapshot(orDefault(label, "manual"))
			if err != nil {
				return err
			}
			say(ui.Status(ui.New(), true, fmt.Sprintf("Snapshot #%d taken.", id)))
			return emit(map[string]any{"snapshot": id})
		},
	}
	take.Flags().StringVar(&label, "label", "", "a note to remember it by")
	cmd.AddCommand(take)
	return cmd
}

func orDefault(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
}

func newLegoRestoreCmd() *cobra.Command {
	var yes, dryRun bool
	cmd := &cobra.Command{
		Use:   "restore <snapshot id | date>",
		Short: "Put your owned parts and sets back as they were at a snapshot (the present is saved first, so it can be undone)",
		Long: "Replaces your owned parts and sets with the snapshot's. Part-DB is not touched: parts that are no longer in\n" +
			"your collection stay in Part-DB, and `wms lego sync-parts` re-syncs the rest. The present state is snapshotted\n" +
			"first, so `wms lego restore <that snapshot>` undoes a restore.",
		Example: "  wms lego restore 2026-09-20 --dry-run\n  wms lego restore 14 --yes",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			t := ui.New()
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			snap, err := db.FindSnapshot(args[0])
			if err != nil {
				return withCode(exitNotFound, err)
			}
			plan, err := db.PlanRestore(*snap)
			if err != nil {
				return err
			}
			say(ui.Fact(t, "Snapshot", fmt.Sprintf("#%d from %s (%s)", snap.ID, snap.At.Local().Format("2006-01-02 15:04"), snap.Label)))
			say(ui.Fact(t, "Parts", fmt.Sprintf("%d added back, %d changed, %d removed", plan.PartsAdded, plan.PartsChanged, plan.PartsRemoved)))
			say(ui.Fact(t, "Sets", fmt.Sprintf("%d added back, %d changed, %d removed", plan.SetsAdded, plan.SetsChanged, plan.SetsRemoved)))
			for _, s := range plan.Samples {
				say("    " + s)
			}
			if dryRun {
				return planned("restore snapshot "+strconv.FormatInt(snap.ID, 10), map[string]any{"plan": plan})
			}
			if plan.PartsAdded+plan.PartsChanged+plan.PartsRemoved+plan.SetsAdded+plan.SetsChanged+plan.SetsRemoved == 0 {
				say(ui.Status(t, true, "Already exactly like that snapshot: nothing to do."))
				return emit(map[string]any{"restored": false})
			}
			if err := confirm("Restore your collection to this snapshot?", yes); err != nil {
				return err
			}
			if _, err := db.RestoreSnapshot(context.Background(), *snap); err != nil {
				return err
			}
			say(ui.Status(t, true, "Restored. The state from just before is saved as the newest snapshot, in case you want it back."))
			return emit(map[string]any{"restored": true, "plan": plan})
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "don't ask for confirmation")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would change and stop")
	return cmd
}

func newLegoExportCmd() *cobra.Command {
	var format, outPath, set string
	var copies int
	var missing, force, withImages bool
	var discord discordFlags
	cmd := &cobra.Command{
		Use:   "export --format <rebrickable-csv|bricklink-xml|csv|sets-csv|json|xlsx|html> [-o file]",
		Short: "Write your collection (or one set's missing parts) for another tool",
		Long: "rebrickable-csv   Part,Color,Quantity: Rebrickable's parts list, and what `wms lego import-parts` reads back\n" +
			"bricklink-xml     your parts as a BrickLink inventory (needs `wms bricklink colors sync` for colours)\n" +
			"csv / sets-csv    open in Excel or LibreOffice (text that could be read as a formula is neutralised)\n" +
			"json              everything, structured (--with-images adds each part's picture URL, embedding cached pictures)\n" +
			"xlsx              an Excel workbook: Parts (with a picture link per row) and Sets sheets\n" +
			"html              a printable page: open it in a browser and Print > Save as PDF\n" +
			"With --set N --missing it exports what that set still needs instead of what you own.",
		Example: "  wms lego export --format rebrickable-csv -o my-parts.csv\n  wms lego export --format html -o inventory.html\n  wms lego export --format csv --set 75192 --missing -o falcon-shopping.csv\n  wms lego export --format xlsx --set 75192 --missing --with-images -o falcon.xlsx",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			t := ui.New()
			if out.JSON && outPath == "" {
				return usageError("--json prints a summary, so the export needs -o <file>")
			}
			db, rb, err := openLegoWithClient()
			if err != nil {
				return err
			}
			defer db.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()

			var data *lego.ExportData
			switch {
			case set != "" && missing:
				rep, _, err := inventoryReport(ctx, db, rb, set, copies, lego.EquivalentsFromConfig())
				if err != nil {
					return err
				}
				data = db.ExportMissing(set, rep)
			case set != "" || missing:
				return usageError("--set and --missing go together")
			default:
				if data, err = db.ExportOwned(); err != nil {
					return err
				}
			}

			if withImages {
				data.AddPartImages(img.NewFetcher(config.Get(config.ImageDir)))
			}
			body, _, warns, err := lego.Encode(format, data)
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
			say(ui.Status(t, true, fmt.Sprintf("Wrote %s (%d part line(s), %d set(s))", abs, len(data.Rows), len(data.Sets))))
			for _, w := range warns {
				say(ui.Warn(t, w))
			}
			what := "your collection"
			if set != "" && missing {
				what = "missing parts for " + set
			}
			if err := sendToDiscord(t, discord, abs, what); err != nil {
				return err
			}
			return emit(map[string]any{"file": abs, "format": format, "part_lines": len(data.Rows), "sets": len(data.Sets), "warnings": warns})
		},
	}
	cmd.Flags().StringVar(&format, "format", "", "output format (required)")
	cmd.Flags().StringVarP(&outPath, "out", "o", "", "write to this file (default: print it)")
	cmd.Flags().StringVar(&set, "set", "", "with --missing: the set whose missing parts to export")
	cmd.Flags().BoolVar(&missing, "missing", false, "export what --set still needs instead of what you own")
	cmd.Flags().IntVar(&copies, "copies", 1, "copies of the set")
	discord.register(cmd.Flags())
	cmd.Flags().BoolVar(&force, "force", false, "replace the output file if it exists")
	cmd.Flags().BoolVar(&withImages, "with-images", false, "add picture links to each part (json, xlsx, html); cached pictures are embedded")
	_ = cmd.MarkFlagRequired("format")
	return cmd
}

// writeExportFile writes an export to path, refusing to replace a file unless force.
func writeExportFile(path string, body []byte, force bool) (string, error) {
	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if force {
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}
	f, err := os.OpenFile(path, flags, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", usageError("%s already exists — use --force to replace it", path)
		}
		return "", err
	}
	_, werr := f.Write(body)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return "", werr
	}
	abs, _ := filepath.Abs(path)
	return abs, nil
}
