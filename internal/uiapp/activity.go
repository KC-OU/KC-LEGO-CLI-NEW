package uiapp

import "fmt"

// Admin → Recent Activity: a scannable feed of what's happened — messages,
// accuracy docks/escalations, missing-parts, forced-off/reassigned jobs (see
// lego.DB.LogEvent's call sites) — distinct from the tamper-evident audit log
// (internal/audit), which is a hash-chained security record, not a UI feed.

const scrAdminEvents = "admin_events"

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
			return rows, fmt.Sprintf("%d event(s)", len(rows)), err
		},
	}
}
