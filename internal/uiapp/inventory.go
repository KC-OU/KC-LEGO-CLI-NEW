package uiapp

import (
	"fmt"
	"strconv"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

const scrInventoryResults = "inventory_results"

func inventoryScreen() screenModel {
	return &formScreen{
		panelID: "STKLKP",
		title:   "Inventory — Stock Lookup",
		build: func(app *App) []ui.Field {
			return []ui.Field{{Label: "SPU Name / Code / ID (blank = all)"}}
		},
		submit: func(app *App, values []string) {
			app.partdbSearchTerm = values[0]
			app.goTo(scrInventoryResults)
		},
	}
}

func inventoryResultsScreen() screenModel {
	return &tableScreen{
		panelID: "STKRES",
		title:   "Stock Balances",
		columns: []string{"Stock ID", "SPU Code", "SPU Name", "Qty", "Status"},
		fetch: func(app *App) ([][]string, string, error) {
			rows, err := app.wms.StockLookup(app.ctx(), app.partdbSearchTerm)
			if err != nil {
				return nil, "", err
			}
			out := make([][]string, len(rows))
			for i, r := range rows {
				status := "NORMAL"
				if r.Frozen {
					status = "FROZEN"
				}
				out[i] = []string{strconv.Itoa(r.ID), r.SPUCode, r.SPUName, strconv.Itoa(r.Qty), status}
			}
			return out, fmt.Sprintf("%d record(s)", len(out)), nil
		},
	}
}
