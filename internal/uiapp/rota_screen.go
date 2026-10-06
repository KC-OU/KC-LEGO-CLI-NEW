package uiapp

import (
	"fmt"
	"time"
)

// Weekly rota view: who's scheduled when, the next 7 days starting today —
// read-only, reached from the Admin hub like Live Sessions or Recent
// Activity. Editing a day's schedule is still the CLI (`wms attendance rota
// set`) for now; this is the "see it all at a glance" half of the feature.
const scrRotaView = "rota_view"

func rotaViewScreen() screenModel {
	return &tableScreen{
		panelID: "ROTA",
		title:   "Weekly Rota",
		columns: []string{"Date", "Who", "Start", "End", "Note"},
		fetch: func(app *App) ([][]string, string, error) {
			var rows [][]string
			scheduled := 0
			for i := 0; i < 7; i++ {
				date := time.Now().AddDate(0, 0, i).Format("2006-01-02")
				label := date
				if i == 0 {
					label = date + " (today)"
				}
				entries, err := app.legoDB.RotaForDate(date)
				if err != nil {
					return nil, "", err
				}
				if len(entries) == 0 {
					rows = append(rows, []string{label, "—", "", "", ""})
					continue
				}
				for _, e := range entries {
					scheduled++
					note := e.Note
					if e.EmergencyOverride {
						note = "[emergency cover] " + note
					}
					rows = append(rows, []string{label, e.Username, e.StartTime, e.EndTime, note})
				}
			}
			return rows, fmt.Sprintf("%d shift(s) scheduled over the next 7 days", scheduled), nil
		},
	}
}
