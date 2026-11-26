package kanbnsync

import (
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

func TestMarkerRoundTrips(t *testing.T) {
	desc := description(lego.KanbanCard{
		Ticket:  lego.Ticket{ID: 42, Kind: lego.TicketCheck, Target: "21042", AssignedTo: "alice"},
		Summary: "M3 E1",
	})
	id, ok := ticketIDFromMarker(desc)
	if !ok || id != 42 {
		t.Fatalf("ticketIDFromMarker(%q) = %d, %v, want 42, true", desc, id, ok)
	}
}

func TestTicketIDFromMarkerRejectsNoMarker(t *testing.T) {
	if _, ok := ticketIDFromMarker("just some text, no marker here"); ok {
		t.Fatal("expected no marker to be found")
	}
}

func TestTitlePrefersTheTicketLabel(t *testing.T) {
	withLabel := lego.KanbanCard{Ticket: lego.Ticket{Label: "Check 21042", Kind: lego.TicketCheck, Target: "21042"}}
	if got := title(withLabel); got != "Check 21042" {
		t.Errorf("title() = %q, want %q", got, "Check 21042")
	}
	withoutLabel := lego.KanbanCard{Ticket: lego.Ticket{Kind: lego.TicketOrder, Target: "99"}}
	if got := title(withoutLabel); got != "order 99" {
		t.Errorf("title() = %q, want %q", got, "order 99")
	}
}

func TestLabelsForOnlyTagsAbandonedCards(t *testing.T) {
	s := &Syncer{label: map[bool]string{true: "abandoned-label-id"}}
	abandoned := lego.KanbanCard{Abandoned: true}
	if got := s.labelsFor(abandoned); len(got) != 1 || got[0] != "abandoned-label-id" {
		t.Errorf("labelsFor(abandoned) = %v, want [abandoned-label-id]", got)
	}
	active := lego.KanbanCard{Abandoned: false}
	if got := s.labelsFor(active); len(got) != 0 {
		t.Errorf("labelsFor(active) = %v, want empty", got)
	}
}

func TestSyncerIsNotConfiguredWithoutEverySetting(t *testing.T) {
	s := &Syncer{client: &client{}, listID: map[string]string{}}
	if s.Configured() {
		t.Fatal("expected Configured() to be false with nothing set")
	}
	s.client.apiKey = "key"
	s.listID[lego.KanbanColBacklog] = "b"
	s.listID[lego.KanbanColActive] = "a"
	if s.Configured() {
		t.Fatal("expected Configured() to still be false -- KanbanColDone's list is missing")
	}
	s.listID[lego.KanbanColDone] = "d"
	if !s.Configured() {
		t.Fatal("expected Configured() to be true once every list is set")
	}
}
