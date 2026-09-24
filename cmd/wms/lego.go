package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/partdb"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

func newLegoCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "lego", Short: "Manage the local LEGO collection (sets, parts, Rebrickable lookups)"}
	cmd.AddCommand(newLegoImportCmd(), newLegoAddPartCmd(), newLegoSyncPartsCmd(), newLegoCatalogCmd(),
		newLegoLowCmd(), newLegoSetMinCmd(), newLegoOptionalCmd(), newLegoStatsCmd(), newLegoMissingCmd(), newLegoWantedCmd(), newLegoDetailCmd(), newLegoAchievementsCmd(), newLegoTodayCmd(), newLegoCheckCmd(), newLegoShoppingCmd(), newLegoOrdersCmd(), newLegoSpendCmd(), newLegoLabelsCmd(), newLegoSetInfoCmd(), newLegoImportPartsCmd(),
		newLegoSearchCmd(), newLegoBackupCmd(), newLegoWatchCmd(), newLegoValueCmd(), newLegoBuildCmd(),
		newLegoHistoryCmd(), newLegoSnapshotsCmd(), newLegoRestoreCmd(), newLegoExportCmd(),
		newLegoReportCmd(), newLegoRetirementCmd(), newLegoShareCmd(), newLegoHelpSheetCmd())
	return cmd
}

func newLegoAddPartCmd() *cobra.Command {
	var colorFlag, categoryFlag string
	var qty int
	var yes, dryRun bool
	cmd := &cobra.Command{
		Use:   "add-part <part_num>",
		Short: "Add or update an owned LEGO part: details from the catalog/Rebrickable, you give colour and quantity",
		Long: "Looks the part up (offline catalog first, then Rebrickable) to fill in its name and category, then\n" +
			"records how many you hold in that colour in your LEGO collection and in Part-DB (one Part-DB part per\n" +
			"part + colour, filed under Lego > <category>). Colour and quantity are always yours to give.",
		Example:           "  wms lego add-part 3001 --color red --qty 25\n  wms lego add-part 3001 --color 4 --qty 25 --yes",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completePartNum,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true // a bad --color is not a usage error worth a screen of help
			t := ui.New()
			legoDB, err := openLego()
			if err != nil {
				return err
			}
			defer legoDB.Close()
			ctx := context.Background()

			if err := lego.CheckPartNum(args[0]); err != nil {
				return usageError("%v", err)
			}
			partArg := args[0]
			// A LEGO element ID (the 6-8 digit number on a bag or brick) is one part in one colour;
			// a real part number of the same digits wins.
			if cp, err := legoDB.CatalogPart(partArg); err != nil || cp == nil {
				if part, colorID, ok := legoDB.ElementPart(partArg); ok {
					if c, cok := legoDB.ColorByID(colorID); cok {
						say(ui.Warn(t, fmt.Sprintf("Element %s is part %s in %s.", partArg, part, c.Name)))
						partArg = part
						if colorFlag == "" {
							colorFlag = strconv.Itoa(c.ID)
						}
					}
				}
			}
			info := legoDB.LookupPart(ctx, lego.NewClientFor(legoDB), partArg)
			if info == nil {
				say(ui.Status(t, false, fmt.Sprintf("No source knows part %q. Run `wms lego catalog refresh`, check your Rebrickable key, or add it by hand in the TUI (Part-DB Hub > Create New Part).", args[0])))
				return withCode(exitNotFound, fmt.Errorf("part %s not found", args[0]))
			}
			for _, n := range info.Notes {
				say(ui.Warn(t, n))
			}

			colorID, colorName := lego.NoColor, ""
			switch {
			case colorFlag != "" && len(info.Colors) > 0:
				c, ok := lego.MatchColor(info.Colors, colorFlag)
				if !ok {
					names := make([]string, len(info.Colors))
					for i, c := range info.Colors {
						names[i] = fmt.Sprintf("%s (%d)", c.Name, c.ID)
					}
					return usageError("colour %q is not one of the colours %s comes in (or is ambiguous): %s", colorFlag, info.Num, strings.Join(names, ", "))
				}
				colorID, colorName = c.ID, c.Name
			case colorFlag != "":
				colorName = colorFlag // no colour list for this part: kept as typed
			case len(info.Colors) > 0:
				names := make([]string, len(info.Colors))
				for i, c := range info.Colors {
					names[i] = c.Name
				}
				return usageError("--color is required; %s comes in: %s", info.Num, strings.Join(names, ", "))
			}
			if !cmd.Flags().Changed("qty") {
				return usageError("--qty is required (how many you hold in total)")
			}
			category := info.Category
			if categoryFlag != "" {
				category = categoryFlag
			}

			owned := lego.OwnedPart{PartNum: info.Num, Name: info.Name, Category: category, ColorID: colorID, ColorName: colorName, Qty: qty}
			spec := lego.SpecFor(owned, 0)
			say(ui.Fact(t, "Part", info.Num+" — "+info.Name+" ("+info.Source+")"))
			say(ui.Fact(t, "Colour", orDash(colorName)))
			say(ui.Fact(t, "Category", strings.Join(lego.CategoryPath(category), " > ")))
			say(ui.Fact(t, "Part-DB part", spec.Name+"  [IPN "+spec.IPN+"]"))
			say(ui.Fact(t, "Quantity", fmt.Sprint(qty)))
			say(t.Muted.Render("   " + lego.CatalogAttribution))
			if dryRun {
				return planned("save this part", map[string]any{"part": info.Num, "name": info.Name, "colour": colorName, "category": strings.Join(lego.CategoryPath(category), " > "), "ipn": spec.IPN, "part_db_name": spec.Name, "qty": qty, "source": info.Source})
			}
			if err := confirm("Save this?", yes); err != nil {
				return err
			}

			if err := legoDB.AddOwnedPart(owned); err != nil {
				return err
			}
			row, err := legoDB.GetOwnedPart(owned.PartNum, owned.ColorID, owned.ColorName)
			if err != nil || row == nil {
				return fmt.Errorf("saved, but could not read the row back: %v", err)
			}
			say(ui.Status(t, true, "Saved in your LEGO collection"))

			pdb, err := partdb.Open("")
			if err != nil {
				say(ui.Warn(t, "Not sent to Part-DB ("+err.Error()+"); `wms lego sync-parts` will retry."))
				return emit(map[string]any{"saved": true, "part": info.Num, "colour": colorName, "qty": qty, "part_db": nil, "part_db_error": err.Error()})
			}
			defer pdb.Close()
			syncer := &lego.PartSyncer{Lego: legoDB, Writer: partdb.NewWriter(pdb)}
			pctx, cancel := syncer.Writer.Ctx()
			defer cancel()
			res, err := syncer.Push(pctx, *row)
			if err != nil {
				say(ui.Warn(t, "Not sent to Part-DB: "+err.Error()+" — `wms lego sync-parts` will retry."))
				return emit(map[string]any{"saved": true, "part": info.Num, "colour": colorName, "qty": qty, "part_db": nil, "part_db_error": err.Error()})
			}
			say(ui.Status(t, true, fmt.Sprintf("Part-DB part #%d now has %d", res.PartID, qty)))
			emitHook("part_added", map[string]any{"part": info.Num, "name": info.Name, "colour": colorName, "qty": qty, "part_db_id": res.PartID})
			return emit(map[string]any{"saved": true, "part": info.Num, "colour": colorName, "qty": qty, "part_db": res.PartID, "ipn": spec.IPN})
		},
	}
	cmd.Flags().StringVar(&colorFlag, "color", "", "colour name or Rebrickable colour id (required when the part has a colour list)")
	_ = cmd.RegisterFlagCompletionFunc("color", completeColor)
	cmd.Flags().StringVar(&categoryFlag, "category", "", "override the category (default: the one the lookup found)")
	cmd.Flags().IntVar(&qty, "qty", 0, "how many you hold in total in this colour")
	cmd.Flags().BoolVar(&yes, "yes", false, "don't ask for confirmation")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be saved and stop")
	return cmd
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func newLegoCatalogCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "catalog", Short: "The offline copy of Rebrickable's catalog (parts, sets, minifigs, set contents)"}
	var force bool
	var fromDir string
	refresh := &cobra.Command{
		Use:   "refresh",
		Short: "Download the catalog (free, no key needed; at most once a day), or load it from --from-dir",
		Long: "Downloads Rebrickable's free CSV files (parts, colours, sets, themes, minifigures, every set's parts list,\n" +
			"part equivalences) into the local database, so search and adding work with no API and no internet. If the\n" +
			"download isn't possible, put the .csv.gz files from rebrickable.com/downloads in a folder and use\n" +
			"--from-dir <folder>. Data: Rebrickable.",
		RunE: func(cmd *cobra.Command, args []string) error {
			t := ui.New()
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			opts := lego.CatalogOptions{Force: force, Dir: fromDir}
			if !out.JSON && !out.Quiet {
				opts.Progress = func(step string) { fmt.Fprintln(os.Stderr, "  … "+step) }
			}
			res, err := db.RefreshCatalog(context.Background(), opts)
			if err != nil {
				say(ui.Status(t, false, err.Error()))
				return err
			}
			say(ui.Status(t, true, res.Message))
			if res.Updated > 0 {
				emitHook("catalog_refreshed", map[string]any{"files_updated": res.Updated, "rows": res.Rows})
			}
			return emit(map[string]any{"message": res.Message, "files_updated": res.Updated, "rows": res.Rows})
		},
	}
	refresh.Flags().BoolVar(&force, "force", false, "refresh even if it was refreshed less than a day ago")
	refresh.Flags().StringVar(&fromDir, "from-dir", "", "load <name>.csv.gz files from this folder instead of downloading")
	status := &cobra.Command{
		Use:   "status",
		Short: "Show what the offline catalog holds and how old it is",
		RunE: func(cmd *cobra.Command, args []string) error {
			t := ui.New()
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			tables := db.CatalogTables()
			var rows [][]string
			var list []map[string]any
			empty := 0
			for _, tb := range tables {
				loaded := "never"
				if !tb.Loaded.IsZero() {
					loaded = tb.Loaded.Format("2006-01-02 15:04")
				}
				if tb.Rows == 0 {
					empty++
				}
				rows = append(rows, []string{tb.File, strconv.Itoa(tb.Rows), loaded})
				list = append(list, map[string]any{"file": tb.File, "rows": tb.Rows, "loaded": tb.Loaded})
			}
			say(ui.RenderColumns(t, []string{"File", "Rows", "Loaded"}, rows, "Offline catalog"))
			if empty > 0 {
				say(ui.Warn(t, fmt.Sprintf("%d file(s) are empty — run `wms lego catalog refresh`.", empty)))
			}
			say(t.Muted.Render("   " + lego.CatalogAttribution))
			return emit(map[string]any{"files": list, "attribution": lego.CatalogAttribution})
		},
	}
	cmd.AddCommand(refresh, status)
	return cmd
}

func newLegoImportCmd() *cobra.Command {
	var collections []string
	var refSets, refParts string
	cmd := &cobra.Command{
		Use:   "import",
		Short: "One-time import of legacy KC-LEGO-CLI JSON data into the local LEGO store",
		RunE: func(cmd *cobra.Command, args []string) error {
			t := ui.New()
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()

			imported, skipped := 0, 0
			for _, path := range collections {
				i, s, err := lego.ImportCollection(db, path)
				if err != nil {
					say(ui.Status(t, false, err.Error()))
					return err
				}
				imported += i
				skipped += s
			}
			if len(collections) > 0 {
				say(ui.Status(t, true, "Collection import complete"))
				say(ui.Fact(t, "Sets imported", fmt.Sprintf("%d", imported)))
				if skipped > 0 {
					say(ui.Fact(t, "Rows skipped", fmt.Sprintf("%d", skipped)))
				}
			}

			if refSets != "" {
				n, err := lego.ImportRefSets(db, refSets)
				if err != nil {
					say(ui.Status(t, false, err.Error()))
					return err
				}
				say(ui.Fact(t, "Reference sets imported", fmt.Sprintf("%d", n)))
			}
			if refParts != "" {
				n, err := lego.ImportRefParts(db, refParts)
				if err != nil {
					say(ui.Status(t, false, err.Error()))
					return err
				}
				say(ui.Fact(t, "Reference parts imported", fmt.Sprintf("%d", n)))
			}
			return nil
		},
	}
	cmd.Flags().StringArrayVar(&collections, "collection", nil, "path to a collection export JSON (0.json / kc_sets_export_*.json); repeatable, later files win on conflict")
	cmd.Flags().StringVar(&refSets, "ref-sets", "", "path to legolookup.json (Rebrickable set catalog snapshot)")
	cmd.Flags().StringVar(&refParts, "ref-parts", "", "path to legolookup-part.json (Rebrickable part catalog snapshot)")
	return cmd
}

func newLegoSyncPartsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync-parts",
		Short: "Push owned LEGO parts into Part-DB through its API (one-way; sets are never synced)",
		Long: "Normally adding a part already sends it to Part-DB. This pushes anything that could not be sent then\n" +
			"(no API token yet, Part-DB unreachable) and re-syncs quantities. It is safe to run repeatedly.",
		RunE: func(cmd *cobra.Command, args []string) error {
			t := ui.New()
			legoDB, err := openLego()
			if err != nil {
				return err
			}
			defer legoDB.Close()
			pdb, err := partdb.Open("")
			if err != nil {
				return err
			}
			defer pdb.Close()

			syncer := &lego.PartSyncer{Lego: legoDB, Writer: partdb.NewWriter(pdb)}
			ctx, cancel := syncer.Writer.Ctx()
			defer cancel()
			res, err := syncer.SyncNow(ctx)
			if err != nil {
				say(ui.Status(t, false, err.Error()))
				return err
			}
			say(ui.Status(t, len(res.Errors) == 0, "Part sync finished"))
			var errTexts []string
			for _, e := range res.Errors {
				errTexts = append(errTexts, e.Error())
			}
			if err := emit(map[string]any{"created": res.Created, "updated": res.Updated, "unchanged": res.Unchanged, "errors": errTexts}); err != nil {
				return err
			}
			say(ui.Fact(t, "Created", fmt.Sprintf("%d", res.Created)))
			say(ui.Fact(t, "Updated", fmt.Sprintf("%d", res.Updated)))
			say(ui.Fact(t, "Already up to date", fmt.Sprintf("%d", res.Unchanged)))
			if len(res.Errors) > 0 {
				say(ui.Warn(t, fmt.Sprintf("%d error(s):", len(res.Errors))))
				for _, e := range res.Errors {
					fmt.Fprintln(os.Stderr, " - "+e.Error())
				}
				return withCode(exitNetwork, fmt.Errorf("%d part(s) failed to sync", len(res.Errors)))
			}
			return nil
		},
	}
}
