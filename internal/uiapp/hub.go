package uiapp

func anyModuleAllowed(app *App, modules ...string) bool {
	for _, m := range modules {
		if app.moduleAllowed(m) {
			return true
		}
	}
	return false
}

// hubOptions mirrors AVAILABLE_TABS from modernwms_tui.py, but groups the
// warehouse-operation and admin tabs into the Operations/Admin submenus
// below instead of listing all eleven as one flat page — each submenu item
// still gates on the exact same auth.IsModuleAllowed check it always did, so
// this only adds a navigation hop for those items, never a permission
// change; dashboard/partdb/scripts/lego stay top-level since (per
// alwaysAllowedModules) every signed-in user can already reach them.
func hubOptions(app *App) []menuOption {
	// open makes the chosen entry the highlighted tab (classic layout's tab
	// bar) for every screen reached from it, then navigates.
	open := func(key, target string) func(app *App) {
		return func(app *App) {
			app.activeTab = key
			app.goTo(target)
		}
	}
	var opts []menuOption
	if app.moduleAllowed("dashboard") {
		opts = append(opts, menuOption{Key: "1", Label: "Overview", Go: open("1", scrOverview)})
	}
	if app.moduleAllowed("partdb") {
		opts = append(opts, menuOption{Key: "2", Label: "PartDB Hub", Go: open("2", scrPartDBHub)})
	}
	if anyModuleAllowed(app, "asn", "warehouse_ops", "stock_lookup", "master_data", "delivery") {
		opts = append(opts, menuOption{Key: "3", Label: "Operations", Go: open("3", scrOpsHub)})
	}
	if app.moduleAllowed("scripts") {
		opts = append(opts, menuOption{Key: "4", Label: "Script Hub", Go: open("4", scrScripts)})
	}
	if app.moduleAllowed("lego") {
		opts = append(opts, menuOption{Key: "e", Label: "LEGO Collection", Go: open("e", scrLegoHub)})
	}
	if anyModuleAllowed(app, "user_mgmt", "settings", "docker", "access") {
		opts = append(opts, menuOption{Key: "9", Label: "Admin", Go: open("9", scrAdminHub)})
	}
	return opts
}

func hubScreen() screenModel {
	return &menuScreen{panelID: "MAIN", title: "ModernWMS & Part-DB Control Suite", caption: "Main Navigation Hub:", options: hubOptions}
}

// subMenu builds a submenu screen from a static option table, filtering
// each row through auth.IsModuleAllowed exactly like the old flat hub did —
// opsHubScreen and adminHubScreen are both just this with a different table.
func subMenu(panelID, title string, rows []struct{ key, module, label, target string }) screenModel {
	return &menuScreen{
		panelID: panelID,
		title:   title,
		options: func(app *App) []menuOption {
			var opts []menuOption
			for _, o := range rows {
				if !app.moduleAllowed(o.module) {
					continue
				}
				target := o.target
				opts = append(opts, menuOption{Key: o.key, Label: o.label, Go: func(app *App) { app.goTo(target) }})
			}
			opts = append(opts, menuOption{Key: "0", Label: "Return", Go: func(app *App) { app.onBack() }})
			return opts
		},
	}
}

func opsHubScreen() screenModel {
	return subMenu("OPSMEN", "Operations", []struct{ key, module, label, target string }{
		{"1", "asn", "Inbound ASN", scrASN},
		{"2", "warehouse_ops", "Warehouse Ops", scrPartDBAdjust},
		{"3", "stock_lookup", "Inventory", scrInventory},
		{"4", "master_data", "Master Data", scrMasterData},
		{"5", "delivery", "Outbound", scrOutbound},
	})
}

func adminHubScreen() screenModel {
	return subMenu("ADMMEN", "Admin", []struct{ key, module, label, target string }{
		{"1", "user_mgmt", "Users", scrUsers},
		{"2", "settings", "Settings & API Keys", scrSettingsHub},
		{"3", "docker", "Containers", scrContainers},
		{"4", "access", "Access Control (permissions, 2FA, timeouts)", scrAccessHub},
	})
}

func quickAddScreen() screenModel {
	return &menuScreen{
		panelID: "QADD",
		title:   "Quick Add",
		options: func(app *App) []menuOption {
			return []menuOption{
				{Key: "1", Label: "Quick Part-DB Component Creation", Go: func(app *App) { app.goTo(scrPartDBCreate) }},
				{Key: "2", Label: "Quick Inbound Stock Receipt", Go: func(app *App) { app.goTo(scrASN) }},
				{Key: "3", Label: "Quick Stock Quantity Adjustment", Go: func(app *App) { app.goTo(scrPartDBAdjust) }},
				{Key: "4", Label: "Quick Master Data Entry", Go: func(app *App) { app.goTo(scrMasterData) }},
				{Key: "0", Label: "Return", Go: func(app *App) { app.onBack() }},
			}
		},
	}
}
