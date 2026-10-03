package uiapp

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

// Admin → Recent Activity: a scannable feed of what's happened — messages,
// accuracy docks/escalations, missing-parts, forced-off/reassigned jobs (see
// lego.DB.LogEvent's call sites) — distinct from the tamper-evident audit log
// (internal/audit), which is a hash-chained security record, not a UI feed.

const (
	scrAdminEvents      = "admin_events"
	scrAdminEventsClear = "admin_events_clear"
)

func adminEventsScreen() screenModel {
	return &tableScreen{
		panelID: "ADMEVT",
		title:   "Recent Activity",
		columns: []string{"When", "Kind", "Who", "Detail"},
		fetch: func(app *App) ([][]string, string, error) {
			evs, err := app.legoDB.AdminEvents(50)
			rows := make([][]string, len(evs))
			for i, e := range evs {
				who := e.Actor
				if e.Target != "" && e.Target != e.Actor {
					who += " -> " + e.Target
				}
				rows[i] = []string{e.CreatedAt.Format("Jan 2 15:04"), e.Kind, who, e.Detail}
			}
			return rows, fmt.Sprintf("%d event(s) — C clears this feed", len(rows)), err
		},
		extra: func(app *App, msg tea.KeyMsg) {
			if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && (msg.Runes[0] == 'c' || msg.Runes[0] == 'C') {
				app.goTo(scrAdminEventsClear)
			}
		},
	}
}

// adminEventsClearScreen permanently empties the feed for every admin — the
// same "type yes to confirm" shape as usersDeleteScreen (users_screens.go),
// reached only through adminEventsScreen's own 'C' key, which is itself only
// reachable through the already-gated hub entry (hub.go) — the same
// closed-navigation-graph trust usersDeleteScreen relies on, not a second
// permission check here.
func adminEventsClearScreen() screenModel {
	return &formScreen{
		panelID: "ADMEVC",
		title:   "Clear Recent Activity",
		preamble: func(app *App) string {
			return app.theme.Muted.Render("This deletes the feed for every admin and cannot be undone.")
		},
		build: func(app *App) []ui.Field {
			return []ui.Field{{Label: `Type "yes" to confirm`}}
		},
		submit: func(app *App, values []string) {
			if values[0] != "yes" {
				app.setMsg(`Cancelled — type "yes" to confirm.`, true)
				return
			}
			if err := app.legoDB.ClearAdminEvents(); err != nil {
				app.setMsg(err.Error(), true)
				return
			}
			app.audit.Log(app.session.Username, app.session.Role, "ADMIN_EVENTS_CLEARED", "SUCCESS", "")
			app.setMsg("Recent Activity cleared.", false)
			app.onBack()
		},
	}
}
