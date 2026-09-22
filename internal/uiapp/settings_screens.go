package uiapp

import (
	"fmt"
	"strings"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/api"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/twofa"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

// maskSecret shows only the last 4 characters of a stored secret — enough
// for an admin to recognize "yes, that's the current key" without the full
// value round-tripping to the terminal on every visit to this screen.
func maskSecret(s string) string {
	if s == "" {
		return "(not set)"
	}
	if len(s) <= 4 {
		return strings.Repeat("*", len(s))
	}
	return strings.Repeat("*", len(s)-4) + s[len(s)-4:]
}

// settingsHubScreen is the only screen in this file gated directly (via
// adminGated); its children are only reachable by navigating through it, the
// same closed-navigation-graph trust the Users screens already rely on.
func settingsHubScreen() screenModel {
	return &menuScreen{
		panelID:      "SETMGT",
		title:        "Settings & API Keys",
		adminGated:   true,
		deniedAction: "SETTINGS_ACCESS",
		options: func(app *App) []menuOption {
			return []menuOption{
				{Key: "1", Label: "Rebrickable API Key", Go: func(app *App) { app.goTo(scrSettingsRebrickable) }},
				{Key: "2", Label: "Sync Dashboard Admin Login", Go: func(app *App) { app.goTo(scrSettingsSyncAdmin) }},
				{Key: "3", Label: "Two-Factor Authentication (view/disable)", Go: func(app *App) { app.goTo(scrSettings2FA) }},
				{Key: "4", Label: "Part-DB API Token (for adding parts)", Go: func(app *App) { app.goTo(scrSettingsPartDB) }},
				{Key: "5", Label: "Display Theme (colours, high contrast, colour-blind)", Go: func(app *App) { app.goTo(scrSettingsTheme) }},
				{Key: "6", Label: "BrickLink API (lookups by number, prices)", Go: func(app *App) { app.goTo(scrSettingsBrickLink) }},
				{Key: "0", Label: "Return", Go: func(app *App) { app.onBack() }},
			}
		},
	}
}

func settingsRebrickableScreen() screenModel {
	return &formScreen{
		panelID: "SETRBK",
		title:   "Rebrickable API Key",
		build: func(app *App) []ui.Field {
			return []ui.Field{
				{Label: "Current key", Value: maskSecret(config.Get(config.RebrickableAPIKey)), Protected: true},
				{Label: "New key (blank = keep unchanged)", Password: true},
			}
		},
		submit: func(app *App, values []string) {
			newKey := values[1]
			if newKey == "" {
				app.setMsg("No change made.", false)
				app.onBack()
				return
			}
			if err := config.SetOverride(config.RebrickableAPIKey, newKey); err != nil {
				app.setMsg(err.Error(), true)
				return
			}
			app.rebrick = lego.NewClientFor(app.legoDB) // pick up the new key for the rest of this session immediately
			app.audit.Log(app.session.Username, app.session.Role, "SETTINGS_CHANGED", "SUCCESS", "rebrickable_api_key")
			app.setMsg("Rebrickable API key updated.", false)
			app.onBack()
		},
	}
}

func settingsSyncAdminScreen() screenModel {
	return &formScreen{
		panelID: "SETSYN",
		title:   "Sync Dashboard Admin Login",
		build: func(app *App) []ui.Field {
			username, err := api.AdminUsername()
			if err != nil {
				username = "(not set yet — enter one below)"
			}
			return []ui.Field{
				{Label: "Current username", Value: username, Protected: true},
				{Label: "New username (blank = keep)"},
				{Label: "New password (blank = keep)", Password: true},
			}
		},
		submit: func(app *App, values []string) {
			newUser, newPass := values[1], values[2]
			if newUser == "" && newPass == "" {
				app.setMsg("No change made.", false)
				app.onBack()
				return
			}
			if newPass == "" {
				app.setMsg("A password is required to change the sync admin account (it's re-hashed together with the username).", true)
				return
			}
			username := newUser
			if username == "" {
				if cur, err := api.AdminUsername(); err == nil {
					username = cur
				} else {
					username = config.Get(config.SyncAdminUser)
				}
			}
			if err := api.SetAdminCredentials(username, newPass); err != nil {
				app.setMsg(err.Error(), true)
				return
			}
			app.audit.Log(app.session.Username, app.session.Role, "SETTINGS_CHANGED", "SUCCESS", "sync_admin_credentials user="+username)
			app.setMsg(fmt.Sprintf("Sync dashboard admin login updated (user: %s).", username), false)
			app.onBack()
		},
	}
}

func settings2FAScreen() screenModel {
	return &formScreen{
		panelID: "SET2FA",
		title:   "Two-Factor Authentication",
		build: func(app *App) []ui.Field {
			return []ui.Field{
				{Label: "Username"},
				{Label: "Source (modernwms/partdb, blank = modernwms)"},
				{Label: `Type "disable" to remove 2FA, "unlock" to clear a lockout (blank = check status)`},
			}
		},
		submit: func(app *App, values []string) {
			username, source, action := values[0], values[1], strings.ToLower(values[2])
			if username == "" {
				app.setMsg("Username is required.", true)
				return
			}
			if source == "" {
				source = "modernwms"
			}
			if action == "disable" {
				if err := twofa.Disable(username, source); err != nil {
					app.setMsg(err.Error(), true)
					return
				}
				app.audit.Log(app.session.Username, app.session.Role, "SETTINGS_2FA_DISABLED", "SUCCESS", username+" ("+source+")")
				app.setMsg(fmt.Sprintf("2FA disabled for %s (%s).", username, source), false)
				app.onBack()
				return
			}
			if action == "unlock" {
				if err := twofa.Unlock(username, source); err != nil {
					app.setMsg(err.Error(), true)
					return
				}
				app.audit.Log(app.session.Username, app.session.Role, "SETTINGS_2FA_UNLOCKED", "SUCCESS", username+" ("+source+")")
				app.setMsg(fmt.Sprintf("2FA lockout cleared for %s (%s).", username, source), false)
				app.screens[scrSettings2FA].OnEnter(app)
				return
			}
			if twofa.IsEnabled(username, source) {
				status := fmt.Sprintf("%s (%s): 2FA ENABLED, %d backup codes remaining.", username, source, twofa.RemainingBackupCodes(username, source))
				if until, locked := twofa.Locked(username, source); locked {
					status += " LOCKED until " + until.Format("15:04") + " (type unlock to clear)."
				}
				app.setMsg(status, false)
			} else {
				app.setMsg(fmt.Sprintf("%s (%s): 2FA disabled. Enroll from a shell: wms users 2fa enable %s --source %s", username, source, username, source), false)
			}
			app.screens[scrSettings2FA].OnEnter(app) // stay on this screen with a fresh blank form, same pattern as an invalid login/2FA code
		},
	}
}

// settingsPartDBScreen stores the Part-DB REST API token. Parts are created
// and changed through that API (never by writing Part-DB's database file), so
// this token is what makes "Add / Update a Part" reach Part-DB. Saving tests it
// straight away, and a blank entry just re-tests the token that is set.
func settingsPartDBScreen() screenModel {
	return &formScreen{
		panelID: "SETPDB",
		title:   "Part-DB API Token",
		build: func(app *App) []ui.Field {
			return []ui.Field{
				{Label: "Part-DB API address", Value: config.Get(config.PartDBAPIURL), Protected: true},
				{Label: "Current token", Value: maskSecret(config.Get(config.PartDBAPIToken)), Protected: true},
				{Label: "New token (blank = test the current one)", Password: true},
			}
		},
		submit: func(app *App, values []string) {
			token := strings.TrimSpace(values[2])
			if token != "" {
				if err := config.SetOverride(config.PartDBAPIToken, token); err != nil {
					app.setMsg(err.Error(), true)
					return
				}
				app.audit.Log(app.session.Username, app.session.Role, "SETTINGS_CHANGED", "SUCCESS", "partdb_api_token")
			}
			ctx, cancel := app.pdbw.Ctx()
			defer cancel()
			err := app.pdbw.API().Ping(ctx)
			switch {
			case err == nil && token != "":
				app.setMsg("Part-DB API token saved, and Part-DB accepted it.", false)
			case err == nil:
				app.setMsg("The current Part-DB API token works.", false)
			case token != "":
				app.setMsg("Token saved, but Part-DB did not accept it: "+err.Error(), true)
			default:
				app.setMsg(err.Error(), true)
				return
			}
			app.onBack()
		},
	}
}
