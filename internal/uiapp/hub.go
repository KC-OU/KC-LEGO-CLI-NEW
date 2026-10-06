package uiapp

func anyModuleAllowed(app *App, modules ...string) bool {
	for _, m := range modules {
		if app.moduleAllowed(m) {
			return true
		}
	}
	return false
}

// open makes the chosen entry the highlighted tab (classic layout's tab bar)
// for every screen reached from it, then navigates.
func openTab(hotkey, target string) func(app *App) {
	return func(app *App) {
		app.activeTab = hotkey
		app.goTo(target)
	}
}

// mainMenuCatalog is every item that can appear on the top-level menu: the
// six hub tabs, in their traditional order and hotkeys, plus every row in
// the Admin submenu (adminHubScreen) promoted out of it — anything else
// stays nested exactly as it already is. See menu_catalog.go for how an
// admin chooses from this (per user, per group, or globally) and
// resolveAndRenderMenu for how a catalog becomes what's actually on screen.
var mainMenuCatalog = []menuCatalogItem{
	{"overview", "1", "Overview", func(app *App) bool { return app.moduleAllowed("dashboard") }, openTab("1", scrOverview)},
	{"partdb", "2", "PartDB Hub", func(app *App) bool { return app.moduleAllowed("partdb") }, openTab("2", scrPartDBHub)},
	{"operations", "3", "Operations", func(app *App) bool {
		return anyModuleAllowed(app, "asn", "warehouse_ops", "stock_lookup", "master_data", "delivery")
	}, openTab("3", scrOpsHub)},
	{"scripts", "4", "Script Hub", func(app *App) bool { return app.moduleAllowed("scripts") }, openTab("4", scrScripts)},
	{"lego", "e", "LEGO Collection", func(app *App) bool { return app.moduleAllowed("lego") }, openTab("e", scrLegoHub)},
	{"admin", "9", "Admin", func(app *App) bool {
		return anyModuleAllowed(app, "user_mgmt", "settings", "docker", "access")
	}, openTab("9", scrAdminHub)},
	{"admin_users", "b", "Admin: Users", func(app *App) bool { return app.moduleAllowed("user_mgmt") }, func(app *App) { app.goTo(scrUsers) }},
	{"admin_settings", "c", "Admin: Settings & API Keys", func(app *App) bool { return app.moduleAllowed("settings") }, func(app *App) { app.goTo(scrSettingsHub) }},
	{"admin_containers", "d", "Admin: Containers", func(app *App) bool { return app.moduleAllowed("docker") }, func(app *App) { app.goTo(scrContainers) }},
	{"admin_access", "f", "Admin: Access Control", func(app *App) bool { return app.moduleAllowed("access") }, func(app *App) { app.goTo(scrAccessHub) }},
	{"admin_assign", "h", "Admin: Assign Work", func(app *App) bool { return app.moduleAllowed("user_mgmt") }, func(app *App) { app.goTo(scrAssignPick) }},
	{"admin_message", "i", "Admin: Message a User", func(app *App) bool { return app.moduleAllowed("user_mgmt") }, func(app *App) { app.goTo(scrMessagePick) }},
	{"admin_accuracy", "j", "Admin: Dock / Credit Accuracy", func(app *App) bool { return app.moduleAllowed("user_mgmt") }, func(app *App) { app.goTo(scrAccuracyWho) }},
	{"admin_sessions", "k", "Admin: Live Sessions", func(app *App) bool { return app.moduleAllowed("user_mgmt") }, func(app *App) { app.goTo(scrLiveSessions) }},
	{"admin_handover", "m", "Admin: Shift Handover Note", func(app *App) bool { return app.moduleAllowed("user_mgmt") }, func(app *App) { app.goTo(scrHandover) }},
	{"admin_activity", "n", "Admin: Recent Activity", func(app *App) bool { return app.moduleAllowed("user_mgmt") }, func(app *App) { app.goTo(scrAdminEvents) }},
	{"admin_alerts", "o", "Admin: Alerts", func(app *App) bool { return app.moduleAllowed("user_mgmt") }, func(app *App) { app.goTo(scrAlerts) }},
}

// defaultMainMenuKeys is today's unmodified top-level menu — what every
// installation already showed before menu customization existed, and what a
// viewer with no override at any tier still gets.
var defaultMainMenuKeys = []string{"overview", "partdb", "operations", "scripts", "lego", "admin"}

// hubOptions mirrors AVAILABLE_TABS from modernwms_tui.py by default (see
// defaultMainMenuKeys), but an admin can reorder it, drop tabs they don't
// want shown, promote specific Admin rows onto it, or move items into a
// sub-menu instead — per user, per group, or for everyone (see
// resolveAndRenderMenu). Every item still gates on the exact same permission
// check it always did, so this only changes navigation, never what a given
// item requires to open. Deliberately excludes Log out/Exit (see hubScreen):
// this is also what the classic layout's tab bar renders (app.go's
// viewClassic), which is for switching sections, not one-shot actions.
func hubOptions(app *App) []menuOption {
	return resolveAndRenderMenu(app, scrHub, mainMenuCatalog, defaultMainMenuKeys)
}

func hubScreen() screenModel {
	return &menuScreen{
		panelID: "MAIN", title: "ModernWMS & Part-DB Control Suite", caption: "Main Navigation Hub:",
		// Log out and Exit are pinned last here, same as the picker/checker
		// hub (pickerHubScreen) — a customized layout can hide or reorder
		// every other item, but never the way out. Not folded into
		// hubOptions itself: that list doubles as the classic tab bar's
		// source (app.go's viewClassic), where these two don't belong.
		options: func(app *App) []menuOption {
			opts := resolveAndRenderMenu(app, scrHub, mainMenuCatalog, defaultMainMenuKeys, "9", "0")
			return append(opts,
				menuOption{Key: "9", Label: "Log out", Go: func(app *App) { app.logout() }},
				menuOption{Key: "0", Label: "Exit", Go: func(app *App) { app.quitting = true }},
			)
		},
	}
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
		{"5", "user_mgmt", "Assign Work (pickers/checkers)", scrAssignPick},
		{"6", "user_mgmt", "Message a User", scrMessagePick},
		{"7", "user_mgmt", "Dock / Credit Accuracy", scrAccuracyWho},
		{"8", "user_mgmt", "Live Sessions", scrLiveSessions},
		{"9", "user_mgmt", "Shift Handover Note", scrHandover},
		{"a", "user_mgmt", "Recent Activity", scrAdminEvents},
		{"b", "user_mgmt", "Alerts", scrAlerts},
		{"c", "user_mgmt", "Weekly Rota", scrRotaView},
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
