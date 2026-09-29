package lego

import (
	"path/filepath"
	"testing"
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
