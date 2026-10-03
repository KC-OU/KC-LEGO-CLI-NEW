package labels

import (
	"bytes"
	"strings"
	"testing"
)

func TestPartsSheetIsAWellFormedMultiPagePDF(t *testing.T) {
	lines := make([]PartSheetLine, 25) // more than one a4 grid's worth (21) forces a second page
	for i := range lines {
		lines[i] = PartSheetLine{PartNum: "3001", ColorName: "Red", Name: "Brick 2 x 4", Location: "A-02-01", Need: 10}
	}
	b := PartsSheet(lines)
	if !bytes.HasPrefix(b, []byte("%PDF-1.4")) {
		t.Fatal("expected a PDF 1.4 header")
	}
	if n := bytes.Count(b, []byte("/Type /Page ")); n != 2 {
		t.Errorf("25 entries at 21/page should need 2 pages, found %d /Page objects", n)
	}
}

func TestPartsSheetContainsEachPartsDetails(t *testing.T) {
	lines := []PartSheetLine{
		{PartNum: "3001", ColorName: "Red", Name: "Brick 2 x 4", Location: "A-02-01", Need: 10},
		{PartNum: "3794", ColorName: "Black", Name: "Plate 1 x 2 w/ Clip", Location: "A-03-02", Need: 14},
	}
	s := string(PartsSheet(lines))
	for _, want := range []string{"3001", "Brick 2 x 4", "A-02-01", "Need: 10", "3794", "Plate 1 x 2"} {
		if !strings.Contains(s, want) {
			t.Errorf("sheet missing %q", want)
		}
	}
}

func TestPartsSheetWithNoLinesIsStillAValidEmptyPDF(t *testing.T) {
	b := PartsSheet(nil)
	if !bytes.HasPrefix(b, []byte("%PDF-1.4")) {
		t.Fatal("expected a PDF 1.4 header even with no lines")
	}
}
