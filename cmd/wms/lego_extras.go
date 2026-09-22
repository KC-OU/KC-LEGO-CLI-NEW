package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui/img"
)

func newLegoDetailCmd() *cobra.Command {
	var format, outPath string
	var colorID int
	var isSet, force bool
	cmd := &cobra.Command{
		Use:   "detail <part|set> --export <json|xlsx|html> [-o file]",
		Short: "Export a part's or set's page with its picture (a set includes its whole parts list)",
		Long: "The same page as the TUI's Part / Set Detail, as a file: facts, the picture (embedded in json and\n" +
			"html, linked in xlsx) and, for a set, every part in it with its picture link.\n" +
			"A number with a dash (75192-1) is a set; use --set for a set number without one.",
		Example: "  wms lego detail 75192-1 --export json -o falcon.json\n  wms lego detail 3001 --color 4 --export html -o brick.html",
		Args:    cobra.ExactArgs(1),
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
			kind := "part"
			if isSet || strings.Contains(args[0], "-") {
				kind = "set"
			}
			data, err := db.ExportDetail(ctx, rb, img.NewFetcher(config.Get(config.ImageDir)), kind, args[0], colorID)
			if err != nil {
				return withCode(exitNotFound, err)
			}
			body, _, _, err := lego.Encode(format, data)
			if err != nil {
				return usageError("%v", err)
			}
			if outPath == "" {
				_, _ = os.Stdout.Write(body)
				return nil
			}
			abs, err := writeExportFile(outPath, body, force)
			if err != nil {
				return err
			}
			say(ui.Status(t, true, fmt.Sprintf("Wrote %s (%s, %d part line(s), picture: %v)", abs, data.Title, len(data.Rows), data.Picture != nil && data.Picture.Base64 != "")))
			return emit(map[string]any{"file": abs, "format": format, "kind": kind, "title": data.Title, "part_lines": len(data.Rows)})
		},
	}
	cmd.Flags().StringVar(&format, "export", "json", "json, xlsx or html")
	cmd.Flags().StringVarP(&outPath, "out", "o", "", "write to this file (default: print it)")
	cmd.Flags().IntVar(&colorID, "color", -1, "Rebrickable colour id for a part's picture")
	cmd.Flags().BoolVar(&isSet, "set", false, "treat the number as a set")
	cmd.Flags().BoolVar(&force, "force", false, "replace the output file if it exists")
	return cmd
}

func newLegoAchievementsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "achievements",
		Short: "Collection milestones and how close you are to each",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			t := ui.New()
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			list, err := db.Achievements(strings.ToUpper(config.Get(config.BricklinkCondition)))
			if err != nil {
				return err
			}
			got := 0
			out := make([]map[string]any, len(list))
			for i, a := range list {
				if a.Got() {
					got++
				}
				say(ui.AchievementLine(t, a.Name, a.Desc, a.Have, a.Goal))
				out[i] = map[string]any{"id": a.ID, "name": a.Name, "description": a.Desc, "have": a.Have, "goal": a.Goal, "unlocked": a.Got()}
			}
			say(ui.Fact(t, "Unlocked", fmt.Sprintf("%d of %d", got, len(list))))
			return emit(map[string]any{"unlocked": got, "total": len(list), "achievements": out})
		},
	}
}

func newLegoTodayCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "today",
		Short: "Set of the Day: a set your loose parts nearly cover (90%+), the same for everyone today",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			t := ui.New()
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			r, err := db.SetOfTheDay(time.Now())
			if err != nil {
				return err
			}
			if r == nil {
				say(t.Muted.Render("No set is 90% covered by your loose parts yet — `wms lego build` shows the closest."))
				return emit(map[string]any{"set": nil})
			}
			say(ui.Fact(t, "Set of the Day", fmt.Sprintf("%s %s (%d)", r.SetNum, r.Name, r.Year)))
			say(ui.Fact(t, "You hold", fmt.Sprintf("%d%% — %d of %d pieces, %d short", r.Percent, r.Have, r.Total, r.Missing)))
			say(t.Muted.Render("   What is missing: wms lego missing " + r.SetNum))
			return emit(map[string]any{"set": r.SetNum, "name": r.Name, "year": r.Year, "percent": r.Percent, "have": r.Have, "pieces": r.Total, "missing": r.Missing})
		},
	}
}
