package lego

import (
	"path/filepath"
	"testing"
)

func TestOpenAccuracyEscalationsAndFiling(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "lego.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, _, err := db.RecordCheckOutcome("dave", AccuracyChecker, TicketCheck, "75192-1", 500, 25); err != nil {
		t.Fatal(err)
	}
	open, err := db.OpenAccuracyEscalations()
	if err != nil || len(open) != 1 || open[0].Username != "dave" || open[0].Missing != 25 {
		t.Fatalf("OpenAccuracyEscalations = %+v, %v, want one for dave with 25 missing", open, err)
	}

	id, err := db.FileAccuracyReport(open[0].ID, "dave", AccuracyChecker, "admin", "Checked the set, parts genuinely missing from the delivery.", "Reordered the missing parts.", true)
	if err != nil || id == 0 {
		t.Fatalf("FileAccuracyReport = %d, %v", id, err)
	}

	open, err = db.OpenAccuracyEscalations()
	if err != nil || len(open) != 0 {
		t.Fatalf("after filing, OpenAccuracyEscalations = %+v, %v, want none left", open, err)
	}

	reports, err := db.AccuracyReportsFor("dave", AccuracyChecker)
	if err != nil || len(reports) != 1 {
		t.Fatalf("AccuracyReportsFor = %+v, %v, want 1", reports, err)
	}
	r := reports[0]
	if r.ReviewedBy != "admin" || !r.TalkRequested || r.ActionTaken != "Reordered the missing parts." {
		t.Errorf("report = %+v, fields don't match what was filed", r)
	}
}

func TestAnotherEscalationStaysOpenWhileOnlyOneIsReported(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "lego.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, _, err := db.RecordCheckOutcome("dave", AccuracyChecker, TicketCheck, "75192-1", 500, 25); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.RecordCheckOutcome("pat", AccuracyPicker, TicketOrder, "ORD-1", 100, 22); err != nil {
		t.Fatal(err)
	}
	open, err := db.OpenAccuracyEscalations()
	if err != nil || len(open) != 2 {
		t.Fatalf("OpenAccuracyEscalations = %+v, %v, want 2", open, err)
	}

	// Report only dave's — pat's must stay open.
	var daveID int64
	for _, e := range open {
		if e.Username == "dave" {
			daveID = e.ID
		}
	}
	if _, err := db.FileAccuracyReport(daveID, "dave", AccuracyChecker, "admin", "x", "y", false); err != nil {
		t.Fatal(err)
	}
	open, err = db.OpenAccuracyEscalations()
	if err != nil || len(open) != 1 || open[0].Username != "pat" {
		t.Fatalf("OpenAccuracyEscalations = %+v, %v, want only pat's left open", open, err)
	}
}
