package labels

import (
	"bytes"
	"fmt"
	"html"
	"strconv"
	"strings"
)

// PartSheetLine is one entry on a printable parts-reference sheet: a part
// number with a scannable CODE128 barcode, its description, where it lives,
// and how many are needed — so the Parts Check screen's optional scanner
// mode (set_check.go) has something to scan against without needing each
// part's own retail packaging. Scanning stays optional either way; this is
// just a printable aid, not a requirement. ImageURL is only drawn by
// PartsSheetHTML — the PDF path is vector-only (text and filled rectangles,
// same as every other label here), so a part picture needs the HTML path,
// printed from a browser (File > Print > Save as PDF) same as any other
// picture-bearing export already documented in docs/guides/export-import.md.
type PartSheetLine struct {
	PartNum, ColorName, Name, Location, ImageURL string
	Need                                         int
}

// PartsSheet renders lines as barcode tags on the given stock — any size
// from Sizes, not just A4: a sheet size (a4/letter) grids several per page,
// same geometry draw()/PDF() use for set labels; anything else (the small
// thermal/sticky sizes — 50x30, 40x30, 62x29, ...) prints one part per
// label, one label per page, so a part can get its own sticky tag on a bin
// or box instead of only living on an A4 sheet. Same raw-PDF-byte approach
// as PDF() (render.go) and the same CODE128 encoder (barcodeOps, labels.go).
func PartsSheet(lines []PartSheetLine, s Size) []byte {
	const pt = 72 / 25.4
	pageW, pageH := s.W, s.H
	per := 1
	if s.Sheet() {
		pageW, pageH = s.PageW, s.PageH
		per = s.Cols * s.Rows
	}
	var pages []string
	for start := 0; start < len(lines); start += per {
		var c strings.Builder
		for i := start; i < min(len(lines), start+per); i++ {
			ox, oy := 0.0, 0.0
			if s.Sheet() {
				k := i - start
				ox, oy = s.Left+float64(k%s.Cols)*s.PitchX, s.Top+float64(k/s.Cols)*s.PitchY
			}
			for _, o := range partSheetCellOps(lines[i], s.W, s.H, 0) {
				x := (ox + o.x) * pt
				if o.rect {
					y := (pageH - oy - o.y - o.h) * pt
					fmt.Fprintf(&c, "0 g %.2f %.2f %.2f %.2f re f\n", x, y, o.w*pt, o.h*pt)
					continue
				}
				font := "/F1"
				if o.bold {
					font = "/F2"
				}
				fmt.Fprintf(&c, "BT 0 g %s %.2f Tf %.2f %.2f Td (%s) Tj ET\n", font, o.size*pt, x, (pageH-oy-o.y)*pt, pdfEscape(o.text))
			}
		}
		pages = append(pages, c.String())
	}
	return multiPagePDF(pages, pageW*pt, pageH*pt)
}

// PartsSheetHTML is PartsSheet's picture-capable counterpart: same sheet/
// one-per-label sizing, plus each part's picture (when it has one) in a
// reserved strip at the top of its cell — "see the part clearly" the PDF
// path genuinely can't do (it has no image support at all; see PDF, HTML
// in render.go for set labels' own version of this same split). Print it
// from a browser at 100%, no margins, same as every other HTML label page.
func PartsSheetHTML(lines []PartSheetLine, s Size) []byte {
	var b bytes.Buffer
	pageW, pageH := s.W, s.H
	if s.Sheet() {
		pageW, pageH = s.PageW, s.PageH
	}
	fmt.Fprintf(&b, `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Parts sheet</title><style>
@page{size:%.2fmm %.2fmm;margin:0}
html,body{margin:0;padding:0;background:#fff}
.page{position:relative;width:%.2fmm;height:%.2fmm;overflow:hidden;page-break-after:always;break-after:page}
.page:last-child{page-break-after:auto;break-after:auto}
svg{position:absolute;display:block}
@media screen{body{background:#ccc}.page{background:#fff;margin:8px auto;box-shadow:0 1px 4px #0005}}
</style></head><body>
`, pageW, pageH, pageW, pageH)
	per := 1
	if s.Sheet() {
		per = s.Cols * s.Rows
	}
	imgH := min(s.H*0.4, 16.0) // reserved picture strip, capped so text/barcode still fit on small stock
	for start := 0; start < len(lines); start += per {
		b.WriteString(`<div class="page">`)
		for i := start; i < min(len(lines), start+per); i++ {
			l := lines[i]
			x, y := 0.0, 0.0
			if s.Sheet() {
				k := i - start
				x, y = s.Left+float64(k%s.Cols)*s.PitchX, s.Top+float64(k/s.Cols)*s.PitchY
			}
			cellImgH := imgH
			if l.ImageURL == "" {
				cellImgH = 0
			}
			fmt.Fprintf(&b, `<svg style="left:%.2fmm;top:%.2fmm" width="%.2fmm" height="%.2fmm" viewBox="0 0 %.2f %.2f" xmlns="http://www.w3.org/2000/svg">`, x, y, s.W, s.H, s.W, s.H)
			if l.ImageURL != "" {
				fmt.Fprintf(&b, `<image href="%s" x="%.2f" y="0.5" width="%.2f" height="%.2f" preserveAspectRatio="xMidYMid meet"/>`,
					html.EscapeString(l.ImageURL), s.W*0.5-cellImgH/2, cellImgH, cellImgH-1)
			}
			b.WriteString(svgOps(partSheetCellOps(l, s.W, s.H, cellImgH)))
			b.WriteString(`</svg>`)
		}
		b.WriteString("</div>\n")
	}
	b.WriteString("</body></html>\n")
	return b.Bytes()
}

// partSheetCellOps is one cell's content: part number, description,
// location/quantity, and a barcode along the bottom. imgH reserves a strip
// at the top for PartsSheetHTML's picture (0 from the plain PDF path, which
// draws no picture) — everything else shifts down by that much so nothing
// overlaps it.
func partSheetCellOps(l PartSheetLine, cellW, cellH, imgH float64) []op {
	pad := 2.0
	var ops []op
	y := pad + 3.2 + imgH
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

	barH := min(9.0, (cellH-imgH)*0.3)
	barY := cellH - pad - barH
	ops = append(ops, barcodeOps(l.PartNum, pad, barY, cellW-2*pad, barH)...)
	return ops
}
