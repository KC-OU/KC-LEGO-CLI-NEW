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

// pickerHubCatalog is every item that can appear on the picker/checker hub —
// customizable the same way the main hub is (per user, per group, or for
// everyone; see menu_catalog.go), with Log out and Exit always pinned at the
// end (never reassignable/hideable — see pickerHubScreen below).
var pickerHubCatalog = []menuCatalogItem{
	{key: "overview", hotkey: "1", label: "Overview", allowed: func(app *App) bool { return true }, open: func(app *App) { app.goTo(scrOverview) }},
	{key: "request", hotkey: "2", label: "Request a set to check", allowed: func(app *App) bool { return true }, open: func(app *App) { app.goTo(scrRequest) }},
	{key: "current_job", hotkey: "3", label: "My Current Jobs", allowed: func(app *App) bool { return true }, open: func(app *App) { app.goTo(scrCurrentJob) }},
	{key: "query_menu", hotkey: "4", label: "Menu for Querys", allowed: func(app *App) bool { return true }, open: func(app *App) { app.goTo(scrQueryMenu) }},
	{key: "my_accuracy", hotkey: "5", label: "My accuracy", allowed: func(app *App) bool { return true }, open: func(app *App) { app.goTo(scrMyAccuracy) }},
	{key: "my_exports", hotkey: "6", label: "My Exports", allowed: func(app *App) bool { return app.can("exports.download") }, open: func(app *App) { app.goTo(scrMyExports) }},
	// checker/picker already carry lego.view/partdb.view by default (see
	// access.go's seed()) — these only need a menu entry to be reachable
	// without knowing Ctrl-K/the palette already has them. Perm-gated like
	// every other item here, so an admin denying it for one person (the
	// Users list's "b" menu-tabs checklist) hides it here too, same as anywhere else.
	{key: "lego", hotkey: "7", label: "LEGO Collection", allowed: func(app *App) bool { return app.can("lego.view") }, open: func(app *App) { app.goTo(scrLegoHub) }},
	{key: "partdb", hotkey: "8", label: "Part-DB Hub", allowed: func(app *App) bool { return app.can("partdb.view") }, open: func(app *App) { app.goTo(scrPartDBHub) }},
	// Reachable to everyone, same as Overview — not gated behind My Settings'
	// own menu (there isn't one; My Settings IS the destination).
	{key: "my_settings", hotkey: "s", label: "My Settings", allowed: func(app *App) bool { return true }, open: func(app *App) { app.goTo(scrMySettings) }},
}

func init() {
	// The one item whose label depends on who's looking — see
	// catalogDynamicLabels' own doc comment (menu_catalog.go).
	catalogDynamicLabels["request"] = func(app *App) string {
		if roleForApp(app) == lego.AccuracyPicker {
			return "Request an order to pick"
		}
		return "Request a set to check"
	}
}

// defaultPickerHubKeys is today's unmodified picker/checker hub — every
// catalog item above, in their original order, plus My Settings appended
// (new: this is what actually fixes "no menu path to Message an Admin").
var defaultPickerHubKeys = []string{
	"overview", "request", "current_job", "query_menu", "my_accuracy", "my_exports", "lego", "partdb", "my_settings",
}

func pickerHubScreen() screenModel {
	return &menuScreen{
		panelID: "PIKHUB",
		title:   "Picker / Checker",
		options: func(app *App) []menuOption {
			opts := resolveAndRenderMenu(app, scrPickerHub, pickerHubCatalog, defaultPickerHubKeys)
			return append(opts,
				menuOption{Key: "9", Label: "Log out", Go: func(app *App) { app.logout() }},
				menuOption{Key: "0", Label: "Exit", Go: func(app *App) { app.quitting = true }},
			)
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
