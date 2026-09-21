package uiapp

import "strconv"

func outboundScreen() screenModel {
	return &tableScreen{
		panelID: "OUTBND",
		title:   "Outbound Dispatches",
		columns: []string{"ID", "Dispatch No", "Status"},
		fetch: func(app *App) ([][]string, string, error) {
			rows, err := app.wms.DispatchList(app.ctx())
			if err != nil {
				return nil, "", err
			}
			out := make([][]string, len(rows))
			for i, r := range rows {
				out[i] = []string{strconv.Itoa(r.ID), r.No, strconv.Itoa(r.Status)}
			}
			return out, "", nil
		},
	}
}
