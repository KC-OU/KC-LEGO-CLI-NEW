package lego

import (
	"path/filepath"
	"testing"
	"time"
)

func TestIdleTicketCandidatesFlagsOnlyTicketsClaimedPastTheThreshold(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "lego.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Claimed just now: not idle yet.
	fresh, err := db.AssignTicket(TicketCheck, "75192-1", "Falcon", "", "", "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ClaimTicket(fresh.ID, "dave"); err != nil {
		t.Fatal(err)
	}

	// Claimed an hour ago: idle past a 30-minute threshold.
	stale, err := db.AssignTicket(TicketCheck, "10312-1", "Icy", "", "", "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ClaimTicket(stale.ID, "sam"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE job_tickets SET claimed_at = ? WHERE id = ?`,
		time.Now().Add(-1*time.Hour).Format(time.RFC3339), stale.ID); err != nil {
		t.Fatal(err)
	}

	// Still queued, never claimed: must never show up, regardless of age.
	if _, err := db.AssignTicket(TicketOrder, "317", "Order #317", "", "", "", "admin"); err != nil {
		t.Fatal(err)
	}

	cands, err := db.IdleTicketCandidates(30)
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 1 || cands[0].Target != "10312-1" || cands[0].AssignedTo != "sam" {
		t.Fatalf("IdleTicketCandidates(30) = %+v, want just sam's hour-old 10312-1 claim", cands)
	}
	if cands[0].IdleMinutes < 59 || cands[0].IdleMinutes > 61 {
		t.Errorf("IdleMinutes = %d, want ~60", cands[0].IdleMinutes)
	}
}

func TestIdleTicketCandidatesIgnoresFinishedTickets(t *testing.T) {
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
	if _, err := db.Exec(`UPDATE job_tickets SET claimed_at = ? WHERE id = ?`,
		time.Now().Add(-2*time.Hour).Format(time.RFC3339), tk.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.FinishTicket(TicketCheck, "75192-1", "dave"); err != nil {
		t.Fatal(err)
	}

	cands, err := db.IdleTicketCandidates(30)
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 0 {
		t.Errorf("a finished ticket must never be flagged as idle: %+v", cands)
	}
}
