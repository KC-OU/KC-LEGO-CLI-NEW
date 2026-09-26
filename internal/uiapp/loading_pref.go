package uiapp

// Whether startBusy's spinner shows for genuinely slow calls (sign-on, Part-DB sync,
// price look-ups) — on by default, per-user, reachable from the command palette (Ctrl-K
// / F2) exactly like "My display theme" is, not from any menu. Turning it off never
// brings back the freeze those calls used to cause: they still run off the key-handling
// path either way (see startBusy, app.go) — off only means no spinner text.

const scrMyLoading = "my_loading"

func loadingPrefScreen() screenModel {
	return &menuScreen{
		panelID: "MYLOAD",
		title:   "Loading Indicator",
		options: func(app *App) []menuOption {
			return []menuOption{
				{Key: "1", Label: "Turn on — show a spinner during sign-on, Part-DB sync, price look-ups", Go: func(app *App) { setLoadingPref(app, true) }},
				{Key: "2", Label: "Turn off — no spinner (these calls still won't freeze the screen)", Go: func(app *App) { setLoadingPref(app, false) }},
				{Key: "0", Label: "Return", Go: func(app *App) { app.onBack() }},
			}
		},
		intro: func(app *App) string {
			user := ""
			if app.session != nil {
				user = app.session.Username
			}
			state := "off"
			if loadingEnabled(user) {
				state = "on"
			}
			return app.theme.Muted.Render("Currently: " + state)
		},
	}
}

func setLoadingPref(app *App, on bool) {
	user := ""
	if app.session != nil {
		user = app.session.Username
	}
	if err := savePrefField(user, func(p *userPref) { p.LoadingOff = !on }); err != nil {
		app.setMsg(err.Error(), true)
		return
	}
	state := "off"
	if on {
		state = "on"
	}
	role := ""
	if app.session != nil {
		role = app.session.Role
	}
	app.audit.Log(user, role, "USER_LOADING_PREF", "SUCCESS", state)
	app.setMsg("Loading indicator is now "+state+".", false)
}
