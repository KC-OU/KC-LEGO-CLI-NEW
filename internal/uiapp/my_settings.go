package uiapp

import (
	"fmt"
	"strings"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

// A per-user settings menu: display theme and the loading indicator already had
// their own screens (reachable only from the command palette); this adds a real
// menu entry that gathers those with the new personal API key override. Part-DB
// and BrickLink credentials stay shared-only for now — nobody asked for a
// personal override there yet, and BrickLink's OAuth secret is not something to
// invite someone to paste into a picker's own settings screen without a reason to.

const (
	scrMySettings     = "my_settings"
	scrMyRebrickKey   = "my_rebrickable_key"
	scrMyRebrickEdit  = "my_rebrickable_key_edit"
	scrFeatureRequest = "feature_request"
	scrMessageAdmin   = "message_admin"
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
				{Key: "5", Label: "Request a feature or improvement", Go: func(app *App) { app.goTo(scrFeatureRequest) }},
				{Key: "6", Label: "Message an admin", Go: func(app *App) { app.goTo(scrMessageAdmin) }},
				{Key: "7", Label: "Show/hide My Accuracy on my hub menu", Go: func(app *App) { app.goTo(scrMyAccuracyVis) }},
				{Key: "8", Label: "Show/hide pictures while checking or picking", Go: func(app *App) { app.goTo(scrMyPictures) }},
				{Key: "0", Label: "Return", Go: func(app *App) { app.onBack() }},
			}
		},
		intro: func(app *App) string {
			return app.theme.Muted.Render("Part-DB and BrickLink credentials use the shared instance-wide keys.")
		},
	}
}

// logAdminNote records something a non-admin sent an admin's way — a feature
// request, a message, anything that isn't itself a database-backed action —
// through the same activity feed and notification routing (Discord, Slack,
// ...) every other admin-relevant event already uses, rather than a new
// delivery mechanism for each one.
func logAdminNote(app *App, kind, auditAction, detail, notifyMsg string) {
	user, role := "", ""
	if app.session != nil {
		user, role = app.session.Username, app.session.Role
	}
	_ = app.legoDB.LogEvent(kind, user, "", detail)
	app.notifyEvent(kind, notifyMsg, "")
	app.audit.Log(user, role, auditAction, "SUCCESS", detail)
}

// ---- Request a feature or improvement ----
//
// Generalizes what used to be a theme-only request (the theme picker's 'R'
// still lands here): a theme, a feature, a fix, anything worth an admin
// knowing about without them having to go looking for it.

func featureRequestScreen() screenModel {
	return &formScreen{
		panelID: "FEATREQ",
		title:   "Request a Feature",
		preamble: func(app *App) string {
			return app.theme.Muted.Render("A theme, a feature, a fix — anything that'd make this more useful. Tell an admin.")
		},
		build: func(app *App) []ui.Field {
			return []ui.Field{{Label: "What would you like?"}, {Label: "Details or a link (optional)"}}
		},
		submit: func(app *App, v []string) {
			what, detail := strings.TrimSpace(v[0]), strings.TrimSpace(v[1])
			if what == "" {
				app.setMsg("Enter what you'd like.", true)
				return
			}
			full := what
			if detail != "" {
				full += ": " + detail
			}
			logAdminNote(app, lego.EventFeatureRequest, "FEATURE_REQUESTED", full, fmt.Sprintf("%s requested: %s", app.userName(), full))
			app.setMsg("Sent — an admin will see it in Recent Activity.", false)
			app.onBack()
		},
	}
}

// ---- Message an admin ----
//
// The reverse of the admin's existing "Message a User": a picker/checker
// explaining a delay or anything else worth flagging, with nowhere to sign
// in and act on it themselves. There's no single "admin" account to queue
// this to (admin is a role, not a person), so it's a Recent Activity entry
// plus a notification, not a per-recipient inbox message.

func messageAdminScreen() screenModel {
	return &formScreen{
		panelID: "MSGADM",
		title:   "Message an Admin",
		preamble: func(app *App) string {
			return app.theme.Muted.Render("Running late, need help, anything an admin should know — it'll show up in Recent Activity.")
		},
		build: func(app *App) []ui.Field {
			return []ui.Field{{Label: "Message"}}
		},
		submit: func(app *App, v []string) {
			body := strings.TrimSpace(v[0])
			if body == "" {
				app.setMsg("Enter a message.", true)
				return
			}
			detail := "to admins: " + body
			logAdminNote(app, lego.EventMessage, "MESSAGE_TO_ADMIN", detail, fmt.Sprintf("%s: %s", app.userName(), body))
			app.setMsg("Sent to admins.", false)
			app.onBack()
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
