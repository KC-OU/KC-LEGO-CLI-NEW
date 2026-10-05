package lego

import (
	"path/filepath"
	"testing"
	"time"
)

func TestTicketAssignClaimFinish(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "lego.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	tk, err := db.AssignTicket(TicketCheck, "75192-1", "Millennium Falcon", "", "", "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if tk.Token == "" {
		t.Error("expected a barcode claim token")
	}

	open, err := db.OpenTickets(TicketCheck, "dave")
	if err != nil || len(open) != 1 {
		t.Fatalf("open tickets for dave = %d, %v, want 1", len(open), err)
	}

	if err := db.ClaimTicket(tk.ID, "dave"); err != nil {
		t.Fatal(err)
	}
	// Someone else can't also claim it.
	if err := db.ClaimTicket(tk.ID, "sam"); err == nil {
		t.Error("a second claim on an already-claimed ticket must fail")
	}

	cur, err := db.CurrentTicket("dave")
	if err != nil || cur == nil || cur.ID != tk.ID {
		t.Fatalf("dave's current ticket = %+v, %v", cur, err)
	}

	if err := db.FinishTicket(TicketCheck, "75192-1", "dave"); err != nil {
		t.Fatal(err)
	}
	cur, _ = db.CurrentTicket("dave")
	if cur != nil {
		t.Errorf("finished ticket must not still be dave's current job: %+v", cur)
	}
}

// TestReopenTicketRequiresAReasonAndLeavesTheOriginalAlone is the direct
// test for "Admin: reopen a wrong check/order": a reason is mandatory, the
// finished ticket is untouched (the audit trail — "this was redone" sits
// beside the original, nothing is overwritten), and the new one is open for
// anyone to claim.
func TestReopenTicketRequiresAReasonAndLeavesTheOriginalAlone(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "lego.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	tk, err := db.AssignTicket(TicketCheck, "75192-1", "Millennium Falcon", "", "", "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ClaimTicket(tk.ID, "dave"); err != nil {
		t.Fatal(err)
	}
	if err := db.FinishTicket(TicketCheck, "75192-1", "dave"); err != nil {
		t.Fatal(err)
	}

	if _, err := db.ReopenTicket(TicketCheck, "75192-1", "Millennium Falcon", "", "admin"); err == nil {
		t.Error("ReopenTicket without a reason must fail")
	}

	reopened, err := db.ReopenTicket(TicketCheck, "75192-1", "Millennium Falcon", "miscounted the dark grey plates", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if reopened.ID == tk.ID || reopened.Status != TicketQueued || reopened.AssignedTo != "" {
		t.Errorf("reopened ticket = %+v, want a new queued/open ticket", reopened)
	}

	// The original stays exactly as finished — a new row, not a rewrite.
	var stillDone string
	if err := db.QueryRow(`SELECT status FROM job_tickets WHERE id = ?`, tk.ID).Scan(&stillDone); err != nil || stillDone != TicketDone {
		t.Errorf("the original ticket's status = %q, %v, want unchanged %q", stillDone, err, TicketDone)
	}

	open, err := db.OpenTickets(TicketCheck, "anyone")
	if err != nil || len(open) != 1 || open[0].ID != reopened.ID {
		t.Errorf("open tickets = %+v, %v, want just the reopened one", open, err)
	}
}

// TestEstimatePaceAveragesRecentFinishedTickets is the direct test for the
// "how long it will take" half of the time-estimate feature: no history ->
// zero samples (caller shows nothing); finished tickets of the other kind
// are excluded; the average is actually an average, not just the last one.
func TestEstimatePaceAveragesRecentFinishedTickets(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "lego.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if pace, err := db.EstimatePace(TicketCheck, 20); err != nil || pace.Samples != 0 {
		t.Fatalf("no history yet = %+v, %v, want zero samples", pace, err)
	}

	finish := func(kind, target string, claimed, done time.Time) {
		t.Helper()
		tk, err := db.AssignTicket(kind, target, target, "", "", "", "admin")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`UPDATE job_tickets SET status = ?, assigned_to = 'dave', claimed_at = ?, done_at = ? WHERE id = ?`,
			TicketDone, claimed.Format(time.RFC3339), done.Format(time.RFC3339), tk.ID); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	finish(TicketCheck, "1-1", now.Add(-20*time.Minute), now.Add(-10*time.Minute)) // 10 min
	finish(TicketCheck, "2-1", now.Add(-40*time.Minute), now.Add(-20*time.Minute)) // 20 min
	finish(TicketOrder, "99", now.Add(-5*time.Minute), now)                        // 5 min, other kind — excluded

	pace, err := db.EstimatePace(TicketCheck, 20)
	if err != nil {
		t.Fatal(err)
	}
	if pace.Samples != 2 || pace.AvgSeconds != 900 { // (10+20)/2 = 15 min = 900s
		t.Errorf("pace = %+v, want 2 samples averaging 900s", pace)
	}

	orderPace, err := db.EstimatePace(TicketOrder, 20)
	if err != nil || orderPace.Samples != 1 || orderPace.AvgSeconds != 300 {
		t.Errorf("order pace = %+v, %v, want 1 sample of 300s", orderPace, err)
	}
}

func TestTicketAbandonReturnsToOpenQueue(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "lego.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	tk, err := db.AssignTicket(TicketOrder, "42", "Order #42", "dave", "", "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ClaimTicket(tk.ID, "dave"); err != nil {
		t.Fatal(err)
	}
	if err := db.AbandonTicket(tk.ID, "dave"); err != nil {
		t.Fatal(err)
	}
	open, err := db.OpenTickets(TicketOrder, "sam")
	if err != nil || len(open) != 1 || open[0].AssignedTo != "" {
		t.Fatalf("abandoned ticket should be back in the open queue: %+v, %v", open, err)
	}
}

func TestForceOffTicketReleasesOrReassigns(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "lego.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	tk, err := db.AssignTicket(TicketCheck, "75192-1", "Falcon", "", "", "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ClaimTicket(tk.ID, "dave"); err != nil {
		t.Fatal(err)
	}
	if err := db.ForceOffTicket(tk.ID, ""); err != nil {
		t.Fatal(err)
	}
	open, err := db.OpenTickets(TicketCheck, "sam")
	if err != nil || len(open) != 1 || open[0].AssignedTo != "" {
		t.Fatalf("force-off to the open queue: %+v, %v", open, err)
	}
	if cur, _ := db.CurrentTicket("dave"); cur != nil {
		t.Errorf("dave must no longer have this as their current ticket: %+v", cur)
	}

	if err := db.ClaimTicket(tk.ID, "sam"); err != nil {
		t.Fatal(err)
	}
	if err := db.ForceOffTicket(tk.ID, "pat"); err != nil {
		t.Fatal(err)
	}
	open, err = db.OpenTickets(TicketCheck, "pat")
	if err != nil || len(open) != 1 || open[0].AssignedTo != "pat" {
		t.Fatalf("force-off reassigned to pat: %+v, %v", open, err)
	}

	if err := db.ForceOffTicket(tk.ID, ""); err == nil {
		t.Error("force-off on a ticket nobody currently has claimed should error")
	}
}

func TestTicketByToken(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "lego.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	tk, err := db.AssignTicket(TicketCheck, "75192-1", "Falcon", "", "", "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	found, err := db.TicketByToken(tk.Token)
	if err != nil || found == nil || found.ID != tk.ID {
		t.Fatalf("TicketByToken(%q) = %+v, %v", tk.Token, found, err)
	}
	if _, err := db.TicketByToken("nonexistent"); err != nil {
		t.Errorf("unknown token should return (nil, nil): %v", err)
	}
}
