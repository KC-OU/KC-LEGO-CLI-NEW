package lego

import (
	"path/filepath"
	"testing"
)

func TestLogEventAndAdminEvents(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "lego.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := db.LogEvent(EventForcedOff, "admin", "dave", "75192-1: dave -> —"); err != nil {
		t.Fatal(err)
	}
	if err := db.LogEvent(EventMessage, "admin", "sam", "hello"); err != nil {
		t.Fatal(err)
	}
	evs, err := db.AdminEvents(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 2 {
		t.Fatalf("AdminEvents = %d, want 2", len(evs))
	}
	if evs[0].Kind != EventMessage || evs[0].Actor != "admin" || evs[0].Target != "sam" {
		t.Errorf("newest event first = %+v, want the message event", evs[0])
	}
	if evs[1].Kind != EventForcedOff {
		t.Errorf("second event = %+v, want the forced_off event", evs[1])
	}
}

func TestAdminEventsRespectsLimit(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "lego.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for i := 0; i < 5; i++ {
		if err := db.LogEvent(EventMessage, "admin", "sam", "hi"); err != nil {
			t.Fatal(err)
		}
	}
	evs, err := db.AdminEvents(2)
	if err != nil || len(evs) != 2 {
		t.Fatalf("AdminEvents(2) = %d, %v, want 2", len(evs), err)
	}
}
