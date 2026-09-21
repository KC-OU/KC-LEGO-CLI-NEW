package uiapp

import "strconv"

func masterDataScreen() screenModel {
	return &menuScreen{
		panelID: "MSTDTA",
		title:   "Master Data",
		options: func(app *App) []menuOption {
			return []menuOption{
				{Key: "1", Label: "View Commodity SPUs", Go: func(app *App) { app.goTo(scrMasterDataSPU) }},
				{Key: "2", Label: "View Suppliers", Go: func(app *App) { app.goTo(scrMasterDataSup) }},
				{Key: "3", Label: "View Customers", Go: func(app *App) { app.goTo(scrMasterDataCus) }},
				{Key: "0", Label: "Return", Go: func(app *App) { app.onBack() }},
			}
		},
	}
}

func masterDataSPUScreen() screenModel {
	return &tableScreen{
		panelID: "MSTSPU",
		title:   "Commodity SPUs",
		columns: []string{"ID", "Code", "Name"},
		fetch: func(app *App) ([][]string, string, error) {
			rows, err := app.wms.MasterDataSPUs(app.ctx())
			if err != nil {
				return nil, "", err
			}
			out := make([][]string, len(rows))
			for i, r := range rows {
				out[i] = []string{strconv.Itoa(r.ID), r.Code, r.Name}
			}
			return out, "", nil
		},
	}
}

func masterDataSupplierScreen() screenModel {
	return &tableScreen{
		panelID: "MSTSUP",
		title:   "Suppliers",
		columns: []string{"ID", "Name"},
		fetch: func(app *App) ([][]string, string, error) {
			rows, err := app.wms.MasterDataSuppliers(app.ctx())
			if err != nil {
				return nil, "", err
			}
			out := make([][]string, len(rows))
			for i, r := range rows {
				out[i] = []string{strconv.Itoa(r.ID), r.Name}
			}
			return out, "", nil
		},
	}
}

func masterDataCustomerScreen() screenModel {
	return &tableScreen{
		panelID: "MSTCUS",
		title:   "Customers",
		columns: []string{"ID", "Name"},
		fetch: func(app *App) ([][]string, string, error) {
			rows, err := app.wms.MasterDataCustomers(app.ctx())
			if err != nil {
				return nil, "", err
			}
			out := make([][]string, len(rows))
			for i, r := range rows {
				out[i] = []string{strconv.Itoa(r.ID), r.Name}
			}
			return out, "", nil
		},
	}
}
