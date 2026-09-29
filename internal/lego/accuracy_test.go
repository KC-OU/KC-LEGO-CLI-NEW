package lego

import (
	"path/filepath"
	"testing"
)

func TestDeductionForBands(t *testing.T) {
	cases := []struct {
		missing int
		want    float64
		escl    bool
	}{
		{0, 0, false},
		{1, 2, false},
		{5, 6, false},
		{6, 10, false},
		{15, 14, false},
		{16, 15, false},
		{20, 20, false},
		{21, 0, true},
		{40, 0, true},
	}
	for _, c := range cases {
		got, escl := DeductionFor(c.missing)
		if got != c.want || escl != c.escl {
			t.Errorf("DeductionFor(%d) = (%v, %v), want (%v, %v)", c.missing, got, escl, c.want, c.escl)
		}
	}
}

func TestRecordCheckOutcomeAndAccuracyToday(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "lego.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	acc, err := db.AccuracyToday("dave", AccuracyChecker)
	if err != nil || acc != 100 {
		t.Fatalf("fresh accuracy = %v, %v, want 100", acc, err)
	}

	delta, escl, err := db.RecordCheckOutcome("dave", AccuracyChecker, TicketCheck, "75192-1", 300, 20)
	if err != nil {
		t.Fatal(err)
	}
	if escl {
		t.Fatal("20 missing must not escalate")
	}
	if delta != -20 {
		t.Errorf("delta = %v, want -20", delta)
	}
	acc, err = db.AccuracyToday("dave", AccuracyChecker)
	if err != nil || acc != 80 {
		t.Fatalf("accuracy after 20 missing = %v, %v, want 80", acc, err)
	}

	// A clean set claws back half the remaining deficit (20), not all of it.
	delta, escl, err = db.RecordCheckOutcome("dave", AccuracyChecker, TicketCheck, "10254-1", 100, 0)
	if err != nil || escl {
		t.Fatalf("clean set: %v %v %v", delta, escl, err)
	}
	if delta != 10 {
		t.Errorf("recovery delta = %v, want 10", delta)
	}
	acc, _ = db.AccuracyToday("dave", AccuracyChecker)
	if acc != 90 {
		t.Errorf("accuracy after one clean set = %v, want 90", acc)
	}

	// 21+ never auto-deducts.
	delta, escl, err = db.RecordCheckOutcome("dave", AccuracyChecker, TicketCheck, "10281-1", 50, 25)
	if err != nil {
		t.Fatal(err)
	}
	if !escl || delta != 0 {
		t.Errorf("25 missing: delta=%v escalate=%v, want 0/true", delta, escl)
	}
	acc, _ = db.AccuracyToday("dave", AccuracyChecker)
	if acc != 90 {
		t.Errorf("accuracy must be unchanged by an escalation, got %v", acc)
	}

	// Roles are independent.
	pickerAcc, _ := db.AccuracyToday("dave", AccuracyPicker)
	if pickerAcc != 100 {
		t.Errorf("dave's picker accuracy leaked from his checker score: %v", pickerAcc)
	}

	hist, err := db.AccuracyHistory("dave", AccuracyChecker, "")
	if err != nil || len(hist) != 3 {
		t.Fatalf("history = %d entries, %v, want 3", len(hist), err)
	}
}

func TestDockAccuracy(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "lego.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := db.DockAccuracy("sam", AccuracyPicker, 15, "too many mis-picks this week", "admin"); err != nil {
		t.Fatal(err)
	}
	acc, err := db.AccuracyToday("sam", AccuracyPicker)
	if err != nil || acc != 85 {
		t.Fatalf("accuracy after a manual dock = %v, %v, want 85", acc, err)
	}
}
