package lego

import (
	"testing"
	"time"
)

func insertClosedShift(t *testing.T, d *DB, username string, in, out time.Time) {
	t.Helper()
	if _, err := d.Exec(`INSERT INTO clock_events (username, clock_in_at, clock_out_at) VALUES (?, ?, ?)`,
		username, in.Format(time.RFC3339), out.Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
}

func TestLastClosedShiftIsNilUntilSomeoneClocksOut(t *testing.T) {
	d := attDB(t)
	e, err := d.LastClosedShift()
	if err != nil || e != nil {
		t.Fatalf("LastClosedShift = %+v, %v, want nil, nil", e, err)
	}
	if _, err := d.ClockIn("dave"); err != nil {
		t.Fatal(err)
	}
	// Still open -- clocking in alone must not produce a "closed shift".
	if e, err := d.LastClosedShift(); err != nil || e != nil {
		t.Fatalf("LastClosedShift with an open shift = %+v, %v, want nil, nil", e, err)
	}
	if err := d.ClockOut("dave"); err != nil {
		t.Fatal(err)
	}
	e, err = d.LastClosedShift()
	if err != nil || e == nil || e.Username != "dave" || e.Open() {
		t.Fatalf("LastClosedShift after clocking out = %+v, %v", e, err)
	}
}

func TestLastClosedShiftIsTheMostRecentOne(t *testing.T) {
	d := attDB(t)
	base := time.Now().Add(-5 * time.Hour)
	insertClosedShift(t, d, "alice", base, base.Add(2*time.Hour))
	insertClosedShift(t, d, "dave", base.Add(2*time.Hour), base.Add(4*time.Hour))
	e, err := d.LastClosedShift()
	if err != nil || e == nil || e.Username != "dave" {
		t.Fatalf("LastClosedShift = %+v, %v, want dave's (the later) shift", e, err)
	}
}

func TestShiftHandoverSummaryOnlyCountsThisUsersTicketsFinishedDuringTheWindow(t *testing.T) {
	d := attDB(t)
	shiftStart := time.Now().Add(-2 * time.Hour)
	shiftEnd := time.Now()

	// Finished by dave, inside the shift: counts.
	tk, err := d.AssignTicket(TicketCheck, "75192-1", "75192-1 Falcon", "dave", "", "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.ClaimTicket(tk.ID, "dave"); err != nil {
		t.Fatal(err)
	}
	if err := d.FinishTicket(TicketCheck, "75192-1", "dave"); err != nil {
		t.Fatal(err)
	}

	// Finished by someone else: must not count toward dave's shift.
	tk2, err := d.AssignTicket(TicketCheck, "10312-1", "10312-1 Icy", "alice", "", "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.ClaimTicket(tk2.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	if err := d.FinishTicket(TicketCheck, "10312-1", "alice"); err != nil {
		t.Fatal(err)
	}

	// Still open: counts toward StillOpen regardless of who it's for.
	if _, err := d.AssignTicket(TicketOrder, "317", "Order #317", "", "", "", "admin"); err != nil {
		t.Fatal(err)
	}

	e := ClockEvent{Username: "dave", ClockInAt: shiftStart, ClockOutAt: shiftEnd}
	s, err := d.ShiftHandoverSummary(e)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Done) != 1 || s.Done[0].Target != "75192-1" {
		t.Fatalf("Done = %+v, want just dave's 75192-1 check", s.Done)
	}
	if len(s.StillOpen) != 1 || s.StillOpen[0].Target != "317" {
		t.Fatalf("StillOpen = %+v, want the open order", s.StillOpen)
	}
}

func TestShiftHandoverSummaryExcludesTicketsFinishedOutsideTheWindow(t *testing.T) {
	d := attDB(t)
	tk, err := d.AssignTicket(TicketCheck, "75192-1", "75192-1 Falcon", "dave", "", "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.ClaimTicket(tk.ID, "dave"); err != nil {
		t.Fatal(err)
	}
	if err := d.FinishTicket(TicketCheck, "75192-1", "dave"); err != nil {
		t.Fatal(err)
	}

	// A shift window that ended before this ticket was ever finished.
	e := ClockEvent{Username: "dave", ClockInAt: time.Now().Add(-4 * time.Hour), ClockOutAt: time.Now().Add(-3 * time.Hour)}
	s, err := d.ShiftHandoverSummary(e)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Done) != 0 {
		t.Errorf("Done = %+v, want empty -- the ticket finished after this shift window ended", s.Done)
	}
}
