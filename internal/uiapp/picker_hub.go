package uiapp

import "github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"

// The dedicated landing hub for a picker or checker (see isPickerOrChecker,
// tickets_screens.go) — a tight, bespoke menu rather than whatever's left over
// after filtering the general ModernWMS/Part-DB hub, since a picker/checker's day
// genuinely doesn't touch most of that hub at all.

const (
	scrPickerHub = "picker_hub"
	scrQueryMenu = "query_menu"
)

func pickerHubScreen() screenModel {
	return &menuScreen{
		panelID: "PIKHUB",
		title:   "Picker / Checker",
		options: func(app *App) []menuOption {
			role := roleForApp(app)
			requestLabel := "Request a set to check"
			if role == lego.AccuracyPicker {
				requestLabel = "Request an order to pick"
			}
			return []menuOption{
				{Key: "1", Label: "Overview", Go: func(app *App) { app.goTo(scrOverview) }},
				{Key: "2", Label: requestLabel, Go: func(app *App) { app.goTo(scrRequest) }},
				{Key: "3", Label: "My Current Jobs", Go: func(app *App) { app.goTo(scrCurrentJob) }},
				{Key: "4", Label: "Menu for Querys", Go: func(app *App) { app.goTo(scrQueryMenu) }},
				{Key: "5", Label: "My accuracy", Go: func(app *App) { app.goTo(scrMyAccuracy) }},
				{Key: "6", Label: "My Exports", Go: func(app *App) { app.goTo(scrMyExports) }, Perm: "exports.download"},
				// checker/picker already carry lego.view/partdb.view by default (see
				// access.go's seed()) — these only need a menu entry to be reachable
				// without knowing Ctrl-K/the palette already has them. Perm-gated like
				// every other option here, so an admin denying it for one person (the
				// Users list's "b" menu-tabs checklist) hides it here too, same as anywhere else.
				{Key: "7", Label: "LEGO Collection", Go: func(app *App) { app.goTo(scrLegoHub) }, Perm: "lego.view"},
				{Key: "8", Label: "Part-DB Hub", Go: func(app *App) { app.goTo(scrPartDBHub) }, Perm: "partdb.view"},
				{Key: "9", Label: "Log out", Go: func(app *App) { app.logout() }},
				{Key: "0", Label: "Exit", Go: func(app *App) { app.quitting = true }},
			}
		},
	}
}

func queryMenuScreen() screenModel {
	return &menuScreen{
		panelID: "QRYMEN",
		title:   "Query a set or part",
		options: func(app *App) []menuOption {
			return []menuOption{
				{Key: "1", Label: "Query a set or part (Ctrl-K)", Go: func(app *App) { app.openPalette() }},
				{Key: "2", Label: "My Owned Parts", Go: func(app *App) { app.goTo(scrLegoPartOwned) }},
				{Key: "3", Label: "My Owned Sets", Go: func(app *App) { app.legoSearchTerm = ""; app.goTo(scrLegoSetResults) }},
				{Key: "0", Label: "Return", Go: func(app *App) { app.onBack() }},
			}
		},
	}
}
