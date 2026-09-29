package uiapp

import (
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/auth"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

// The V key: a picker/checker who needs to do one admin thing (assign work,
// dock someone's accuracy) can switch straight to an admin account and straight
// back, without signing out — and, deliberately, without that admin account's
// own 2FA: this is a second identity being verified from inside an already-
// authenticated session, the same trust model the "abandon a job" admin
// sign-off already uses (see finishAbandon/verifyAdmin, tickets_screens.go),
// not a fresh network login. Every switch and every return is audited either
// way. Someone whose OWN account already holds both a picker/checker permission
// and real admin access never sees the credentials prompt at all — V just
// jumps straight to Admin and back, like toggleLego's G key.

const scrSwitchAdmin = "switch_admin"

// parkedSession is the identity V set aside to switch to a different one —
// restored exactly as it was, no re-authentication, when V is pressed again.
type parkedSession struct {
	session   *auth.Session
	pUser     *access.User
	perms     access.Perms
	cur       string
	stack     []string
	activeTab string
}

func (a *App) toggleAdminView() {
	if a.session == nil || !a.authed {
		return
	}
	if a.parked != nil {
		a.restoreParkedSession()
		return
	}
	if anyModuleAllowed(a, "user_mgmt", "settings", "docker", "access") {
		// This account already has admin-level access itself: no identity
		// switch needed, just show the Admin screens and back — same round
		// trip as toggleLego's G key, direction picked by where V was pressed.
		a.stack = nil
		if a.cur == scrPickerHub {
			a.activeTab = "9"
			a.cur = scrAdminHub
			a.setMsg("Switching to Admin...", false)
		} else {
			a.activeTab = "1"
			a.cur = scrPickerHub
			a.setMsg("Back to your Picker/Checker screen...", false)
		}
		if scr, ok := a.screens[a.cur]; ok {
			scr.OnEnter(a)
		}
		return
	}
	a.goTo(scrSwitchAdmin)
}

// restoreParkedSession switches back to the parked identity — always allowed,
// never needs credentials: stepping back down to a lower-privilege session
// this process already verified once is not a security decision.
func (a *App) restoreParkedSession() {
	p := a.parked
	a.session, a.pUser, a.perms = p.session, p.pUser, p.perms
	a.legoDB.SetActor(p.session.Username)
	a.audit.Log(p.session.Username, p.session.Role, "ADMIN_QUICK_SWITCH", "SUCCESS", "returned from admin switch")
	a.parked = nil
	a.stack = p.stack
	a.activeTab = p.activeTab
	a.cur = p.cur
	if scr, ok := a.screens[a.cur]; ok {
		scr.OnEnter(a)
	}
	a.setMsg("Back to "+p.session.Username+".", false)
}

func switchAdminScreen() screenModel {
	return &formScreen{
		panelID: "SWADM",
		title:   "Switch to Admin",
		preamble: func(app *App) string {
			return app.theme.Muted.Render("An admin account's own username and password — no authenticator code needed, this session is already signed in.")
		},
		build: func(app *App) []ui.Field {
			return []ui.Field{{Label: "Admin username"}, {Label: "Admin password", Password: true}}
		},
		submit: func(app *App, values []string) {
			username, password := values[0], values[1]
			session, err := auth.AuthenticateUser(app.ctx(), app.wms, app.pdb, username, password)
			if err != nil || session == nil {
				app.audit.Log(username, "", "ADMIN_QUICK_SWITCH", "DENIED", "bad credentials")
				app.setMsg("Invalid admin username or password.", true)
				return
			}
			was := &parkedSession{session: app.session, pUser: app.pUser, perms: app.perms, cur: app.landingScreen(), stack: nil, activeTab: "1"}

			app.session, app.pUser, app.perms = session, nil, access.Perms{}
			app.loadPolicy() // recomputes pUser/perms for the new session, not the one being parked
			if !anyModuleAllowed(app, "user_mgmt", "settings", "docker", "access") {
				app.session, app.pUser, app.perms = was.session, was.pUser, was.perms
				app.audit.Log(username, session.Role, "ADMIN_QUICK_SWITCH", "DENIED", "signed in, but not an admin-level account")
				app.setMsg(username+" doesn't have admin-level access.", true)
				return
			}

			app.parked = was
			app.legoDB.SetActor(session.Username)
			app.audit.Log(session.Username, session.Role, "ADMIN_QUICK_SWITCH", "SUCCESS", "from "+was.session.Username+", no 2FA (already an authenticated session)")
			app.stack = nil
			app.activeTab = "9"
			app.cur = scrAdminHub
			app.screens[scrAdminHub].OnEnter(app)
			app.setMsg("Switched to "+session.Username+" (admin) — V to go back to "+was.session.Username+".", false)
		},
	}
}
