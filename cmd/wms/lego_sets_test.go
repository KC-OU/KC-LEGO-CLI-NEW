package main

import (
	"testing"

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
