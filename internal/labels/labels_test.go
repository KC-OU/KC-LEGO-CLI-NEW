package labels

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

func sample() Data {
	return Data{SetNum: "75192-1", Name: "Millennium Falcon — Ultimate Collector Series", Year: 2017, Pieces: 7541, Missing: 3, OnOrder: 2,
		Checked: true, CheckedBy: "alex", CheckedAt: time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC), Location: "Shelf B2",
		QR: "https://rebrickable.com/sets/75192-1/"}
}

func TestEveryStockKeepsEverythingOnTheLabel(t *testing.T) {
	for _, s := range Sizes {
		ops := draw(sample(), s)
		var texts, rects int
		for _, o := range ops {
			if o.rect {
				rects++
				if o.x < -0.01 || o.y < -0.01 || o.x+o.w > s.W+0.01 || o.y+o.h > s.H+0.01 {
					t.Errorf("%s: rect outside the label: %+v", s.ID, o)
				}
				continue
			}
			texts++
			right := o.x + float64(len(o.text))*charW(o.size, o.bold)
			if o.x < 0 || right > s.W+0.5 || o.y > s.H+0.01 || o.y-o.size < -0.5 {
				t.Errorf("%s: text %q runs off the label (x %.1f..%.1f, y %.1f)", s.ID, o.text, o.x, right, o.y)
			}
		}
		if texts < 4 || rects < 50 { // set number, name, info, status + QR/barcode modules
			t.Errorf("%s: only %d text and %d rects", s.ID, texts, rects)
		}
		joined := ""
		for _, o := range ops {
			joined += o.text + "|"
		}
		wants := []string{"75192-1", "3 MISSING", "ALEX"}
		if s.H >= 30 {
			wants = append(wants, "SHELF B2") // the location is dropped on the shortest labels
		}
		for _, want := range wants {
			if !strings.Contains(strings.ToUpper(joined), want) {
				t.Errorf("%s: label lacks %q: %s", s.ID, want, joined)
			}
		}
	}
}

func TestHTMLAndPDF(t *testing.T) {
	s, _ := SizeByID("4x6")
	h := string(HTML([]Data{sample(), sample()}, s))
	if !strings.Contains(h, "@page{size:101.60mm 152.40mm;margin:0}") || strings.Count(h, "<svg") != 2 || strings.Contains(h, "—") {
		t.Fatalf("html:\n%s", h[:400])
	}
	a4, _ := SizeByID("a4")
	items := make([]Data, 25)
	for i := range items {
		items[i] = sample()
	}
	p := PDF(items, a4)
	if !bytes.HasPrefix(p, []byte("%PDF-1.4")) || !bytes.HasSuffix(p, []byte("%%EOF\n")) {
		t.Fatal("not a PDF")
	}
	if n := bytes.Count(p, []byte("/Type /Page /Parent")); n != 2 {
		t.Fatalf("25 labels on 21-up sheets = 2 pages, got %d", n)
	}
	// every xref offset points at "N 0 obj"
	m := regexp.MustCompile(`(?s)xref\n0 (\d+)\n0000000000 65535 f \n(.*)trailer`).FindSubmatch(p)
	if m == nil {
		t.Fatal("no xref")
	}
	for i, line := range strings.Split(strings.TrimSpace(string(m[2])), "\n") {
		off, _ := strconv.Atoi(line[:10])
		if !bytes.HasPrefix(p[off:], []byte(fmt.Sprintf("%d 0 obj", i+1))) {
			t.Fatalf("xref entry %d is wrong", i+1)
		}
	}
	if _, err := SizeByID("99x99"); err == nil {
		t.Error("unknown size")
	}
}
