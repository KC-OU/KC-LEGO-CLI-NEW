package uiapp

import (
	"fmt"
	"strings"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

const (
	scrMyAccuracy   = "my_accuracy"
	scrAccuracyDock = "accuracy_dock"
	scrAccuracyWho  = "accuracy_who"
)

func myAccuracyScreen() screenModel {
	return &tableScreen{
		panelID: "MYACC",
		title:   "My Accuracy",
		columns: []string{"When", "Kind", "Missing", "Change", "Note"},
		fetch: func(app *App) ([][]string, string, error) {
			role := roleForApp(app)
			if role == "" {
				return nil, "", nil
			}
			today, err := app.legoDB.AccuracyToday(app.userName(), role)
			if err != nil {
				return nil, "", err
			}
			hist, err := app.legoDB.AccuracyHistory(app.userName(), role, "")
			if err != nil {
				return nil, "", err
			}
			trend, _ := app.legoDB.AccuracyTrend(app.userName(), role, 7)
			rows := make([][]string, len(hist))
			for i := len(hist) - 1; i >= 0; i-- {
				e := hist[i]
				rows[len(hist)-1-i] = []string{e.CreatedAt.Format("15:04"), e.Kind, orDash(fmtNonZero(e.Missing)), fmt.Sprintf("%+.1f%%", e.Delta), e.Reason}
			}
			var tr []string
			for _, v := range trend {
				tr = append(tr, fmt.Sprintf("%.0f", v))
			}
			title := fmt.Sprintf("Today (%s): %.1f%%  —  last 7 days: %s", role, today, strings.Join(tr, "  "))
			return rows, title, nil
		},
	}
}

func fmtNonZero(n int) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprint(n)
}

// ---- Admin: dock someone's accuracy ----

func accuracyWhoScreen() screenModel {
	return &menuScreen{
		panelID:      "ACCWHO",
		title:        "Dock Accuracy",
		adminGated:   true,
		deniedAction: "SETTINGS_ACCESS",
		options: func(app *App) []menuOption {
			return []menuOption{
				{Key: "1", Label: "Pick a user…", Go: startDockPick},
				{Key: "0", Label: "Return", Go: func(app *App) { app.onBack() }},
			}
		},
	}
}

type dockDraft struct {
	username, role string
}

func startDockPick(app *App) {
	rows, _ := app.users.ListAll(app.ctx())
	var items []pickItem
	seen := map[string]bool{}
	for _, r := range rows {
		if r.Username == "" || seen[r.Username] {
			continue
		}
		seen[r.Username] = true
		items = append(items, pickItem{Key: r.Username, Label: r.Username + "  (id " + r.ID + ", " + r.RoleOrGroup + ")"})
	}
	startPick(app, &pickState{
		Header: "Dock whose accuracy?",
		Prompt: "Choose",
		Items:  items,
		OnPick: func(app *App, it pickItem) {
			startDockRole(app, it.Key)
		},
	})
}

func startDockRole(app *App, username string) {
	startPick(app, &pickState{
		Header: "As a picker or a checker?",
		Prompt: "Choose",
		Items:  []pickItem{{Key: lego.AccuracyPicker, Label: "Picker accuracy"}, {Key: lego.AccuracyChecker, Label: "Checker accuracy"}},
		OnPick: func(app *App, it pickItem) {
			app.dock = &dockDraft{username: username, role: it.Key}
			app.goTo(scrAccuracyDock)
		},
	})
}

func accuracyDockScreen() screenModel {
	return &formScreen{
		panelID: "ACCDCK",
		title:   "Dock Accuracy",
		build: func(app *App) []ui.Field {
			return []ui.Field{{Label: "Points to dock (e.g. 10)"}, {Label: "Reason"}}
		},
		submit: func(app *App, values []string) {
			if app.dock == nil {
				app.onBack()
				return
			}
			var amount float64
			if _, err := fmt.Sscanf(values[0], "%f", &amount); err != nil || amount <= 0 {
				app.setMsg("Enter a positive number of points.", true)
				return
			}
			if values[1] == "" {
				app.setMsg("A reason is required.", true)
				return
			}
			d := app.dock
			if err := app.legoDB.DockAccuracy(d.username, d.role, amount, values[1], app.userName()); err != nil {
				app.setMsg(err.Error(), true)
				return
			}
			app.audit.Log(app.userName(), "", "ACCURACY_DOCKED", "SUCCESS", fmt.Sprintf("%s (%s) -%.1f%%: %s", d.username, d.role, amount, values[1]))
			_ = app.legoDB.LogEvent(lego.EventAccuracyDock, app.userName(), d.username, fmt.Sprintf("%s -%.1f%%: %s", d.role, amount, values[1]))
			app.notifyEvent(lego.EventAccuracyDock, fmt.Sprintf("%s's %s accuracy docked %.1f%% by %s: %s", d.username, d.role, amount, app.userName(), values[1]), "")
			_ = app.legoDB.SendMessage(app.userName(), d.username, fmt.Sprintf("Your %s accuracy was docked %.1f%% by an admin: %s", d.role, amount, values[1]))
			app.dock = nil
			app.setMsg("Docked.", false)
			app.stack = nil
			app.cur = scrAdminHub
		},
	}
}
