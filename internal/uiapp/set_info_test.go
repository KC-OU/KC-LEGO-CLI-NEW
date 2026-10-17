package uiapp

import (
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

// TestSetInfoScreenRoundTripsInstructionBookFields covers "Where the Set Is
// Kept"'s two new fields end to end: build() reads the set's current
// instruction-book number/count, and submit() writes them back through
// DB.SetInstructionBook -- catching a positional-index mistake (the form's
// v[6]/v[7] slots), not just that the setter itself works.
func TestSetInfoScreenRoundTripsInstructionBookFields(t *testing.T) {
	app := newTestApp(t)
	if err := app.legoDB.UpsertSet(lego.Set{SetNum: "75192-1", Name: "Millennium Falcon"}); err != nil {
		t.Fatal(err)
	}
	app.workshop = &workshopState{set: "75192-1"}

	screen := setInfoScreen().(*formScreen)
	fields := screen.build(app)
	if len(fields) != 8 {
		t.Fatalf("expected 8 fields, got %d: %+v", len(fields), fields)
	}
	if fields[6].Value != "" {
		t.Errorf("instruction book # should start blank, got %q", fields[6].Value)
	}
	if fields[7].Value != "0" {
		t.Errorf("instruction book count should start at 0, got %q", fields[7].Value)
	}

	screen.submit(app, []string{"75192-1", "", "", "", "", "", "6644777/6644778", "2"})
	if app.messageErr {
		t.Fatalf("submit failed: %q", app.message)
	}

	owned, err := app.legoDB.GetSetByNum("75192-1")
	if err != nil {
		t.Fatal(err)
	}
	if owned.InstructionBookNumber != "6644777/6644778" || owned.InstructionBookCount != 2 {
		t.Errorf("got = %+v, want 6644777/6644778 / 2", owned)
	}

	// Rebuilding the form now shows what was just saved.
	fields = screen.build(app)
	if fields[6].Value != "6644777/6644778" || fields[7].Value != "2" {
		t.Errorf("rebuilt fields = %+v, want the saved values", fields)
	}
}

// TestLegoSetDetailShowsPhotosAttachedCount covers the read-only "Photos
// Attached" row on the set detail screen, alongside the existing
// Instruction Book rows.
func TestLegoSetDetailShowsPhotosAttachedCount(t *testing.T) {
	app := newTestApp(t)
	if err := app.legoDB.UpsertSet(lego.Set{SetNum: "75192-1", Name: "Millennium Falcon"}); err != nil {
		t.Fatal(err)
	}

	screen := legoSetLookupScreen().(*formScreen)
	screen.submit(app, []string{"75192-1"})
	if app.messageErr {
		t.Fatalf("lookup failed: %q", app.message)
	}
	photosRow := ""
	for _, row := range app.legoSetDetailRows {
		if row[0] == "Photos Attached" {
			photosRow = row[1]
		}
	}
	if photosRow != "0" {
		t.Errorf("Photos Attached = %q, want 0 before any are attached", photosRow)
	}

	// AttachSetPhoto only records a database row (a file is verified/stored
	// separately, by VerifyProofFile/StoreProof -- see photos_test.go in
	// internal/lego for that part), so a fake path is enough here.
	if _, err := app.legoDB.AttachSetPhoto("75192-1", "png", "/fake/box.png", "the box", "kc"); err != nil {
		t.Fatal(err)
	}

	screen.submit(app, []string{"75192-1"})
	for _, row := range app.legoSetDetailRows {
		if row[0] == "Photos Attached" {
			photosRow = row[1]
		}
	}
	if photosRow != "1" {
		t.Errorf("Photos Attached = %q, want 1 after attaching one", photosRow)
	}
}

func TestSetInfoScreenRejectsANonNumericInstructionBookCount(t *testing.T) {
	app := newTestApp(t)
	if err := app.legoDB.UpsertSet(lego.Set{SetNum: "75192-1", Name: "Millennium Falcon"}); err != nil {
		t.Fatal(err)
	}
	app.workshop = &workshopState{set: "75192-1"}

	screen := setInfoScreen().(*formScreen)
	screen.submit(app, []string{"75192-1", "", "", "", "", "", "6644777", "not-a-number"})
	if !app.messageErr {
		t.Error("expected an error for a non-numeric instruction book count")
	}
}
