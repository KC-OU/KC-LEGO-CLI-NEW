package uiapp

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/notify"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

// Admin → Notifications: which channels exist (notify's provider file plus
// ntfy), where each event goes, and a test of every channel.

const (
	scrNotify      = "notify"
	scrNotifyRoute = "notify_route"
)

func notifyScreens() map[string]screenModel {
	return map[string]screenModel{
		scrNotify:      &selectList{panelID: "NOTIFY", title: "Notifications", rows: notifyRows, keys: notifyKeys, hint: "↑/↓ event  Enter change its channels  T test every channel"},
		scrNotifyRoute: notifyRouteScreen(),
	}
}

func notifyRows(app *App) ([]string, [][]string, []string) {
	chans, err := notify.Channels()
	if err != nil {
		app.setMsgIfEmpty("Provider file not used: " + err.Error())
	}
	if len(chans) == 0 {
		app.setMsgIfEmpty("No channels yet: add notify's provider-config.yaml (see the Notifications guide) or set NOTIFY_URL.")
	}
	var names []string
	for _, c := range chans {
		names = append(names, c.ID+" ("+c.Kind+")")
	}
	var rows [][]string
	for _, e := range notify.Events {
		var ids []string
		for _, c := range notify.Route(e, chans) {
			ids = append(ids, c.ID)
		}
		to := strings.Join(ids, ", ")
		if p, err := access.Load(); err == nil && len(p.Settings.NotifyRoutes[e]) == 0 && len(ids) > 0 {
			to = "all: " + to
		}
		rows = append(rows, []string{e, orDash(to)})
	}
	return []string{"Event", "Goes to   (channels: " + orDash(strings.Join(names, ", ")) + ")"}, rows, notify.Events
}

func notifyKeys(app *App, event string, msg tea.KeyMsg) {
	switch {
	case msg.Type == tea.KeyEnter && event != "":
		app.ws().set = event // reuse the workshop focus slot for the event name
		app.goTo(scrNotifyRoute)
	case isKey(msg, 't'):
		chans, _ := notify.Channels()
		var res []string
		bad := false
		for _, c := range chans {
			if err := c.Send("KC-PARTS test alert\nSent from the admin panel by " + app.userName() + "."); err != nil {
				res = append(res, c.ID+" FAILED: "+err.Error())
				bad = true
			} else {
				res = append(res, c.ID+" ok")
			}
		}
		if len(res) == 0 {
			res = []string{"no channels configured"}
			bad = true
		}
		app.audit.Log(app.userName(), "", "NOTIFY_TEST", map[bool]string{true: "FAILED", false: "SUCCESS"}[bad], strings.Join(res, "; "))
		app.setMsg("Test: "+strings.Join(res, " · "), bad)
	}
}

func notifyRouteScreen() screenModel {
	return &formScreen{
		panelID: "NTFRTE",
		title:   "Route an Event",
		build: func(app *App) []ui.Field {
			ev := app.ws().set
			cur := "all"
			if p, err := access.Load(); err == nil && len(p.Settings.NotifyRoutes[ev]) > 0 {
				cur = strings.Join(p.Settings.NotifyRoutes[ev], ",")
			}
			return []ui.Field{{Label: "Event", Value: ev, Protected: true}, {Label: "Channels (ids, comma-separated, or all)", Value: cur}}
		},
		submit: func(app *App, v []string) {
			ev, ids := v[0], splitCSV(v[1])
			all := len(ids) == 0 || (len(ids) == 1 && ids[0] == "all")
			if app.saveAccess(fmt.Sprintf("notify route %s = %s", ev, v[1]), func(p *access.Policy) error {
				if p.Settings.NotifyRoutes == nil {
					p.Settings.NotifyRoutes = map[string][]string{}
				}
				if all {
					delete(p.Settings.NotifyRoutes, ev)
				} else {
					p.Settings.NotifyRoutes[ev] = ids
				}
				return nil
			}) {
				app.onBack()
			}
		},
	}
}
