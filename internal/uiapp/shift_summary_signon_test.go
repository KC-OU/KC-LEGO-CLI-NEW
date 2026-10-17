package uiapp

import (
	"strings"
	"testing"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

// TestShiftSummaryShownAtSignOnAlongsideHandoverNote is roadmap item 8's
// sign-on integration: the auto shift-handover summary shows full-screen
// at the next picker/checker's sign-on, the same treatment
// showHandoverNoteOnce's manual note already gets -- sitting alongside it,
// not replacing it.
func TestShiftSummaryShownAtSignOnAlongsideHandoverNote(t *testing.T) {
	app := newTestApp(t)
	setPolicy(t, func(p *access.Policy) {
		p.Users["partdb:checker3"] = &access.User{Groups: []string{"checker"}, TwoFA: access.TwoFAExempt}
	})
	if err := app.legoDB.SetHandoverNote("Fridge is out of milk.", "admin"); err != nil {
		t.Fatal(err)
	}

	tk, err := app.legoDB.AssignTicket(lego.TicketCheck, "75192-1", "75192-1 Falcon", "dave", "", "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := app.legoDB.ClaimTicket(tk.ID, "dave"); err != nil {
		t.Fatal(err)
	}
	if err := app.legoDB.FinishTicket(lego.TicketCheck, "75192-1", "dave"); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if _, err := app.legoDB.Exec(`INSERT INTO clock_events (username, clock_in_at, clock_out_at) VALUES (?, ?, ?)`,
		"dave", now.Add(-2*time.Hour).Format(time.RFC3339), now.Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}

	signOn(app, "checker3", "")

	if app.cur != scrMessagesFull {
		t.Fatalf("cur after sign-on = %q, want scrMessagesFull (both the note and the summary queued)", app.cur)
	}
	if len(app.toasts) != 2 {
		t.Fatalf("toasts = %+v, want 2 (handover note + shift summary)", app.toasts)
	}
	var sawNote, sawSummary bool
	for _, m := range app.toasts {
		if strings.Contains(m.Body, "Fridge is out of milk") {
			sawNote = true
		}
		if strings.Contains(m.From, "Shift summary") {
			sawSummary = true
			if !strings.Contains(m.Body, "75192-1") {
				t.Errorf("shift summary body missing the finished ticket: %q", m.Body)
			}
		}
	}
	if !sawNote || !sawSummary {
		t.Fatalf("expected both the manual note and the auto summary queued, got: %+v", app.toasts)
	}
}

// TestShiftSummaryAgesOutAfterARecentCutoff: a week-old shift's summary
// should no longer be shown -- unlike the manual note, which is
// deliberately evergreen.
func TestShiftSummaryAgesOutAfterARecentCutoff(t *testing.T) {
	app := newTestApp(t)
	setPolicy(t, func(p *access.Policy) {
		p.Users["partdb:checker3"] = &access.User{Groups: []string{"checker"}, TwoFA: access.TwoFAExempt}
	})
	old := time.Now().Add(-7 * 24 * time.Hour)
	if _, err := app.legoDB.Exec(`INSERT INTO clock_events (username, clock_in_at, clock_out_at) VALUES (?, ?, ?)`,
		"dave", old.Add(-2*time.Hour).Format(time.RFC3339), old.Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}

	signOn(app, "checker3", "")

	if app.cur == scrMessagesFull {
		t.Errorf("a week-old shift summary should not be shown; toasts = %+v", app.toasts)
	}
}
