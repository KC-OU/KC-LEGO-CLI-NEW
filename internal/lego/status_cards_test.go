package lego

import "testing"

// TestSetStatusCardIsOneAtATimeAndClearIsANoop confirms a worker has exactly
// one current status: putting a second card on closes the first, and
// clearing someone with no active card is a no-op, not an error (every
// auto-clear path -- ClaimTicket, ClockOut -- calls it unconditionally).
func TestSetStatusCardIsOneAtATimeAndClearIsANoop(t *testing.T) {
	d := attDB(t)
	if err := d.ClearStatusCard("dave", "admin"); err != nil {
		t.Fatalf("clearing a card nobody has should be a no-op, got %v", err)
	}

	if _, err := d.SetStatusCard("dave", CardDelay, "waiting on parts", "admin"); err != nil {
		t.Fatal(err)
	}
	c, err := d.ActiveStatusCard("dave")
	if err != nil || c == nil || c.Reason != CardDelay {
		t.Fatalf("ActiveStatusCard after setting delay = %+v, %v", c, err)
	}

	if _, err := d.SetStatusCard("dave", CardNoWork, "", "admin"); err != nil {
		t.Fatal(err)
	}
	c, err = d.ActiveStatusCard("dave")
	if err != nil || c == nil || c.Reason != CardNoWork {
		t.Fatalf("ActiveStatusCard after setting no_work = %+v, %v, want the delay card replaced", c, err)
	}

	cards, err := d.ActiveStatusCards()
	if err != nil || len(cards) != 1 {
		t.Fatalf("ActiveStatusCards = %+v, %v, want exactly one (the delay card should have closed)", cards, err)
	}

	if err := d.ClearStatusCard("dave", "admin"); err != nil {
		t.Fatal(err)
	}
	if c, err := d.ActiveStatusCard("dave"); err != nil || c != nil {
		t.Fatalf("ActiveStatusCard after clearing = %+v, %v, want nil", c, err)
	}
}

func TestSetStatusCardRejectsAnInvalidReason(t *testing.T) {
	d := attDB(t)
	if _, err := d.SetStatusCard("dave", "lunch", "", "admin"); err == nil {
		t.Error("expected an error for an unrecognised reason")
	}
	if _, err := d.SetStatusCard("", CardDelay, "", "admin"); err == nil {
		t.Error("expected an error for a blank username")
	}
}

// TestClaimTicketAutoClearsAnActiveCard: claiming work is itself evidence
// that "no work available" (or a delay) no longer applies.
func TestClaimTicketAutoClearsAnActiveCard(t *testing.T) {
	d := attDB(t)
	if _, err := d.SetStatusCard("dave", CardNoWork, "", "admin"); err != nil {
		t.Fatal(err)
	}
	tk, err := d.AssignTicket(TicketCheck, "75192-1", "Millennium Falcon", "", "", "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.ClaimTicket(tk.ID, "dave"); err != nil {
		t.Fatal(err)
	}
	if c, err := d.ActiveStatusCard("dave"); err != nil || c != nil {
		t.Fatalf("ActiveStatusCard after claiming a ticket = %+v, %v, want nil", c, err)
	}
}

// TestClockOutAutoClearsAnActiveCard: there's nothing left for a card to
// excuse once the shift itself is over.
func TestClockOutAutoClearsAnActiveCard(t *testing.T) {
	d := attDB(t)
	if _, err := d.ClockIn("dave"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.SetStatusCard("dave", CardDelay, "", "admin"); err != nil {
		t.Fatal(err)
	}
	if err := d.ClockOut("dave"); err != nil {
		t.Fatal(err)
	}
	if c, err := d.ActiveStatusCard("dave"); err != nil || c != nil {
		t.Fatalf("ActiveStatusCard after clocking out = %+v, %v, want nil", c, err)
	}
}

// TestCurrentStatusAndHasActiveCardPreferABreakOverAnAdminCard covers the
// edge case of both being true at once (an admin set a card, then the
// worker also went on a break) -- break wins, since neither delay nor
// no-work still makes sense once someone's actually on a break.
func TestCurrentStatusAndHasActiveCardPreferABreakOverAnAdminCard(t *testing.T) {
	d := attDB(t)
	if _, err := d.ClockIn("dave"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.SetStatusCard("dave", CardNoWork, "", "admin"); err != nil {
		t.Fatal(err)
	}
	if err := d.StartBreak("dave"); err != nil {
		t.Fatal(err)
	}
	status, err := d.CurrentStatus("dave")
	if err != nil || status != "On a break" {
		t.Fatalf("CurrentStatus = %q, %v, want \"On a break\"", status, err)
	}
	has, err := d.HasActiveCard("dave")
	if err != nil || !has {
		t.Fatalf("HasActiveCard = %v, %v, want true", has, err)
	}
}

func TestCurrentStatusIsBlankWithNoBreakOrCard(t *testing.T) {
	d := attDB(t)
	status, err := d.CurrentStatus("dave")
	if err != nil || status != "" {
		t.Fatalf("CurrentStatus with nothing active = %q, %v, want \"\"", status, err)
	}
	has, err := d.HasActiveCard("dave")
	if err != nil || has {
		t.Fatalf("HasActiveCard with nothing active = %v, %v, want false", has, err)
	}
}

// TestScanStatusCardTogglesBreakAndRejectsUnknownCodes is the self-serve
// half: the one printed "break card" toggles the scanning user's own
// break via the existing StartBreak/EndBreak -- Delay/No Work have no scan
// code at all, since they're admin-applied only.
func TestScanStatusCardTogglesBreakAndRejectsUnknownCodes(t *testing.T) {
	d := attDB(t)
	if _, err := d.ClockIn("dave"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ScanStatusCard("dave", "CARD-BREAK"); err != nil {
		t.Fatal(err)
	}
	if on, err := d.OnBreak("dave"); err != nil || !on {
		t.Fatalf("OnBreak after scanning the break card = %v, %v, want true", on, err)
	}
	if _, err := d.ScanStatusCard("dave", "CARD-BREAK"); err != nil {
		t.Fatal(err)
	}
	if on, err := d.OnBreak("dave"); err != nil || on {
		t.Fatalf("OnBreak after scanning the break card again = %v, %v, want false (toggled off)", on, err)
	}
	if _, err := d.ScanStatusCard("dave", "CARD-DELAY"); err == nil {
		t.Error("expected an error scanning a non-break card code -- Delay/No Work are admin-applied only")
	}
}
