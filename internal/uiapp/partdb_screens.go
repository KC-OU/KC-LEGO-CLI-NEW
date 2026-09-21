package uiapp

import (
	"fmt"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

func partDBHubScreen() screenModel {
	return &menuScreen{
		panelID: "PARTDB",
		title:   "Part-DB Hub",
		options: func(app *App) []menuOption {
			return []menuOption{
				{Key: "1", Label: "Search & Browse", Go: func(app *App) { app.goTo(scrPartDBBrowse) }},
				{Key: "2", Label: "Create New Part", Go: func(app *App) { app.goTo(scrPartDBCreate) }},
				{Key: "3", Label: "Receive / Adjust Stock", Go: func(app *App) { app.goTo(scrPartDBAdjust) }},
				{Key: "0", Label: "Return", Go: func(app *App) { app.onBack() }},
			}
		},
	}
}

func partDBBrowseScreen() screenModel {
	return &formScreen{
		panelID: "PDBSCH",
		title:   "Search Part-DB Components",
		build: func(app *App) []ui.Field {
			return []ui.Field{{Label: "Search term (blank = all)"}}
		},
		submit: func(app *App, values []string) {
			app.partdbSearchTerm = values[0]
			app.goTo(scrPartDBResults)
		},
	}
}

func partDBResultsScreen() screenModel {
	return &tableScreen{
		panelID: "PDBRES",
		title:   "Search Results",
		columns: []string{"ID", "Name", "Mfg PN", "Category", "Stock", "Location"},
		fetch: func(app *App) ([][]string, string, error) {
			parts, err := app.pdb.SearchParts(app.partdbSearchTerm)
			if err != nil {
				return nil, "", err
			}
			rows := make([][]string, len(parts))
			for i, p := range parts {
				rows[i] = []string{strconv.Itoa(p.ID), p.Name, p.MfgPN, p.Category, fmt.Sprintf("%g", p.StockQty), p.Location}
			}
			title := fmt.Sprintf("%d match(es) — press I to inspect a Part ID", len(parts))
			return rows, title, nil
		},
		extra: func(app *App, msg tea.KeyMsg) {
			if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && (msg.Runes[0] == 'i' || msg.Runes[0] == 'I') {
				app.goTo(scrPartDBLookup)
			}
		},
	}
}

func partDBLookupScreen() screenModel {
	return &formScreen{
		panelID: "PDBINS",
		title:   "Inspect Part",
		build: func(app *App) []ui.Field {
			return []ui.Field{{Label: "Part ID"}}
		},
		submit: func(app *App, values []string) {
			id, err := strconv.Atoi(values[0])
			if err != nil {
				app.setMsg("Part ID must be a number.", true)
				return
			}
			pd, err := app.pdb.GetPart(id)
			if err != nil {
				app.setMsg(err.Error(), true)
				return
			}
			app.partDetailRows = [][]string{
				{"ID", strconv.Itoa(pd.ID)},
				{"Name", pd.Name},
				{"Description", pd.Description},
				{"Comment", pd.Comment},
				{"Mfg PN", pd.MfgPN},
				{"Min Amount", fmt.Sprintf("%g", pd.MinAmount)},
				{"Category", pd.Category},
				{"Location", pd.Location},
				{"Total Stock", fmt.Sprintf("%g", pd.TotalStock)},
			}
			app.goTo(scrPartDBDetail)
		},
	}
}

func partDBDetailScreen() screenModel {
	return &tableScreen{
		panelID: "PDBDET",
		title:   "Part Detail",
		columns: []string{"Field", "Value"},
		boxed:   true,
		fetch:   func(app *App) ([][]string, string, error) { return app.partDetailRows, "Part Detail", nil },
	}
}

func partDBAdjustScreen() screenModel {
	return &formScreen{
		panelID:      "PDBADJ",
		title:        "Adjust Stock Quantity",
		writeGated:   true,
		deniedAction: "ADJUST_PARTDB_STOCK",
		build: func(app *App) []ui.Field {
			return []ui.Field{
				{Label: "Part ID"},
				{Label: "Quantity Delta (+/-)"},
			}
		},
		submit: func(app *App, values []string) {
			id, err := strconv.Atoi(values[0])
			if err != nil {
				app.setMsg("Part ID must be a number.", true)
				return
			}
			delta, err := strconv.ParseFloat(values[1], 64)
			if err != nil {
				app.setMsg("Quantity delta must be a number, e.g. 5 or -2.", true)
				return
			}
			newAmount, undo, err := app.pdbw.AdjustStock(id, delta)
			if err != nil {
				app.setMsg(err.Error(), true)
				return
			}
			app.pushUndo(fmt.Sprintf("adjust stock for part #%d", id), undo)
			app.audit.Log(app.session.Username, app.session.Role, "ADJUST_PARTDB_STOCK", "SUCCESS", fmt.Sprintf("id=%d delta=%g new=%g", id, delta, newAmount))
			app.setMsg(fmt.Sprintf("Part #%d stock now %g. Press F9/U to undo.", id, newAmount), false)
			app.onBack()
		},
	}
}
