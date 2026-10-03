package uiapp

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/exports"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/reports"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

// Admin → Alerts: every 21+-missing check/order flagged for manual review
// (see RecordCheckOutcome/accKindEscalate, internal/lego/accuracy.go) that
// hasn't had a report filed against it yet. Filing one (accuracyReportScreen)
// is what clears it from this list — see OpenAccuracyEscalations.

const (
	scrAlerts              = "alerts"
	scrAccuracyReport      = "accuracy_report"
	scrAccuracyReportsView = "accuracy_reports_view"
)

func alertsScreen() screenModel {
	return &selectList{
		panelID:   "ALERTS",
		title:     "Alerts",
		hint:      "Enter reviews it and files a report, J jumps to the part/order",
		emptyHint: "No open alerts — nothing needs a manual accuracy review right now.",
		rows: func(app *App) ([]string, [][]string, []string) {
			open, err := app.legoDB.OpenAccuracyEscalations()
			if err != nil {
				app.setMsg(err.Error(), true)
			}
			var rows [][]string
			var keys []string
			for _, e := range open {
				rows = append(rows, []string{e.CreatedAt.Format("Jan 2 15:04"), e.Username, e.Role, fmt.Sprint(e.Missing), e.Target})
				keys = append(keys, strconv.FormatInt(e.ID, 10))
			}
			return []string{"When", "Who", "Role", "Missing", "Target"}, rows, keys
		},
		keys: func(app *App, key string, msg tea.KeyMsg) {
			if key == "" {
				return
			}
			isJump := msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && (msg.Runes[0] == 'j' || msg.Runes[0] == 'J')
			if msg.Type != tea.KeyEnter && !isJump {
				return
			}
			id, err := strconv.ParseInt(key, 10, 64)
			if err != nil {
				return
			}
			e, err := app.legoDB.AccuracyEscalationByID(id)
			if err != nil {
				app.setMsg("That alert is no longer open.", true)
				return
			}
			if isJump {
				app.legoSearchTerm = e.Target
				app.goTo(scrLegoSetResults)
				return
			}
			app.reviewingEscalation = &e
			app.goTo(scrAccuracyReport)
		},
	}
}

// ---- Admin: review one alert and file a report ----

func accuracyReportScreen() screenModel {
	return &formScreen{
		panelID: "ACCRPT",
		title:   "File an Accuracy Report",
		preamble: func(app *App) string {
			e := app.reviewingEscalation
			if e == nil {
				return ""
			}
			return app.theme.Muted.Render(fmt.Sprintf("%s (%s) — %d missing on %s", e.Username, e.Role, e.Missing, e.Target))
		},
		build: func(app *App) []ui.Field {
			return []ui.Field{
				{Label: "What did you find? (summary)"},
				{Label: "Action taken"},
				{Label: `Discuss with them? ("yes" or "no")`},
			}
		},
		submit: func(app *App, values []string) {
			e := app.reviewingEscalation
			if e == nil {
				app.onBack()
				return
			}
			summary, action := strings.TrimSpace(values[0]), strings.TrimSpace(values[1])
			if summary == "" || action == "" {
				app.setMsg("A summary and action taken are both required.", true)
				return
			}
			talk := strings.HasPrefix(strings.ToLower(strings.TrimSpace(values[2])), "y")

			if _, err := app.legoDB.FileAccuracyReport(e.ID, e.Username, e.Role, app.userName(), summary, action, talk); err != nil {
				app.setMsg(err.Error(), true)
				return
			}

			link := saveAccuracyReportPDF(app, *e, summary, action, talk)

			if talk {
				_ = app.legoDB.SendMessage(app.userName(), e.Username, "Your manager would like to discuss a recent accuracy report with you.")
			}
			detail := fmt.Sprintf("%s (%s): %s", e.Username, e.Role, summary)
			app.audit.Log(app.userName(), "", "ACCURACY_REPORT_FILED", "SUCCESS", detail)
			_ = app.legoDB.LogEvent(lego.EventAccuracyReportFiled, app.userName(), e.Username, summary)
			app.notifyEvent(lego.EventAccuracyReportFiled, fmt.Sprintf("Accuracy report filed for %s by %s", e.Username, app.userName()), "")

			msg := "Report filed."
			if link != "" {
				msg += " Download: " + link
			}
			app.setMsg(msg, false)
			app.reviewingEscalation = nil
			app.stack = nil
			app.cur = scrAlerts
		},
	}
}

// saveAccuracyReportPDF renders and saves the filed report as a PDF, same
// export-and-link pattern every other download in this app uses — "" if it
// couldn't be saved (the report is still filed either way; the PDF is a
// convenience copy, not the record of truth).
func saveAccuracyReportPDF(app *App, e lego.AccuracyEscalation, summary, action string, talk bool) string {
	pdf := reports.AccuracyReportPDF(reports.AccuracyReportData{
		Username: e.Username, Role: e.Role, ReviewedBy: app.userName(),
		Target: e.Target, Missing: e.Missing,
		Summary: summary, ActionTaken: action, TalkRequested: talk,
		CreatedAt: time.Now(),
	})
	path, err := exports.Save(exports.Dir(), app.userName(), "accuracy-report", e.Username, "pdf", pdf)
	if err != nil {
		return ""
	}
	tok, err := exports.NewLink(exports.Dir(), path, app.userName())
	if err != nil {
		return ""
	}
	return exports.URL(tok)
}

// ---- Admin: view someone's past accuracy reports ----

type reportsViewDraft struct{ username, role string }

func startAccuracyReportsPick(app *App) {
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
		Header: "Whose accuracy reports?",
		Prompt: "Choose",
		Items:  items,
		OnPick: func(app *App, it pickItem) { startAccuracyReportsRole(app, it.Key) },
	})
}

func startAccuracyReportsRole(app *App, username string) {
	startPick(app, &pickState{
		Header: "Picker or checker reports?",
		Prompt: "Choose",
		Items:  []pickItem{{Key: lego.AccuracyPicker, Label: "Picker accuracy"}, {Key: lego.AccuracyChecker, Label: "Checker accuracy"}},
		OnPick: func(app *App, it pickItem) {
			app.viewingReportsFor = &reportsViewDraft{username: username, role: it.Key}
			app.goTo(scrAccuracyReportsView)
		},
	})
}

func accuracyReportsViewScreen() screenModel {
	return &tableScreen{
		panelID: "ACCRPV",
		title:   "Accuracy Reports",
		columns: []string{"When", "Reviewed by", "Summary", "Action Taken", "Talk?"},
		fetch: func(app *App) ([][]string, string, error) {
			v := app.viewingReportsFor
			if v == nil {
				return nil, "", nil
			}
			reps, err := app.legoDB.AccuracyReportsFor(v.username, v.role)
			rows := make([][]string, len(reps))
			for i, r := range reps {
				talk := "No"
				if r.TalkRequested {
					talk = "Yes"
				}
				rows[i] = []string{r.CreatedAt.Format("Jan 2 15:04"), r.ReviewedBy, r.Summary, r.ActionTaken, talk}
			}
			return rows, fmt.Sprintf("%s (%s) — %d report(s)", v.username, v.role, len(rows)), err
		},
	}
}
