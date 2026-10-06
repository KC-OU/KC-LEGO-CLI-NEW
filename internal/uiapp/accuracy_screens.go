package uiapp

import (
	"fmt"
	"strings"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
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
			for i, v := range trend { // trend[last] is today, trend[0] is 6 days ago — see AccuracyTrend
				date := time.Now().AddDate(0, 0, -(len(trend) - 1 - i)).Format("2006-01-02")
				if accuracyIsNS(app, app.userName(), date) {
					tr = append(tr, "NS")
				} else {
					tr = append(tr, fmt.Sprintf("%.0f", v))
				}
			}
			todayStr := fmt.Sprintf("%.1f%%", today)
			if accuracyIsNS(app, app.userName(), time.Now().Format("2006-01-02")) {
				todayStr = "NS (Not Scheduled)"
			}
			title := fmt.Sprintf("Today (%s): %s  —  last 7 days: %s", role, todayStr, strings.Join(tr, "  "))
			return rows, title, nil
		},
	}
}

// accuracyIsNS reports whether date should show as "Not Scheduled" instead of
// a percentage: only possible once the clock-in gate is actually on
// (config.RequireClockIn) — before that, nobody has rota data, so every day
// would wrongly show NS. See internal/lego.RequireClockedIn for the same gate.
func accuracyIsNS(app *App, username, date string) bool {
	if config.Get(config.RequireClockIn) != "1" {
		return false
	}
	sched, err := app.legoDB.IsScheduled(username, date)
	return err == nil && !sched
}

func fmtNonZero(n int) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprint(n)
}

// ---- Admin: dock or credit someone's accuracy ----

func accuracyWhoScreen() screenModel {
	return &menuScreen{
		panelID:      "ACCWHO",
		title:        "Dock / Credit Accuracy",
		adminGated:   true,
		deniedAction: "SETTINGS_ACCESS",
		options: func(app *App) []menuOption {
			return []menuOption{
				{Key: "1", Label: "Dock someone's accuracy…", Go: func(app *App) { startDockPick(app, false) }},
				{Key: "2", Label: "Credit someone's accuracy…", Go: func(app *App) { startDockPick(app, true) }},
				{Key: "3", Label: "View accuracy reports…", Go: startAccuracyReportsPick},
				{Key: "0", Label: "Return", Go: func(app *App) { app.onBack() }},
			}
		},
	}
}

type dockDraft struct {
	username, role string
	credit         bool // false = dock (subtract), true = credit (add) — the mirror action
}

func startDockPick(app *App, credit bool) {
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
	verb := "Dock"
	if credit {
		verb = "Credit"
	}
	startPick(app, &pickState{
		Header: verb + " whose accuracy?",
		Prompt: "Choose",
		Items:  items,
		OnPick: func(app *App, it pickItem) {
			startDockRole(app, it.Key, credit)
		},
	})
}

func startDockRole(app *App, username string, credit bool) {
	startPick(app, &pickState{
		Header: "As a picker or a checker?",
		Prompt: "Choose",
		Items:  []pickItem{{Key: lego.AccuracyPicker, Label: "Picker accuracy"}, {Key: lego.AccuracyChecker, Label: "Checker accuracy"}},
		OnPick: func(app *App, it pickItem) {
			app.dock = &dockDraft{username: username, role: it.Key, credit: credit}
			app.goTo(scrAccuracyDock)
		},
	})
}

func accuracyDockScreen() screenModel {
	return &formScreen{
		panelID: "ACCDCK",
		title:   "Dock / Credit Accuracy",
		preamble: func(app *App) string {
			if app.dock == nil {
				return ""
			}
			verb := "Docking"
			if app.dock.credit {
				verb = "Crediting"
			}
			return app.theme.Muted.Render(fmt.Sprintf("%s %s's %s accuracy.", verb, app.dock.username, app.dock.role))
		},
		build: func(app *App) []ui.Field {
			label := "Points to dock (e.g. 10)"
			if app.dock != nil && app.dock.credit {
				label = "Points to credit (e.g. 10)"
			}
			return []ui.Field{{Label: label}, {Label: "Reason"}}
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
			verb, pastTense, sign, kind := "dock", "Docked", "-", lego.EventAccuracyDock
			apply := app.legoDB.DockAccuracy
			if d.credit {
				verb, pastTense, sign, kind = "credit", "Credited", "+", lego.EventAccuracyCredit
				apply = app.legoDB.CreditAccuracy
			}
			if err := apply(d.username, d.role, amount, values[1], app.userName()); err != nil {
				app.setMsg(err.Error(), true)
				return
			}
			app.audit.Log(app.userName(), "", "ACCURACY_"+strings.ToUpper(verb)+"ED", "SUCCESS", fmt.Sprintf("%s (%s) %s%.1f%%: %s", d.username, d.role, sign, amount, values[1]))
			_ = app.legoDB.LogEvent(kind, app.userName(), d.username, fmt.Sprintf("%s %s%.1f%%: %s", d.role, sign, amount, values[1]))
			app.notifyEvent(kind, fmt.Sprintf("%s's %s accuracy %sed %.1f%% by %s: %s", d.username, d.role, verb, amount, app.userName(), values[1]), "")
			_ = app.legoDB.SendMessage(app.userName(), d.username, fmt.Sprintf("Your %s accuracy was %sed %.1f%% by an admin: %s", d.role, verb, amount, values[1]))
			app.dock = nil
			app.setMsg(pastTense+".", false)
			app.stack = nil
			app.cur = scrAdminHub
		},
	}
}
