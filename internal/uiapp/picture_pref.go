package uiapp

// Whether the guided check/pick walk shows a part/set picture and a colour
// swatch (see set_check.go's guidedBody) — a display preference only, same
// per-user, palette-reachable shape as accuracy_vis_pref.go's toggle, but
// off by default (a picture costs a live fetch and real terminal space, so
// it opts in rather than changing the screen for everyone).

const scrMyPictures = "my_pictures"

func picturePrefScreen() screenModel {
	return &menuScreen{
		panelID: "MYPICS",
		title:   "My Check/Pick Pictures",
		options: func(app *App) []menuOption {
			return []menuOption{
				{Key: "1", Label: "Show — a picture and colour swatch while I check or pick", Go: func(app *App) { setPicturePref(app, true) }},
				{Key: "2", Label: "Hide — text only, like today", Go: func(app *App) { setPicturePref(app, false) }},
				{Key: "0", Label: "Return", Go: func(app *App) { app.onBack() }},
			}
		},
		intro: func(app *App) string {
			user := ""
			if app.session != nil {
				user = app.session.Username
			}
			state := "hidden"
			if picturesEnabled(user) {
				state = "shown"
			}
			return app.theme.Muted.Render("Currently: " + state + ". Useful if you find a picture faster to recognise than the part number and name — a new part's picture takes a moment to load the first time, then it's instant.")
		},
	}
}

func setPicturePref(app *App, show bool) {
	user := ""
	if app.session != nil {
		user = app.session.Username
	}
	if err := savePrefField(user, func(p *userPref) { p.ShowPictures = show }); err != nil {
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
	app.audit.Log(user, role, "USER_PICTURES_PREF", "SUCCESS", state)
	app.setMsg("Check/pick pictures are now "+state+".", false)
}
