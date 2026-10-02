package uiapp

// submenuView is which admin-named sub-menu scrSubMenuView renders — set by
// resolveAndRenderMenu right before navigating there, the same draft-on-App
// idiom as accessEdit/dock/kick, since there's one named sub-menu per screen
// per admin choice, not a fixed, registrable set of screen ids.
type submenuView struct {
	screenKey, name string
}

const scrSubMenuView = "submenu_view"

// submenuViewScreen renders exactly one admin-defined sub-menu's items,
// resolved through its screen's own catalog — one level only, since
// Settings.validateMenuLayouts already refuses a sub-menu that references
// another sub-menu.
func submenuViewScreen() screenModel {
	return &menuScreen{
		panelID: "SUBMNU",
		title:   "More",
		intro: func(app *App) string {
			if app.viewingSubmenu == nil {
				return ""
			}
			return app.theme.Muted.Render(app.viewingSubmenu.name)
		},
		options: func(app *App) []menuOption {
			v := app.viewingSubmenu
			if v == nil {
				return []menuOption{{Key: "0", Label: "Return", Go: func(app *App) { app.onBack() }}}
			}
			def := menuScreenDefFor(v.screenKey)
			var opts []menuOption
			if def != nil {
				for _, key := range app.pol().Settings.MenuLayouts[v.screenKey].SubMenus[v.name] {
					it := catalogItemByKey(def.catalog, key)
					if it == nil || !it.allowed(app) {
						continue
					}
					opts = append(opts, menuOption{Key: it.hotkey, Label: it.labelFor(app), Go: it.open})
				}
			}
			opts = append(opts, menuOption{Key: "0", Label: "Return", Go: func(app *App) { app.onBack() }})
			return opts
		},
	}
}
