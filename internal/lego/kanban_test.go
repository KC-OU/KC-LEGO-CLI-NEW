package lego

import (
	"testing"
	"time"
)

func TestSetBacklogStatusGatesAndReleases(t *testing.T) {
	d := newFixture(t)
	if err := d.UpsertSet(Set{SetNum: "75192-1", Name: "Millennium Falcon"}); err != nil {
		t.Fatal(err)
	}
	if err := d.SetBacklogStatus("75192-1", BacklogQueued, "admin"); err != nil {
		t.Fatal(err)
	}
	s := d.GetSetState("75192-1")
	if s.BacklogStatus != BacklogQueued {
		t.Fatalf("BacklogStatus = %q, want %q", s.BacklogStatus, BacklogQueued)
	}
	if s.BacklogReleasedBy != "" {
		t.Errorf("a still-queued set shouldn't have a released-by, got %q", s.BacklogReleasedBy)
	}

	if err := d.SetBacklogStatus("75192-1", BacklogReleased, "dana"); err != nil {
		t.Fatal(err)
	}
	s = d.GetSetState("75192-1")
	if s.BacklogStatus != BacklogReleased {
		t.Fatalf("BacklogStatus = %q, want %q", s.BacklogStatus, BacklogReleased)
	}
	if s.BacklogReleasedBy != "dana" || s.BacklogReleasedAt == "" {
		t.Errorf("expected released-by/at to be recorded, got %q / %q", s.BacklogReleasedBy, s.BacklogReleasedAt)
	}

	if err := d.SetBacklogStatus("75192-1", "bogus", "dana"); err == nil {
		t.Error("expected an invalid status to be rejected")
	}
}

func TestBacklogSetsListsOnlyBacklog(t *testing.T) {
	d := newFixture(t)
	for _, n := range []string{"1-1", "2-1", "3-1"} {
		if err := d.UpsertSet(Set{SetNum: n, Name: n}); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.SetBacklogStatus("1-1", BacklogQueued, "admin"); err != nil {
		t.Fatal(err)
	}
	if err := d.SetBacklogStatus("2-1", BacklogQueued, "admin"); err != nil {
		t.Fatal(err)
	}
	// 3-1 stays released (the default) -- never queued.

	backlog, err := d.BacklogSets()
	if err != nil {
		t.Fatal(err)
	}
	if len(backlog) != 2 {
		t.Fatalf("got %d backlog sets, want 2: %+v", len(backlog), backlog)
	}
	for _, s := range backlog {
		if s.SetNum == "3-1" {
			t.Error("3-1 was never queued, shouldn't be in the backlog list")
		}
	}
}

func TestAddKanbanNoteAppendOnlyAndVisibilityFilter(t *testing.T) {
	d := newFixture(t)
	tk, err := d.AssignTicket(TicketCheck, "75192-1", "Falcon", "kc", "", "", "admin")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := d.AddKanbanNote(tk.ID, "", -1, 0, "box already opened", "shared", "kc"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.AddKanbanNote(tk.ID, "3001", 4, 0, "recheck this colour", "private", "kc"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.AddKanbanNote(tk.ID, "", -1, 0, "", "private", "kc"); err == nil {
		t.Error("an empty note body should be rejected")
	}

	admin, err := d.KanbanNotes(tk.ID, "someone-else", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(admin) != 2 {
		t.Fatalf("admin should see both notes, got %d", len(admin))
	}

	author, err := d.KanbanNotes(tk.ID, "kc", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(author) != 2 {
		t.Fatalf("the author should see both of their own notes, got %d", len(author))
	}

	other, err := d.KanbanNotes(tk.ID, "someone-else", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 1 || other[0].Visibility != "shared" {
		t.Fatalf("a non-author/non-admin should see only the shared note, got %+v", other)
	}
}

func TestKanbanBoardShowsOnlyOwnJobs(t *testing.T) {
	d := newFixture(t)
	tkA, err := d.AssignTicket(TicketCheck, "75192-1", "Falcon", "kc", "", "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.ClaimTicket(tkA.ID, "kc"); err != nil {
		t.Fatal(err)
	}
	tkB, err := d.AssignTicket(TicketCheck, "10307-1", "Eiffel", "dave", "", "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.ClaimTicket(tkB.ID, "dave"); err != nil {
		t.Fatal(err)
	}

	board, err := d.KanbanBoard("kc")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range board {
		if c.Ticket.AssignedTo != "" && c.Ticket.AssignedTo != "kc" {
			t.Errorf("kc's board should never show another user's card, got assigned_to=%q", c.Ticket.AssignedTo)
		}
	}
	found := false
	for _, c := range board {
		if c.Ticket.ID == tkA.ID {
			found = true
			if c.Column != KanbanColActive {
				t.Errorf("a claimed ticket should be in the Active column, got %q", c.Column)
			}
		}
	}
	if !found {
		t.Error("kc's own current ticket should appear on their board")
	}
}

func TestSaveCheckSetsDeprioritizedAboveThreshold(t *testing.T) {
	d := newFixture(t)
	c := &SetCheck{SetNum: "75192-1", Status: StatusDraft, StartedAt: time.Now(), Lines: []CheckLine{
		{PartNum: "3001", ColorID: 4, Need: 60, Have: 10},  // missing 50 -- at the default threshold
		{PartNum: "3002", ColorID: 4, Need: 10, Have: 10},  // missing 0
		{PartNum: "3003", ColorID: 4, Need: 100, Have: 51}, // missing 49 -- just under
	}}
	if err := d.SaveCheck(c); err != nil {
		t.Fatal(err)
	}
	got, err := d.GetCheck(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range got.Lines {
		want := l.PartNum == "3001"
		if l.Deprioritized != want {
			t.Errorf("part %s: Deprioritized = %v, want %v (missing %d)", l.PartNum, l.Deprioritized, want, l.Missing())
		}
	}
}
