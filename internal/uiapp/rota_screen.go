package uiapp

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Weekly rota view: who's scheduled when, the next 7 days starting today —
// reached from the Admin hub like Live Sessions or Recent Activity. N sets
// a planned shift, O is the quick emergency-cover override for today, C
// clears a day — the same three operations `wms attendance rota` already
// gives the CLI, just reachable without a terminal command. Both still
// write through the same DB methods (SetRota/QuickNSOverride/ClearRota),
// so a CLI change and a TUI change land identically either way.
const scrRotaView = "rota_view"

func rotaToday() string { return time.Now().Format("2006-01-02") }

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
			subtitle := fmt.Sprintf("%d shift(s) over the next 7 days — N schedule, O quick override today, C clear a day", scheduled)
			return rows, subtitle, nil
		},
		extra: func(app *App, msg tea.KeyMsg) {
			switch {
			case isKey(msg, 'n'):
				startPick(app, rotaUserPick(app, "Schedule a shift for:", rotaSetShiftDate))
			case isKey(msg, 'o'):
				startPick(app, rotaUserPick(app, "Quick override (emergency cover) today for:", rotaQuickOverride))
			case isKey(msg, 'c'):
				startPick(app, rotaUserPick(app, "Clear whose schedule:", rotaClearDate))
			}
		},
	}
}

// rotaUserPick is the shared first step of every rota admin action — every
// known user, narrows as you type the same way Assign Work's own picker
// does (assignWhoPick), since this is the same "who" choice by a different
// name. No "(open queue)" option here — a rota entry always names someone.
func rotaUserPick(app *App, header string, next func(app *App, username string)) *pickState {
	rows, _ := app.users.ListAll(app.ctx())
	items := make([]pickItem, 0, len(rows))
	seen := map[string]bool{}
	for _, r := range rows {
		if r.Username == "" || seen[r.Username] {
			continue
		}
		seen[r.Username] = true
		items = append(items, pickItem{Key: r.Username, Label: r.Username + "  (id " + r.ID + ", " + r.RoleOrGroup + ")"})
	}
	return &pickState{
		Header: header,
		Prompt: "Choose",
		Items:  items,
		OnPick: func(app *App, it pickItem) { next(app, it.Key) },
	}
}

func rotaSetShiftDate(app *App, username string) {
	startPick(app, &pickState{
		Header:     "Date for " + username + " (YYYY-MM-DD):",
		Prompt:     "Date",
		AllowFree:  true,
		FreeHint:   "blank = today",
		BlankLabel: "today",
		OnFree: func(app *App, date string) {
			if date == "" {
				date = rotaToday()
			}
			startPick(app, &pickState{
				Header:     "Start time (optional, e.g. 09:00):",
				Prompt:     "Start",
				AllowFree:  true,
				FreeHint:   "blank = no set time",
				BlankLabel: "no set time",
				OnFree: func(app *App, start string) {
					startPick(app, &pickState{
						Header:     "End time (optional, e.g. 17:00):",
						Prompt:     "End",
						AllowFree:  true,
						FreeHint:   "blank = no set time",
						BlankLabel: "no set time",
						OnFree: func(app *App, end string) {
							startPick(app, &pickState{
								Header:     "Note (optional):",
								Prompt:     "Note",
								AllowFree:  true,
								FreeHint:   "e.g. covering lates",
								BlankLabel: "no note",
								OnFree: func(app *App, note string) {
									finishRotaSet(app, username, date, start, end, note)
								},
							})
						},
					})
				},
			})
		},
	})
}

func finishRotaSet(app *App, username, date, start, end, note string) {
	if err := app.legoDB.SetRota(username, date, start, end, note, app.userName()); err != nil {
		app.setMsg(err.Error(), true)
	} else {
		app.audit.Log(app.userName(), "", "ROTA_SET", "SUCCESS", fmt.Sprintf("%s on %s (%s-%s)", username, date, start, end))
		app.setMsg(fmt.Sprintf("%s scheduled on %s.", username, date), false)
	}
	app.stack = nil
	app.cur = scrRotaView
}

func rotaQuickOverride(app *App, username string) {
	date := rotaToday()
	if err := app.legoDB.QuickNSOverride(username, date, app.userName()); err != nil {
		app.setMsg(err.Error(), true)
	} else {
		app.audit.Log(app.userName(), "", "ROTA_OVERRIDE", "SUCCESS", username+" on "+date)
		app.setMsg(fmt.Sprintf("%s covered for %s (emergency cover, not a planned shift).", username, date), false)
	}
	app.stack = nil
	app.cur = scrRotaView
}

func rotaClearDate(app *App, username string) {
	startPick(app, &pickState{
		Header:     "Clear which date for " + username + " (YYYY-MM-DD):",
		Prompt:     "Date",
		AllowFree:  true,
		FreeHint:   "blank = today",
		BlankLabel: "today",
		OnFree: func(app *App, date string) {
			if date == "" {
				date = rotaToday()
			}
			if err := app.legoDB.ClearRota(username, date); err != nil {
				app.setMsg(err.Error(), true)
			} else {
				app.audit.Log(app.userName(), "", "ROTA_CLEARED", "SUCCESS", username+" on "+date)
				app.setMsg(fmt.Sprintf("%s's schedule for %s cleared.", username, date), false)
			}
			app.stack = nil
			app.cur = scrRotaView
		},
	})
}
