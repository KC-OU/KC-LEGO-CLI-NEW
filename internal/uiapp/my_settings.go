package uiapp

import (
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

// A per-user settings menu: display theme and the loading indicator already had
// their own screens (reachable only from the command palette); this adds a real
// menu entry that gathers those with the new personal API key override. Part-DB
// and BrickLink credentials stay shared-only for now — nobody asked for a
// personal override there yet, and BrickLink's OAuth secret is not something to
// invite someone to paste into a picker's own settings screen without a reason to.

const (
	scrMySettings    = "my_settings"
	scrMyRebrickKey  = "my_rebrickable_key"
	scrMyRebrickEdit = "my_rebrickable_key_edit"
)

func mySettingsScreen() screenModel {
	return &menuScreen{
		panelID: "MYSET",
		title:   "My Settings",
		options: func(app *App) []menuOption {
			return []menuOption{
				{Key: "1", Label: "My display theme", Go: func(app *App) { app.goTo(scrMyTheme) }},
				{Key: "2", Label: "My loading indicator", Go: func(app *App) { app.goTo(scrMyLoading) }},
				{Key: "3", Label: "My Rebrickable API key", Go: func(app *App) { app.goTo(scrMyRebrickKey) }},
				{Key: "4", Label: "My Exports", Go: func(app *App) { app.goTo(scrMyExports) }, Perm: "exports.download"},
				{Key: "0", Label: "Return", Go: func(app *App) { app.onBack() }},
			}
		},
		intro: func(app *App) string {
			return app.theme.Muted.Render("Part-DB and BrickLink credentials use the shared instance-wide keys.")
		},
	}
}

// myRebrickableKeyScreen is a real choice, not a magic keyword buried in a text
// field: use your own personal key, or explicitly go back to the shared one —
// "global" is always one keypress away.
func myRebrickableKeyScreen() screenModel {
	return &menuScreen{
		panelID: "MYRBK",
		title:   "My Rebrickable API Key",
		options: func(app *App) []menuOption {
			return []menuOption{
				{Key: "1", Label: "Set or change my personal key", Go: func(app *App) { app.goTo(scrMyRebrickEdit) }},
				{Key: "2", Label: "Use the shared/global key", Go: useSharedRebrickableKey},
				{Key: "0", Label: "Return", Go: func(app *App) { app.onBack() }},
			}
		},
		intro: func(app *App) string {
			status := "using the shared/global key"
			if app.session != nil {
				if cur := loadPrefs()[app.session.Username].RebrickableKey; cur != "" {
					status = "personal key: " + maskSecret(cur)
				}
			}
			return app.theme.Muted.Render("Currently " + status + ".")
		},
	}
}

func myRebrickableEditScreen() screenModel {
	return &formScreen{
		panelID: "MYRBKE",
		title:   "Set My Rebrickable Key",
		build: func(app *App) []ui.Field {
			return []ui.Field{{Label: "Personal Rebrickable API key", Password: true}}
		},
		submit: func(app *App, values []string) {
			newKey := values[0]
			if newKey == "" {
				app.setMsg("Enter a key, or Return then \"Use the shared/global key\" to clear it.", true)
				return
			}
			saveUserRebrickableKey(app, newKey)
			app.rebrick.APIKey = newKey
			app.setMsg("Your personal Rebrickable key is saved and in use for this session.", false)
			app.stack = nil
			app.cur = scrMySettings
		},
	}
}

func useSharedRebrickableKey(app *App) {
	saveUserRebrickableKey(app, "")
	app.rebrick.APIKey = config.Get(config.RebrickableAPIKey)
	app.setMsg("Back on the shared/global Rebrickable key.", false)
}

func saveUserRebrickableKey(app *App, key string) {
	user, role := "", ""
	if app.session != nil {
		user, role = app.session.Username, app.session.Role
	}
	if err := savePrefField(user, func(p *userPref) { p.RebrickableKey = key }); err != nil {
		app.setMsg(err.Error(), true)
		return
	}
	app.audit.Log(user, role, "USER_REBRICKABLE_KEY", "SUCCESS", maskSecret(key))
}

// applyUserRebrickableKey switches app.rebrick to the signed-in user's personal
// Rebrickable key when they have one — called at sign-on and whenever the key
// changes. With no personal override, this deliberately leaves a.rebrick alone
// (already built with the shared key at NewApp, or — in tests — pre-configured
// with a fake one) rather than reassigning the same field to itself; app.rebrick
// is a single shared client, not a per-call decision, so there is nothing to do
// in the common case. A broken personal key isn't distinguished from any other
// Rebrickable failure (see myRebrickableKeyScreen's doc comment).
func (a *App) applyUserRebrickableKey() {
	if a.session == nil || a.rebrick == nil {
		return
	}
	p, ok := loadPrefs()[a.session.Username]
	if !ok || p.RebrickableKey == "" {
		return
	}
	a.rebrick.APIKey = p.RebrickableKey
}
