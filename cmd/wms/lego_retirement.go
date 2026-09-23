package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/audit"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

func newLegoRetirementCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "retirement",
		Short: "LEGO set retirement dates (from a community-maintained sheet — not Rebrickable, which has no such field)",
	}
	cmd.AddCommand(newLegoRetirementRefreshCmd(), newLegoRetirementImportCmd(), newLegoRetirementListCmd())
	return cmd
}

func newLegoRetirementRefreshCmd() *cobra.Command {
	var url string
	cmd := &cobra.Command{
		Use:   "refresh",
		Short: "Fetch retirement dates from the configured (or --url) sheet",
		Long: "This is someone's personal public spreadsheet, not an official API: a failed fetch changes nothing " +
			"(whatever was imported last stays in place) — see LEGO_RETIREMENT_SHEET_URL, or `wms lego retirement import` " +
			"if the live sheet is ever down, moved or restructured.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			t := ui.New()
			if url == "" {
				url = config.Get(config.RetirementSheetURL)
			}
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			rows, err := lego.FetchRetirementCSV(ctx, url)
			if err != nil {
				return err
			}
			n, err := db.ImportRetirements(rows)
			if err != nil {
				return err
			}
			say(ui.Status(t, true, fmt.Sprintf("Imported %d set(s) from %s", n, url)))
			return emit(map[string]any{"imported": n, "url": url})
		},
	}
	cmd.Flags().StringVar(&url, "url", "", "fetch this URL instead of LEGO_RETIREMENT_SHEET_URL")
	return cmd
}

func newLegoRetirementImportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "import <csv-file>",
		Short:   "Import retirement dates from a local CSV (the sheet's own column headers, or close to them)",
		Example: "  wms lego retirement import brick-tap-export.csv",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			t := ui.New()
			f, err := os.Open(args[0])
			if err != nil {
				return err
			}
			defer f.Close()
			rows, err := lego.ParseRetirementCSV(f)
			if err != nil {
				return usageError("%v", err)
			}
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			n, err := db.ImportRetirements(rows)
			if err != nil {
				return err
			}
			say(ui.Status(t, true, fmt.Sprintf("Imported %d set(s) from %s", n, args[0])))
			return emit(map[string]any{"imported": n, "file": args[0]})
		},
	}
	return cmd
}

func newLegoRetirementListCmd() *cobra.Command {
	var withinDays int
	cmd := &cobra.Command{
		Use:     "list",
		Short:   "Sets retiring soon, flagged with whether you own or are watching them",
		Example: "  wms lego retirement list\n  wms lego retirement list --within 30",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			t := ui.New()
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			list, err := db.RetiringSoon(time.Duration(withinDays) * 24 * time.Hour)
			if err != nil {
				return err
			}
			age := db.RetirementDataAge()
			if age.IsZero() {
				say(ui.Warn(t, "No retirement data imported yet: wms lego retirement refresh"))
			}
			rows := make([][]string, len(list))
			for i, s := range list {
				track := ""
				switch {
				case s.Owned && s.Watching:
					track = "owned, watching"
				case s.Owned:
					track = "owned"
				case s.Watching:
					track = "watching"
				}
				rows[i] = []string{s.SetNum, s.Name, s.Theme, s.RetiresAt.Format("2006-01-02"), track}
			}
			if len(rows) > 0 {
				say(ui.RenderColumns(t, []string{"Set", "Name", "Theme", "Retires", "Tracked"}, rows, fmt.Sprintf("%d set(s) retiring within %d day(s)", len(list), withinDays)))
			} else if !age.IsZero() {
				say(ui.Status(t, true, fmt.Sprintf("Nothing retiring within %d day(s).", withinDays)))
			}
			return emit(map[string]any{"retiring": list, "data_age": age})
		},
	}
	cmd.Flags().IntVar(&withinDays, "within", 180, "only sets retiring within this many days")
	return cmd
}

// retirementAutoRefresh keeps retirement data current (RETIREMENT_AUTO_REFRESH_HOURS, off by
// default), mirroring autoRefreshCatalog: fail-soft, logged, never fatal to the gateway.
func retirementAutoRefresh(ctx context.Context, every time.Duration) {
	logger := audit.New()
	wait := time.Minute
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = every
		url := config.Get(config.RetirementSheetURL)
		rows, err := lego.FetchRetirementCSV(ctx, url)
		if err != nil {
			logger.Log("system", "", "RETIREMENT_AUTO_REFRESH", "FAILED", err.Error())
			continue
		}
		db, err := openLego()
		if err != nil {
			logger.Log("system", "", "RETIREMENT_AUTO_REFRESH", "FAILED", err.Error())
			continue
		}
		n, err := db.ImportRetirements(rows)
		db.Close()
		if err != nil {
			logger.Log("system", "", "RETIREMENT_AUTO_REFRESH", "FAILED", err.Error())
			continue
		}
		logger.Log("system", "", "RETIREMENT_AUTO_REFRESH", "SUCCESS", fmt.Sprintf("%d set(s)", n))
	}
}
