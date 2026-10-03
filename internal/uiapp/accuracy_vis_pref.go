package uiapp

// Whether "My Accuracy" shows on a picker/checker's own hub menu (see
// picker_hub.go) — a display preference only, same on-by-default,
// per-user, palette-reachable shape as loading_pref.go's spinner toggle.
// Turning it off never touches the accuracy data itself or what an admin
// sees; it only removes the menu item for this one person.

const scrMyAccuracyVis = "my_accuracy_vis"

func accuracyVisPrefScreen() screenModel {
	return &menuScreen{
		panelID: "MYACVS",
		title:   "My Accuracy — Menu Visibility",
		options: func(app *App) []menuOption {
			return []menuOption{
				{Key: "1", Label: "Show — keep My Accuracy on my hub menu", Go: func(app *App) { setAccuracyVisPref(app, true) }},
				{Key: "2", Label: "Hide — remove it from my hub menu", Go: func(app *App) { setAccuracyVisPref(app, false) }},
				{Key: "0", Label: "Return", Go: func(app *App) { app.onBack() }},
			}
		},
		intro: func(app *App) string {
			user := ""
			if app.session != nil {
				user = app.session.Username
			}
			state := "hidden"
			if myAccuracyVisible(user) {
				state = "shown"
			}
			return app.theme.Muted.Render("Currently: " + state)
		},
	}
}

func setAccuracyVisPref(app *App, show bool) {
	user := ""
	if app.session != nil {
		user = app.session.Username
	}
	if err := savePrefField(user, func(p *userPref) { p.HideMyAccuracy = !show }); err != nil {
		app.setMsg(err.Error(), true)
		return
	}
	state := "hidden"
	if show {
		state = "shown"
	}
	role := ""
	if app.session != nil {
		role = app.session.Role
	}
	app.audit.Log(user, role, "USER_ACCURACY_VIS_PREF", "SUCCESS", state)
	app.setMsg("My Accuracy is now "+state+" on your hub menu.", false)
}
