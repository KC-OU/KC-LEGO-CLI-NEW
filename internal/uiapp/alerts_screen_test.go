package uiapp

import (
	"strings"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

func TestAlertsScreenListsOpenEscalationsAndFilingClearsIt(t *testing.T) {
	app := newTestApp(t)
	if _, _, err := app.legoDB.RecordCheckOutcome("dave", lego.AccuracyChecker, lego.TicketCheck, "75192-1", 500, 25); err != nil {
		t.Fatal(err)
	}

	scr := alertsScreen().(*selectList)
	_, rows, _ := scr.rows(app)
	if len(rows) != 1 || rows[0][1] != "dave" {
		t.Fatalf("rows = %+v, want one open alert for dave", rows)
	}

	open, _ := app.legoDB.OpenAccuracyEscalations()
	if len(open) != 1 {
		t.Fatalf("precondition: want exactly 1 open escalation, got %d", len(open))
	}
	app.reviewingEscalation = &open[0]
	app.goTo(scrAccuracyReport)

	app.screens[scrAccuracyReport].(*formScreen).submit(app, []string{
		"Checked the delivery, parts genuinely missing.",
		"Reordered the missing parts.",
		"yes",
	})

	_, rows, _ = scr.rows(app)
	if len(rows) != 0 {
		t.Fatalf("after filing, rows = %+v, want none left open", rows)
	}

	reps, err := app.legoDB.AccuracyReportsFor("dave", lego.AccuracyChecker)
	if err != nil || len(reps) != 1 {
		t.Fatalf("AccuracyReportsFor = %+v, %v, want 1 filed report", reps, err)
	}
	if reps[0].ReviewedBy != "admin" || !reps[0].TalkRequested {
		t.Errorf("report = %+v, want reviewed_by=admin and talk_requested=true", reps[0])
	}

	msgs, err := app.legoDB.UndeliveredMessages("dave")
	if err != nil || len(msgs) != 1 {
		t.Fatalf("UndeliveredMessages = %+v, %v, want the let's-talk message since talk was requested", msgs, err)
	}
	if !strings.Contains(msgs[0].Body, "discuss") {
		t.Errorf("message body = %q, want it to mention discussing the report", msgs[0].Body)
	}
}

func TestAccuracyReportRequiresSummaryAndAction(t *testing.T) {
	app := newTestApp(t)
	if _, _, err := app.legoDB.RecordCheckOutcome("dave", lego.AccuracyChecker, lego.TicketCheck, "75192-1", 500, 25); err != nil {
		t.Fatal(err)
	}
	open, _ := app.legoDB.OpenAccuracyEscalations()
	app.reviewingEscalation = &open[0]

	app.screens[scrAccuracyReport].(*formScreen).submit(app, []string{"", "", "no"})

	still, err := app.legoDB.OpenAccuracyEscalations()
	if err != nil || len(still) != 1 {
		t.Fatalf("a blank submission must not file anything: OpenAccuracyEscalations = %+v, %v", still, err)
	}
}

func TestAccuracyReportSkipsTheTalkMessageWhenNotRequested(t *testing.T) {
	app := newTestApp(t)
	if _, _, err := app.legoDB.RecordCheckOutcome("dave", lego.AccuracyChecker, lego.TicketCheck, "75192-1", 500, 25); err != nil {
		t.Fatal(err)
	}
	open, _ := app.legoDB.OpenAccuracyEscalations()
	app.reviewingEscalation = &open[0]

	app.screens[scrAccuracyReport].(*formScreen).submit(app, []string{"Found it.", "Reordered.", "no"})

	msgs, err := app.legoDB.UndeliveredMessages("dave")
	if err != nil || len(msgs) != 0 {
		t.Fatalf("UndeliveredMessages = %+v, %v, want none when talk wasn't requested", msgs, err)
	}
}

func TestAccuracyReportsViewScreenShowsFiledReports(t *testing.T) {
	app := newTestApp(t)
	if _, err := app.legoDB.FileAccuracyReport(1, "dave", lego.AccuracyChecker, "admin", "summary here", "action here", false); err != nil {
		t.Fatal(err)
	}
	app.viewingReportsFor = &reportsViewDraft{username: "dave", role: lego.AccuracyChecker}

	scr := accuracyReportsViewScreen().(*tableScreen)
	rows, subtitle, err := scr.fetch(app)
	if err != nil || len(rows) != 1 {
		t.Fatalf("fetch = %+v, %q, %v, want 1 row", rows, subtitle, err)
	}
	if rows[0][2] != "summary here" || rows[0][3] != "action here" {
		t.Errorf("row = %+v, want the filed summary/action", rows[0])
	}
}
