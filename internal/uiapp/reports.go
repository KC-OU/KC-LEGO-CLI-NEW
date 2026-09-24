package uiapp

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/exports"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

// Reports reuse the same export screen (format picker, saved file, QR download link)
// as every other export in this app — a "report" format renders the clean, printable
// form (internal/lego.ReportHTML) instead of the flat table the other formats give.

func retiringRows(app *App) ([]string, [][]string, []string) {
	list, err := app.legoDB.RetiringSoon(180 * 24 * time.Hour)
	if err != nil {
		app.setMsg(err.Error(), true)
		return nil, nil, nil
	}
	var rows [][]string
	var keys []string
	for _, s := range list {
		track := ""
		switch {
		case s.Owned && s.Watching:
			track = "owned, watching"
		case s.Owned:
			track = "owned"
		case s.Watching:
			track = "watching"
		}
		rows = append(rows, []string{s.SetNum, s.Name, s.Theme, s.RetiresAt.Format("2 Jan 2006"), track})
		keys = append(keys, s.SetNum)
	}
	return []string{"Set", "Name", "Theme", "Retires", "Tracked"}, rows, keys
}

func reportsHubScreen() screenModel {
	return &menuScreen{
		panelID: "REPORT",
		title:   "Reports",
		options: func(app *App) []menuOption {
			return []menuOption{
				{Key: "1", Label: "Parts Stock-take: a blank checklist for a set", Go: func(app *App) { app.goTo(scrReportsStocktake) }, Perm: "lego.view"},
				{Key: "2", Label: "Set (ID) Parts Lists: full parts list for one or more sets", Go: func(app *App) { app.goTo(scrReportsSet) }, Perm: "lego.view"},
				{Key: "3", Label: "List of Sets on Collection", Go: func(app *App) { startExport(app, setListReportJob()) }, Perm: "lego.view"},
				{Key: "4", Label: "Missing Parts — every incomplete set", Go: func(app *App) { startExport(app, missingReportJob()) }, Perm: "lego.view"},
				{Key: "5", Label: "Extra Parts: spares from completed checks", Go: func(app *App) { startExport(app, extraPartsReportJob()) }, Perm: "lego.view"},
				{Key: "6", Label: "Order List", Go: func(app *App) { startExport(app, orderListReportJob()) }, Perm: "orders.view"},
				{Key: "7", Label: "Archive: previously generated reports", Go: func(app *App) { app.goTo(scrReportsArchive) }, Perm: "lego.view"},
				{Key: "0", Label: "Return", Go: func(app *App) { app.onBack() }},
			}
		},
	}
}

func reportsSetAskScreen() screenModel {
	return &formScreen{
		panelID: "REPSET",
		title:   "Set (ID) Parts Lists",
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

func reportsStocktakeAskScreen() screenModel {
	return &formScreen{
		panelID: "REPSTK",
		title:   "Parts Stock-take",
		build: func(app *App) []ui.Field {
			return []ui.Field{{Label: "Set number (e.g. 75192)"}}
		},
		preamble: func(app *App) string {
			return app.theme.Muted.Render("A blank checklist — part, colour, name, expected qty, an empty box — to print and count against by hand.")
		},
		submit: func(app *App, values []string) {
			set := strings.TrimSpace(values[0])
			if set == "" {
				app.setMsg("Enter a set number.", true)
				return
			}
			startExport(app, stocktakeReportJob(set))
		},
	}
}

func missingReportJob() *exportJob {
	return &exportJob{What: "missing parts across every incomplete set", Kind: "missing-report",
		Formats: []string{"report", "html", "xlsx", "csv", "json"},
		Archive: true,
		Build: func(app *App) (*lego.ExportData, error) {
			return app.legoDB.MissingPartsReport(nil)
		}}
}

func setListReportJob() *exportJob {
	return &exportJob{What: "list of sets", Kind: "set-list-report",
		Formats: []string{"report", "sets-csv", "xlsx", "json"},
		Archive: true,
		Build: func(app *App) (*lego.ExportData, error) {
			return app.legoDB.SetsListReport()
		}}
}

func extraPartsReportJob() *exportJob {
	return &exportJob{What: "extra parts from completed checks", Kind: "extra-parts-report",
		Formats: []string{"report", "html", "xlsx", "csv", "json"},
		Archive: true,
		Build: func(app *App) (*lego.ExportData, error) {
			return app.legoDB.ExtraPartsReport(nil)
		}}
}

func orderListReportJob() *exportJob {
	return &exportJob{What: "order list", Kind: "order-list-report",
		Formats: []string{"report", "html", "xlsx", "csv", "json"},
		Archive: true,
		Build: func(app *App) (*lego.ExportData, error) {
			return app.legoDB.OrderListReport("")
		}}
}

func setPartsReportJob(sets []string) *exportJob {
	return &exportJob{What: "parts list for " + strings.Join(sets, ", "), Kind: "set-parts-report", Num: strings.Join(sets, "-"),
		Formats: []string{"report", "html", "xlsx", "csv", "json"},
		Archive: true,
		Build: func(app *App) (*lego.ExportData, error) {
			return app.legoDB.SetPartsReport(sets)
		}}
}

// ---- archive browsing: reuses the export screen (scrExport) to show the fresh share
// link, exactly like every other export — no separate result rendering needed. ----

func archiveRows(app *App) ([]string, [][]string, []string) {
	list, err := app.legoDB.ListArchive(app.userKey(), app.isAdmin())
	if err != nil {
		app.setMsg(err.Error(), true)
		return nil, nil, nil
	}
	var rows [][]string
	var keys []string
	for _, e := range list {
		id := strconv.FormatInt(e.ID, 10)
		rows = append(rows, []string{id, e.Kind, e.Title, e.CreatedBy, e.CreatedAt.Format("2 Jan 2006 15:04")})
		keys = append(keys, id)
	}
	return []string{"ID", "Kind", "Title", "By", "Created"}, rows, keys
}

func archiveKeys(app *App, key string, msg tea.KeyMsg) {
	if key == "" || msg.Type != tea.KeyEnter {
		return
	}
	id, err := strconv.ParseInt(key, 10, 64)
	if err != nil {
		return
	}
	e, err := app.legoDB.GetArchiveEntry(id)
	if err != nil {
		app.setMsg("Not found.", true)
		return
	}
	if e.CreatedBy != app.userKey() && !app.isAdmin() {
		app.setMsg("That report belongs to someone else.", true)
		return
	}
	body, err := os.ReadFile(filepath.Join(config.Get(config.ArchiveDir), e.File))
	if err != nil {
		app.setMsg(err.Error(), true)
		return
	}
	dir := exports.Dir()
	ext := strings.TrimPrefix(filepath.Ext(e.File), ".")
	path, err := exports.Save(dir, app.userKey(), e.Kind, "", ext, body)
	if err != nil {
		app.setMsg(err.Error(), true)
		return
	}
	res := &exportResult{Path: path}
	if exports.URL("x") != "" && app.can("exports.download") {
		if tok, terr := exports.NewShareLink(dir, path, app.userKey(), 24*time.Hour); terr == nil {
			res.URL = exports.URL(tok)
		}
	}
	app.exportJob = &exportJob{What: e.Title, Kind: e.Kind}
	app.exportRes = res
	app.goTo(scrExport)
}

func stocktakeReportJob(set string) *exportJob {
	return &exportJob{What: "stock-take checklist for " + set, Kind: "stocktake", Num: set,
		Formats: []string{"checklist"},
		Archive: true,
		RawBuild: func(app *App) ([]byte, string, error) {
			title, lines, err := app.legoDB.StockSheet(app.ctx(), app.rebrick, set)
			if err != nil {
				return nil, "", err
			}
			body, err := lego.StockSheetHTML(title, lines)
			return body, "html", err
		}}
}
