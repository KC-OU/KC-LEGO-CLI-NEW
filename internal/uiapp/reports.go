package uiapp

import (
	"strings"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

// Reports reuse the same export screen (format picker, saved file, QR download link)
// as every other export in this app — a "report" format renders the clean, printable
// form (internal/lego.ReportHTML) instead of the flat table the other formats give.

func reportsHubScreen() screenModel {
	return &menuScreen{
		panelID: "REPORT",
		title:   "Reports",
		options: func(app *App) []menuOption {
			return []menuOption{
				{Key: "1", Label: "Missing parts — every incomplete set", Go: func(app *App) { startExport(app, missingReportJob()) }, Perm: "lego.view"},
				{Key: "2", Label: "Full collection: every set and loose part", Go: func(app *App) { startExport(app, collectionReportJob()) }, Perm: "lego.view"},
				{Key: "3", Label: "Full parts list for one or more sets", Go: func(app *App) { app.goTo(scrReportsSet) }, Perm: "lego.view"},
				{Key: "0", Label: "Return", Go: func(app *App) { app.onBack() }},
			}
		},
	}
}

func reportsSetAskScreen() screenModel {
	return &formScreen{
		panelID: "REPSET",
		title:   "Parts List for a Set",
		build: func(app *App) []ui.Field {
			return []ui.Field{{Label: "Set number(s), space-separated (e.g. 75192 10230)"}}
		},
		preamble: func(app *App) string {
			return app.theme.Muted.Render("Every part in each set, side by side — the full list, not just what's missing.")
		},
		submit: func(app *App, values []string) {
			sets := strings.Fields(values[0])
			if len(sets) == 0 {
				app.setMsg("Enter at least one set number.", true)
				return
			}
			startExport(app, setPartsReportJob(sets))
		},
	}
}

func missingReportJob() *exportJob {
	return &exportJob{What: "missing parts across every incomplete set", Kind: "missing-report",
		Formats: []string{"report", "html", "xlsx", "csv", "json"},
		Build: func(app *App) (*lego.ExportData, error) {
			return app.legoDB.MissingPartsReport(nil)
		}}
}

func collectionReportJob() *exportJob {
	return &exportJob{What: "your full collection", Kind: "collection-report",
		Formats: []string{"report", "html", "xlsx", "csv", "json"},
		Build: func(app *App) (*lego.ExportData, error) {
			return app.legoDB.CollectionReport()
		}}
}

func setPartsReportJob(sets []string) *exportJob {
	return &exportJob{What: "parts list for " + strings.Join(sets, ", "), Kind: "set-parts-report", Num: strings.Join(sets, "-"),
		Formats: []string{"report", "html", "xlsx", "csv", "json"},
		Build: func(app *App) (*lego.ExportData, error) {
			return app.legoDB.SetPartsReport(sets)
		}}
}
