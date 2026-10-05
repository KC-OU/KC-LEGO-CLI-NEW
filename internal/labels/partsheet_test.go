package labels

import (
	"bytes"
	"strings"
	"testing"
)

func a4Size(t *testing.T) Size {
	t.Helper()
	s, err := SizeByID("a4")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPartsSheetIsAWellFormedMultiPagePDF(t *testing.T) {
	lines := make([]PartSheetLine, 25) // more than one a4 grid's worth (21) forces a second page
	for i := range lines {
		lines[i] = PartSheetLine{PartNum: "3001", ColorName: "Red", Name: "Brick 2 x 4", Location: "A-02-01", Need: 10}
	}
	b := PartsSheet(lines, a4Size(t))
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
	s := string(PartsSheet(lines, a4Size(t)))
	for _, want := range []string{"3001", "Brick 2 x 4", "A-02-01", "Need: 10", "3794", "Plate 1 x 2"} {
		if !strings.Contains(s, want) {
			t.Errorf("sheet missing %q", want)
		}
	}
}

func TestPartsSheetWithNoLinesIsStillAValidEmptyPDF(t *testing.T) {
	b := PartsSheet(nil, a4Size(t))
	if !bytes.HasPrefix(b, []byte("%PDF-1.4")) {
		t.Fatal("expected a PDF 1.4 header even with no lines")
	}
}

// TestPartsSheetOnStickyStockIsOnePartPerPage is the direct test for
// "allow it to be printed on sticky labels": a non-sheet size (small
// thermal/sticky stock, not a4/letter) must print one part per page/label,
// not try to grid them.
func TestPartsSheetOnStickyStockIsOnePartPerPage(t *testing.T) {
	small, err := SizeByID("50x30")
	if err != nil {
		t.Fatal(err)
	}
	lines := []PartSheetLine{
		{PartNum: "3001", ColorName: "Red", Name: "Brick 2 x 4", Need: 10},
		{PartNum: "3794", ColorName: "Black", Name: "Plate 1 x 2 w/ Clip", Need: 14},
	}
	b := PartsSheet(lines, small)
	if n := bytes.Count(b, []byte("/Type /Page ")); n != 2 {
		t.Errorf("2 parts on sticky stock should be 2 pages (one label each), found %d", n)
	}
}

// TestPartsSheetHTMLIncludesEachPartsPicture is the direct test for "add
// images to each part" — the PDF path is vector-only and can't, so the
// picture only ever shows up via the HTML path.
func TestPartsSheetHTMLIncludesEachPartsPicture(t *testing.T) {
	lines := []PartSheetLine{
		{PartNum: "3001", ColorName: "Red", Name: "Brick 2 x 4", Need: 10, ImageURL: "https://cdn.rebrickable.com/media/parts/ldraw/4/3001.png"},
		{PartNum: "3794", ColorName: "Black", Name: "Plate 1 x 2 w/ Clip", Need: 14}, // no picture
	}
	s := string(PartsSheetHTML(lines, a4Size(t)))
	if !strings.Contains(s, `href="https://cdn.rebrickable.com/media/parts/ldraw/4/3001.png"`) {
		t.Error("expected an <image> referencing 3001's picture URL")
	}
	if strings.Count(s, "<image") != 1 {
		t.Errorf("expected exactly one <image> (3794 has no ImageURL), got %d", strings.Count(s, "<image"))
	}
}
