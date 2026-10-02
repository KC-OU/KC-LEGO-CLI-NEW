package labels

import (
	"strconv"
	"strings"
	"testing"
)

func TestZPLHasOneFormatPerLabel(t *testing.T) {
	s, _ := SizeByID("62x100")
	b, err := ZPL([]Data{sample(), sample()}, s)
	if err != nil {
		t.Fatal(err)
	}
	out := string(b)
	if n := strings.Count(out, "^XA"); n != 2 {
		t.Errorf("^XA count = %d, want 2 (one per label)", n)
	}
	if n := strings.Count(out, "^XZ"); n != 2 {
		t.Errorf("^XZ count = %d, want 2", n)
	}
	if !strings.Contains(out, "75192-1") {
		t.Error("expected the set number in the ZPL output")
	}
}

func TestZPLSetsThePageSizeInDots(t *testing.T) {
	s, _ := SizeByID("38x90")
	b, err := ZPL([]Data{sample()}, s)
	if err != nil {
		t.Fatal(err)
	}
	out := string(b)
	wantW, wantH := mmToPx(38), mmToPx(90)
	if !strings.Contains(out, "^PW"+strconv.Itoa(wantW)) {
		t.Errorf("expected ^PW%d in:\n%s", wantW, out)
	}
	if !strings.Contains(out, "^LL"+strconv.Itoa(wantH)) {
		t.Errorf("expected ^LL%d in:\n%s", wantH, out)
	}
}

func TestZPLRejectsSheetStock(t *testing.T) {
	s, _ := SizeByID("letter")
	if _, err := ZPL([]Data{sample()}, s); err == nil {
		t.Error("ZPL on sheet stock should be refused, pointing at pdf/html")
	}
}

func TestZPLEscapesControlCharacters(t *testing.T) {
	if got := zplEscape("a^b~c"); got != "a_b_c" {
		t.Errorf("zplEscape = %q, want a_b_c", got)
	}
}
