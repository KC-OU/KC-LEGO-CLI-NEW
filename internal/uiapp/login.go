package uiapp

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/auth"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/twofa"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

func loginFields(app *App) []ui.Field {
	userLabel := "Username / User ID"
	if app.theme.Classic {
		userLabel = "Username / User ID (or 'q' to Quit)"
	}
	return []ui.Field{
		{Label: userLabel},
		{Label: "Password", Password: true},
	}
}

// loginPanel is the classic sign-on card: login_prompt's
// render_panel(..., title="ModernWMS & PartDB Terminal Suite - Secure Login",
// style="brand"), full width with the title inside the top border.
func loginPanel(app *App) string {
	t := app.theme
	return ui.RenderPanel(t, t.Brand, "ModernWMS & PartDB Terminal Suite - Secure Login",
		"Login using ModernWMS or PartDB account credentials.")
}

// cleartextNote warns, on the sign-on screen only, that a telnet session is not
// encrypted. From a private or loopback address it is a quiet reminder; from a
// public one it is a warning, because the password and 2FA code cross the
// internet readable by anyone on the path.
func cleartextNote(app *App) string {
	if app.transport != "telnet" {
		return ""
	}
	t := app.theme
	if ip := net.ParseIP(app.remoteAddr); ip != nil && !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsLinkLocalUnicast() {
		return t.Danger.Render("WARNING: telnet is not encrypted - your password and 2FA code can be read on the way. Use the web terminal (HTTPS) from outside your network.")
	}
	return t.Muted.Render("Note: telnet is not encrypted - use it only on a network you trust.")
}

// The sign-on screens are built once at registration time (buildScreens),
// because screenModel.Title() has no app parameter to consult
// app.theme.Classic per render. In the classic layout they are "bare": just a
// card, an optional status line and inline prompts, like the Python original
// where none of the header/tab bar/legend chrome exists before login.

func loginScreen(app *App) screenModel {
	s := &formScreen{panelID: "SIGNON", title: "ModernWMS & Part-DB Sign On", build: loginFields, submit: doLoginSubmit, preamble: signOnPreamble}
	if app.theme.Classic {
		s.title = "ModernWMS & PartDB Terminal Suite - Secure Login"
		s.bare = true
		s.preamble = func(app *App) string {
			t := app.theme
			// The control room, then the quit hint ("tap Quit" is real in touch mode:
			// App.handleMouse quits on a tap of this row).
			return signOnPreamble(app) + "\n  " + t.Danger.Render("[Q] Quit / Exit Suite") + " " + t.Muted.Render("(Press Q / ESC / tap Quit anytime to exit)")
		}
	}
	return s
}

func twoFAScreen(app *App) screenModel {
	s := &formScreen{panelID: "SIGNON2", title: "Two-Factor Authentication", build: twoFACodeFields, submit: doTwoFACodeSubmit}
	if app.theme.Classic {
		s.bare = true
		s.preamble = func(app *App) string {
			return ui.RenderPanel(app.theme, app.theme.Brand, "Two-Factor Authentication",
				"Enter the 6-digit code from your authenticator app (or an unused backup code).")
		}
	}
	return s
}

func forcedChangeScreen(app *App) screenModel {
	s := &formScreen{panelID: "CHGPWD", title: "Password Change Required", build: forcedChangeFields, submit: doForcedChangeSubmit}
	if app.theme.Classic {
		s.bare = true
		s.preamble = func(app *App) string {
			src := "ModernWMS"
			if app.session != nil {
				src = srcLabel(app.session.Source)
			}
			// Verbatim from forced_password_change_prompt.
			return ui.RenderPanel(app.theme, app.theme.Warning, "First-Time Login: Password Change Required",
				"You are logged in with a temporary password on "+src+".\nFor security compliance, you must choose a new permanent password.")
		}
	}
	return s
}

// doLoginSubmit mirrors modernwms_tui.py's login_prompt: 5 attempts before
// forced exit, ModernWMS-then-Part-DB auth via internal/auth, then a
// forced-password-change detour if either backend's flag requires it.
func doLoginSubmit(app *App, values []string) {
	username, password := values[0], values[1]
	if username == "" {
		app.setMsg("Username is required.", true)
		return
	}

	session, err := auth.AuthenticateUser(app.ctx(), app.wms, app.pdb, username, password)
	if err != nil {
		app.loginAttempts++
		status := "FAILED_INVALID_CREDENTIALS"
		if errors.Is(err, auth.ErrAccountDisabled) {
			status = "FAILED_ACCOUNT_DISABLED"
		}
		app.audit.Log(username, "", "LOGIN", status, err.Error())
		if app.loginAttempts >= 5 {
			app.setMsg("Maximum failed login attempts exceeded.", true)
			app.lockedOut = true
			app.quitting = true
			return
		}
		if app.theme.Classic {
			app.setMsg(fmt.Sprintf("Authentication failed! (%d/5 attempts)", app.loginAttempts), true)
		} else {
			app.setMsg(fmt.Sprintf("Invalid username or password. (%d/5 attempts)", app.loginAttempts), true)
		}
		app.screens[scrLogin].OnEnter(app)
		return
	}

	app.session = session
	app.loginAttempts = 0
	action := "LOGIN_MODERNWMS"
	if session.Source == "partdb" {
		action = "LOGIN_PARTDB"
	}
	app.audit.Log(session.Username, session.Role, action, "SUCCESS", "from "+app.origin())
	continueSignOn(app, session)
}

// continueSignOn is what happens after the password is accepted: the 2FA
// prompt — unless this origin passed a real code within the grace window —
// then the forced-password-change detour, then the hub.
func continueSignOn(app *App, session *auth.Session) {
	app.loadPolicy()
	u := app.pUser
	if u.Expired(app.now()) {
		app.denySignOn("LOGIN_DENIED_EXPIRED", "This account's access ended on "+u.Expires+". Ask an admin.")
		return
	}
	if !u.ChannelAllowed(app.channel()) {
		app.denySignOn("LOGIN_DENIED_CHANNEL", "This account may not sign in over "+app.channel()+" (allowed: "+strings.Join(u.Channels, ", ")+").")
		return
	}
	if ok, why := u.ExemptFrom(app.remoteAddr); ok {
		app.audit.Log(session.Username, session.Role, "LOGIN_2FA_EXEMPT", "SUCCESS", why+" via "+app.channel())
		proceedPastAuth(app, session)
		return
	} else if why != "" {
		app.audit.Log(session.Username, session.Role, "LOGIN_2FA_EXEMPT", "DENIED", why+"; asking for 2FA")
	}
	if twofa.IsEnabled(session.Username, session.Source) {
		if ago, ok := twofa.WithinGrace(session.Username, session.Source, app.graceOrigin, app.graceWindow()); ok {
			mins, window := int(ago.Round(time.Minute)/time.Minute), int(app.graceWindow()/time.Minute)
			app.audit.Log(session.Username, session.Role, "LOGIN_2FA_GRACE", "SUCCESS",
				fmt.Sprintf("code verified %dm ago from %s (window %dm)", mins, app.graceOrigin, window))
			proceedPastAuth(app, session)
			// goTo/enterHub clear the message line, so note the skipped code after.
			note := fmt.Sprintf("2FA code not needed: one was verified %d min ago from this address (window: %d min).", mins, window)
			if app.message != "" {
				note = app.message + " " + note
			}
			app.setMsg(note, false)
			return
		}
		app.goTo(scrTwoFACode)
		return
	}
	if app.requireTwoFA || (u != nil && u.TwoFA == access.TwoFARequired) {
		app.audit.Log(session.Username, session.Role, "LOGIN_DENIED_2FA_REQUIRED", "DENIED", "2FA required ("+app.channel()+"), none enrolled")
		app.session = nil
		app.setMsg("2FA is required for remote access. Ask an admin to run: wms users 2fa enable "+session.Username, true)
		app.screens[scrLogin].OnEnter(app)
		return
	}
	proceedPastAuth(app, session)
}

// proceedPastAuth is the shared continuation after primary auth (and 2FA,
// when enrolled) both succeed: the forced-password-change detour, then hub.
func proceedPastAuth(app *App, session *auth.Session) {
	if mustChangePassword(app, session) {
		app.goTo(scrForcedChange)
		return
	}
	app.enterHub()
}

func twoFACodeFields(app *App) []ui.Field {
	return []ui.Field{{Label: "Authenticator Code", Password: true}}
}

// doTwoFACodeSubmit mirrors the plan's login-flow spec: wrong code stays on
// this screen (not back to the password field) with a row-23 error; a
// consumed backup code surfaces a remaining-count warning so the user
// notices before they run out.
func doTwoFACodeSubmit(app *App, values []string) {
	code := values[0]
	usedBackup, err := twofa.Verify(app.session.Username, app.session.Source, code)
	if err != nil {
		var locked *twofa.LockedError
		msg, action := "Invalid authenticator code.", "LOGIN_2FA_FAILED"
		switch {
		case errors.Is(err, twofa.ErrCodeReused):
			// A right code, used before: not a guess, so it neither counts toward the lockout nor alarms.
			msg, action = "That code was already used. Wait for the next one (about 30 seconds) and try again.", "LOGIN_2FA_REPLAY"
		case errors.As(err, &locked):
			msg, action = err.Error()+". An admin can unlock it: wms users 2fa unlock "+app.session.Username, "LOGIN_2FA_LOCKED"
		}
		app.audit.Log(app.session.Username, app.session.Role, action, "DENIED", err.Error())
		app.setMsg(msg, true)
		app.screens[scrTwoFACode].OnEnter(app)
		return
	}
	app.audit.Log(app.session.Username, app.session.Role, "LOGIN_2FA_SUCCESS", "SUCCESS", "")
	username, source := app.session.Username, app.session.Source
	// A real code was just accepted: start this origin's grace window. A
	// failure to record it only costs a future prompt, so it never blocks login.
	_ = twofa.MarkVerified(username, source, app.graceOrigin, app.graceWindow())
	proceedPastAuth(app, app.session)
	// goTo() above clears the message line for the destination screen, so
	// the backup-code warning is set after navigating, not before.
	if usedBackup {
		remaining := twofa.RemainingBackupCodes(username, source)
		app.setMsg(fmt.Sprintf("Backup code used — %d remaining. Re-enroll soon.", remaining), true)
	}
}

func mustChangePassword(app *App, session *auth.Session) bool {
	if session.Source == "modernwms" {
		mc, _ := app.wms.CheckMustChangePassword(app.ctx(), session.Username)
		return mc
	}
	u, err := app.pdb.GetUserByName(session.Username)
	return err == nil && u != nil && u.NeedPWChange
}

func forcedChangeFields(app *App) []ui.Field {
	if app.theme.Classic {
		return []ui.Field{
			{Label: "Enter New Permanent Password", Password: true},
			{Label: "Confirm New Permanent Password", Password: true},
		}
	}
	return []ui.Field{
		{Label: "New Password", Password: true},
		{Label: "Confirm Password", Password: true},
	}
}

func doForcedChangeSubmit(app *App, values []string) {
	p1, p2 := values[0], values[1]
	if len(p1) < 4 {
		app.setMsg("Password must be at least 4 characters.", true)
		return
	}
	if p1 != p2 {
		app.setMsg("Passwords do not match.", true)
		return
	}

	var err error
	if app.session.Source == "modernwms" {
		err = app.wms.CompleteForcedPasswordChange(app.ctx(), app.session.Username, auth.HashModernWMS(p1))
	} else {
		var hash string
		if hash, err = auth.HashPartDB(p1); err == nil {
			err = app.pdb.ResetPassword(app.session.Username, hash, false)
		}
	}
	if err != nil {
		app.setMsg(err.Error(), true)
		return
	}
	app.audit.Log(app.session.Username, app.session.Role, "PASSWORD_CHANGED_FIRST_LOGIN", "SUCCESS", "")
	app.enterHub()
}

// origin is where this session came from, for the audit log and the last sign-in notice.
func (a *App) origin() string {
	switch {
	case a.transport == "telnet":
		return "telnet " + a.remoteAddr
	case a.touchMode:
		return "web terminal"
	case !a.requireTwoFA:
		return "local console"
	}
	return "gateway"
}

// lastSignInNote is shown after sign-in: when and where you last signed in, and
// any failed attempts on your name since.
func (a *App) lastSignInNote() string {
	if a.session == nil || a.audit == nil {
		return ""
	}
	si, ok := a.audit.LastSignIn(a.session.Username)
	if !ok {
		return "First sign-in recorded for " + a.session.Username + "."
	}
	note := "Last sign-in: " + si.At.Format("Mon 2 Jan 15:04")
	if si.From != "" {
		note += " from " + si.From
	}
	if si.FailedSince > 0 {
		note += fmt.Sprintf(" · %d FAILED attempt(s) since", si.FailedSince)
	}
	return note
}
