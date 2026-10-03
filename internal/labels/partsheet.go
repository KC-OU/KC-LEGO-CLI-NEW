package labels

import (
	"fmt"
	"strconv"
	"strings"
)

// PartSheetLine is one entry on a printable parts-reference sheet: a part
// number with a scannable CODE128 barcode, its description, where it lives,
// and how many are needed — so the Parts Check screen's optional scanner
// mode (set_check.go) has something to scan against without needing each
// part's own retail packaging. Scanning stays optional either way; this is
// just a printable aid, not a requirement.
type PartSheetLine struct {
	PartNum, ColorName, Name, Location string
	Need                               int
}

// PartsSheet renders lines as a multi-up A4 sheet of barcode tags — the
// same raw-PDF-byte approach as PDF() (render.go) and the same CODE128
// encoder (barcodeOps, labels.go), reusing the "a4" stock's grid geometry
// for cell placement, but with its own simpler per-cell content instead of
// draw()'s full set-label layout (no status, no QR — just enough to find
// and scan the right part).
func PartsSheet(lines []PartSheetLine) []byte {
	var s Size
	for _, sz := range Sizes {
		if sz.ID == "a4" {
			s = sz
			break
		}
	}
	const pt = 72 / 25.4
	per := s.Cols * s.Rows
	var pages []string
	for start := 0; start < len(lines); start += per {
		var c strings.Builder
		for i := start; i < min(len(lines), start+per); i++ {
			k := i - start
			ox, oy := s.Left+float64(k%s.Cols)*s.PitchX, s.Top+float64(k/s.Cols)*s.PitchY
			for _, o := range partSheetCellOps(lines[i], s.W, s.H) {
				x := (ox + o.x) * pt
				if o.rect {
					y := (s.PageH - oy - o.y - o.h) * pt
					fmt.Fprintf(&c, "0 g %.2f %.2f %.2f %.2f re f\n", x, y, o.w*pt, o.h*pt)
					continue
				}
				font := "/F1"
				if o.bold {
					font = "/F2"
				}
				fmt.Fprintf(&c, "BT 0 g %s %.2f Tf %.2f %.2f Td (%s) Tj ET\n", font, o.size*pt, x, (s.PageH-oy-o.y)*pt, pdfEscape(o.text))
			}
		}
		pages = append(pages, c.String())
	}
	return multiPagePDF(pages, s.PageW*pt, s.PageH*pt)
}

// partSheetCellOps is one cell's content: part number, description,
// location/quantity, and a barcode along the bottom.
func partSheetCellOps(l PartSheetLine, cellW, cellH float64) []op {
	pad := 2.0
	var ops []op
	y := pad + 3.2
	ops = append(ops, op{x: pad, y: y, size: 3.2, bold: true, text: fit(l.PartNum, 3.2, cellW-2*pad, true)})
	y += 4.2

	desc := l.ColorName
	if l.Name != "" {
		if desc != "" {
			desc += " "
		}
		desc += l.Name
	}
	if desc != "" {
		ops = append(ops, op{x: pad, y: y, size: 2.6, text: fit(desc, 2.6, cellW-2*pad, false)})
		y += 3.6
	}

	info := ""
	if l.Location != "" {
		info = "Loc: " + l.Location
	}
	if l.Need > 0 {
		if info != "" {
			info += "   "
		}
		info += "Need: " + strconv.Itoa(l.Need)
	}
	if info != "" {
		ops = append(ops, op{x: pad, y: y, size: 2.3, text: fit(info, 2.3, cellW-2*pad, false)})
	}

	barH := min(9.0, cellH*0.3)
	barY := cellH - pad - barH
	ops = append(ops, barcodeOps(l.PartNum, pad, barY, cellW-2*pad, barH)...)
	return ops
}
