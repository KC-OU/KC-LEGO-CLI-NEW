package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

func editTestCheck() *lego.SetCheck {
	return &lego.SetCheck{SetNum: "75192-1", Lines: []lego.CheckLine{
		{PartNum: "3001", ColorID: 4, ColorName: "Red", Need: 10, Have: 5},
		{PartNum: "3023", ColorID: 1, ColorName: "Blue", Need: 20, Have: 20},
	}}
}

// TestApplyEditsMalformedInput checks that every spec a person could plausibly
// mistype — or a hostile caller could feed via --json/scripting — is rejected
// with a usage error rather than a panic, an out-of-range write, or a silent
// no-op that looks like it worked.
func TestApplyEditsMalformedInput(t *testing.T) {
	cases := []struct {
		name, spec string
	}{
		{"missing fields", "3001:4"},
		{"too many fields", "3001:4:2:extra"},
		{"empty quantity", "3001:4:"},
		{"non-numeric quantity", "3001:4:abc"},
		{"negative quantity", "3001:4:-1"},
		{"huge quantity overflows int", "3001:4:99999999999999999999"},
		{"unknown part", "9999:4:1"},
		{"unknown colour id", "3001:99:1"},
		{"unknown colour name", "3001:purple:1"},
		{"blank colour", "3001::1"},
		{"blank part", ":4:1"},
		{"control characters in part", "3001\x00:4:1"},
		{"unicode quantity digits", "3001:4:١٢"}, // Arabic-Indic digits: strconv.Atoi must not accept these
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := editTestCheck()
			err := applyEdits(c, tc.spec, func(l *lego.CheckLine, n int) { l.Have = n })
			if err == nil {
				t.Fatalf("spec %q: expected a usage error, got none (lines now %+v)", tc.spec, c.Lines)
			}
		})
	}
}

// TestApplyEditsBlankSpecIsANoOp confirms a spec with nothing but commas or
// whitespace changes nothing and errors on nothing — it's what a trailing
// comma in a real spec (see TestApplyEditsValidInput) leaves behind.
func TestApplyEditsBlankSpecIsANoOp(t *testing.T) {
	for _, spec := range []string{",", "   ", "", ",,,"} {
		c := editTestCheck()
		if err := applyEdits(c, spec, func(l *lego.CheckLine, n int) { l.Have = n }); err != nil {
			t.Errorf("spec %q: unexpected error: %v", spec, err)
		}
		if c.Lines[0].Have != 5 || c.Lines[1].Have != 20 {
			t.Errorf("spec %q: lines changed: %+v", spec, c.Lines)
		}
	}
}

// TestApplyEditsValidInput is the companion happy path: comma-separated
// edits by id and by name both land on the right line, untouched lines stay
// untouched, and blank/whitespace-only items between commas are skipped.
func TestApplyEditsValidInput(t *testing.T) {
	c := editTestCheck()
	err := applyEdits(c, " 3001:4:8 ,, 3023:Blue:20,", func(l *lego.CheckLine, n int) { l.Have = n })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Lines[0].Have != 8 {
		t.Errorf("3001 Have = %d, want 8", c.Lines[0].Have)
	}
	if c.Lines[1].Have != 20 {
		t.Errorf("3023 Have = %d, want 20", c.Lines[1].Have)
	}
}

// checkedSetShort finishes a check for setNum with exactly one line short by
// shortBy, the minimal "something's missing" fixture missing-sheet needs.
func checkedSetShort(t *testing.T, db *lego.DB, invID int, setNum string, shortBy int) {
	t.Helper()
	db.Exec(`INSERT INTO cat_sets (set_num, name, year, theme_id, num_parts, img_url) VALUES (?, ?, 2020, 0, 10, '')`, setNum, "Set "+setNum)
	db.Exec(`INSERT INTO cat_inventories (id, version, set_num) VALUES (?, 1, ?)`, invID, setNum)
	db.Exec(`INSERT INTO cat_inventory_parts (inventory_id, part_num, color_id, quantity) VALUES (?, '3001', 4, 10)`, invID)
	c, err := db.NewCheck(context.Background(), nil, setNum, lego.CheckIntake, "kc")
	if err != nil {
		t.Fatal(err)
	}
	c.Lines[0].Have = c.Lines[0].Need - shortBy
	if _, err := db.FinishCheck(c); err != nil {
		t.Fatal(err)
	}
}

// TestMissingSheetCombinesSeveralSetsAndSkipsUnchecked is the direct test
// for "bulk-print sticky labels for everything missing, across multiple
// sets": two checked sets' missing lines land on one sheet, and a third,
// never-checked set is skipped with a warning rather than failing the
// whole command.
func TestMissingSheetCombinesSeveralSetsAndSkipsUnchecked(t *testing.T) {
	db := legoEnv(t)
	seedCatalog(t, db)
	checkedSetShort(t, db, 1, "1-1", 3)
	checkedSetShort(t, db, 2, "2-1", 5)

	stdout, code := run(t, "lego", "missing-sheet", "1-1", "2-1", "9999-1", "--format", "html")
	if code != 0 {
		t.Fatalf("missing-sheet: code=%d out=%q", code, stdout)
	}
	if !strings.Contains(stdout, "Need: 3") || !strings.Contains(stdout, "Need: 5") {
		t.Errorf("sheet should show each set's shortfall:\n%s", stdout)
	}

	if _, code := run(t, "lego", "missing-sheet", "9999-1"); code == 0 {
		t.Error("every given set unchecked (or nothing missing) should be a usage error, not a quiet success")
	}
}

// TestMissingSheetHintsAtTheConfiguredPDFEditorForPDFOutputOnly covers the
// Stirling-PDF integration: the hint only shows up for --format pdf (there's
// nothing to touch up about an html sheet the same way) and only when an
// editor URL has actually been configured.
func TestMissingSheetHintsAtTheConfiguredPDFEditorForPDFOutputOnly(t *testing.T) {
	db := legoEnv(t)
	seedCatalog(t, db)
	checkedSetShort(t, db, 1, "1-1", 3)
	out := filepath.Join(t.TempDir(), "sheet.pdf")

	stdout, code := run(t, "lego", "missing-sheet", "1-1", "--format", "pdf", "-o", out)
	if code != 0 {
		t.Fatalf("missing-sheet --format pdf: code=%d out=%q", code, stdout)
	}
	if strings.Contains(stdout, "pdf-editor") {
		t.Errorf("no WMS_STIRLING_PDF_URL configured — expected no hint, got:\n%s", stdout)
	}

	t.Setenv(config.StirlingPDFURL, "https://pdf.example.com/editor")
	stdout, code = run(t, "lego", "missing-sheet", "1-1", "--format", "pdf", "-o", out)
	if code != 0 {
		t.Fatalf("missing-sheet --format pdf (with editor configured): code=%d out=%q", code, stdout)
	}
	if !strings.Contains(stdout, "https://pdf.example.com/editor") {
		t.Errorf("expected the configured editor URL to be hinted at, got:\n%s", stdout)
	}

	stdout, code = run(t, "lego", "missing-sheet", "1-1", "--format", "html")
	if code != 0 {
		t.Fatalf("missing-sheet --format html: code=%d out=%q", code, stdout)
	}
	if strings.Contains(stdout, "pdf-editor") {
		t.Errorf("html output shouldn't hint at a PDF editor, got:\n%s", stdout)
	}
}
