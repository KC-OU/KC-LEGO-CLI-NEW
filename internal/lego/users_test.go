package lego

import "testing"

// TestRenameUserMovesHistoricalColumns seeds a ticket, an accuracy record and
// a kanban note under an old username, renames it, and confirms every piece
// of history resolves under the new name and nothing is left under the old
// one — plus that a live heartbeat row for the old name is dropped so a
// stale session is forced to reconnect.
func TestRenameUserMovesHistoricalColumns(t *testing.T) {
	d := newFixture(t)
	const oldUser, newUser = "7354", "7421"

	tk, err := d.AssignTicket(TicketCheck, "75192-1", "Falcon check", oldUser, "", "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.ClaimTicket(tk.ID, oldUser); err != nil {
		t.Fatal(err)
	}
	if _, err := d.AddKanbanNote(tk.ID, "", -1, 0, "a note", "shared", oldUser); err != nil {
		t.Fatal(err)
	}
	// missing > 0 so a real accuracy_log row is written (a perfect check with
	// nothing to recover from logs nothing at all, see RecordCheckOutcome).
	if _, escalate, err := d.RecordCheckOutcome(oldUser, AccuracyChecker, TicketCheck, tk.Target, 100, 2); err != nil || escalate {
		t.Fatalf("RecordCheckOutcome: escalate=%v err=%v", escalate, err)
	}
	if _, err := d.Exec(`INSERT INTO live_sessions (session_id, username, started_at, last_seen) VALUES ('sess-1', ?, datetime('now'), datetime('now'))`, oldUser); err != nil {
		t.Fatal(err)
	}

	if err := d.RenameUser(oldUser, newUser); err != nil {
		t.Fatalf("RenameUser: %v", err)
	}

	if got, err := d.CurrentTicket(newUser); err != nil || got == nil || got.ID != tk.ID {
		t.Fatalf("CurrentTicket(%q) = %v, %v, want ticket %d", newUser, got, err, tk.ID)
	}
	if got, err := d.CurrentTicket(oldUser); err != nil || got != nil {
		t.Fatalf("CurrentTicket(%q) = %v, %v, want nil", oldUser, got, err)
	}

	notes, err := d.KanbanNotes(tk.ID, newUser, true)
	if err != nil || len(notes) != 1 || notes[0].CreatedBy != newUser {
		t.Fatalf("KanbanNotes after rename = %+v, %v, want 1 note by %q", notes, err, newUser)
	}

	// AccuracyToday defaults to 100 for an account with NO rows at all (same
	// convention the always-visible header relies on for a brand-new
	// account), so presence is checked directly against accuracy_log rather
	// than via that default.
	var newRows, oldRows int
	if err := d.QueryRow(`SELECT COUNT(*) FROM accuracy_log WHERE username = ?`, newUser).Scan(&newRows); err != nil {
		t.Fatal(err)
	}
	if err := d.QueryRow(`SELECT COUNT(*) FROM accuracy_log WHERE username = ?`, oldUser).Scan(&oldRows); err != nil {
		t.Fatal(err)
	}
	if newRows != 1 {
		t.Errorf("accuracy_log rows for %q = %d, want 1", newUser, newRows)
	}
	if oldRows != 0 {
		t.Errorf("accuracy_log still has %d row(s) for the renamed-away username %q", oldRows, oldUser)
	}

	var liveCount int
	if err := d.QueryRow(`SELECT COUNT(*) FROM live_sessions WHERE username = ?`, oldUser).Scan(&liveCount); err != nil {
		t.Fatal(err)
	}
	if liveCount != 0 {
		t.Errorf("live_sessions still has a row for the renamed-away username %q", oldUser)
	}
}
