package lego

import (
	"path/filepath"
	"testing"
	"time"
)

func TestCancelTicketAndStaleSweep(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "lego.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	old, _ := d.AssignTicket(TicketCheck, "75192-1", "Falcon", "", "", "", "admin")
	fresh, _ := d.AssignTicket(TicketCheck, "10294-1", "Titanic", "", "", "", "admin")
	claimed, _ := d.AssignTicket(TicketOrder, "7", "Order 7", "sam", "", "", "admin")
	d.Exec(`UPDATE job_tickets SET created_at = ? WHERE id IN (?, ?)`, time.Now().Add(-10*24*time.Hour).Format(timeLayout), old.ID, claimed.ID)
	if err := d.ClaimTicket(claimed.ID, "sam"); err != nil {
		t.Fatal(err)
	}

	if _, err := d.CancelTicket(fresh.ID, "admin", " "); err == nil {
		t.Fatal("a blank reason must be refused")
	}
	if got, _ := d.StaleQueuedTickets(7 * 24 * time.Hour); len(got) != 1 || got[0].ID != old.ID {
		t.Fatalf("stale = %+v, want only the old queued one (claimed work is left alone)", got)
	}
	done, err := d.CancelStaleQueuedTickets(7*24*time.Hour, "admin")
	if err != nil || len(done) != 1 {
		t.Fatalf("sweep = %v, %v", done, err)
	}
	open, _ := d.AllOpenTickets()
	if len(open) != 2 {
		t.Fatalf("open after sweep = %d, want 2 (fresh + claimed)", len(open))
	}
	if _, err := d.CancelTicket(old.ID, "admin", "again"); err == nil {
		t.Fatal("cancelling twice must fail")
	}
	if _, err := d.CancelTicket(claimed.ID, "admin", "added by mistake"); err != nil {
		t.Fatalf("a claimed ticket can be removed by an admin: %v", err)
	}
}
