package reports

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestAccuracyReportPDFIsWellFormed(t *testing.T) {
	b := AccuracyReportPDF(AccuracyReportData{
		Username: "dave", Role: "checker", ReviewedBy: "admin",
		Target: "75192-1", Missing: 25,
		Summary:       "Checked the set — parts genuinely missing from the delivery, not a counting error.",
		ActionTaken:   "Reordered the missing parts and logged a supplier note.",
		TalkRequested: true,
		CreatedAt:     time.Date(2026, 10, 3, 9, 30, 0, 0, time.UTC),
	})
	if !bytes.HasPrefix(b, []byte("%PDF-1.4")) {
		t.Fatal("expected a PDF 1.4 header")
	}
	if !bytes.HasSuffix(bytes.TrimRight(b, "\n"), []byte("%%EOF")) {
		t.Error("expected a trailing EOF marker")
	}
	s := string(b)
	for _, want := range []string{"dave", "checker", "admin", "75192-1", "Reordered the missing parts"} {
		if !strings.Contains(s, want) {
			t.Errorf("PDF content missing %q", want)
		}
	}
}

func TestWrapTextKeepsWordsWhole(t *testing.T) {
	lines := wrapText("one two three four five six seven eight nine ten", 15)
	for _, l := range lines {
		if len(l) > 15 {
			t.Errorf("line %q exceeds the 15-char target", l)
		}
	}
	joined := strings.Join(lines, " ")
	if joined != "one two three four five six seven eight nine ten" {
		t.Errorf("wrapping lost or reordered words: %q", joined)
	}
}

func TestWrapTextEmptyStringIsOneEmptyLine(t *testing.T) {
	lines := wrapText("", 95)
	if len(lines) != 1 || lines[0] != "" {
		t.Fatalf("wrapText(\"\") = %+v, want one empty line", lines)
	}
}
