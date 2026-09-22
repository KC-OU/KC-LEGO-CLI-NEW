package labels

import (
	"bytes"
	"fmt"
	"html"
	"strings"
)

// HTML is a page of labels at their exact size: one per page on label stock, a
// grid on sheet stock. Print it with the browser's margins set to none and scale
// at 100%.
func HTML(items []Data, s Size) []byte {
	var b bytes.Buffer
	pageW, pageH := s.W, s.H
	if s.Sheet() {
		pageW, pageH = s.PageW, s.PageH
	}
	fmt.Fprintf(&b, `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Set labels</title><style>
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
	for start := 0; start < len(items); start += per {
		b.WriteString(`<div class="page">`)
		for i := start; i < min(len(items), start+per); i++ {
			x, y := 0.0, 0.0
			if s.Sheet() {
				k := i - start
				x, y = s.Left+float64(k%s.Cols)*s.PitchX, s.Top+float64(k/s.Cols)*s.PitchY
			}
			fmt.Fprintf(&b, `<svg style="left:%.2fmm;top:%.2fmm" width="%.2fmm" height="%.2fmm" viewBox="0 0 %.2f %.2f" xmlns="http://www.w3.org/2000/svg">`, x, y, s.W, s.H, s.W, s.H)
			b.WriteString(svgOps(draw(items[i], s)))
			b.WriteString(`</svg>`)
		}
		b.WriteString("</div>\n")
	}
	b.WriteString("</body></html>\n")
	return b.Bytes()
}

func svgOps(ops []op) string {
	var b strings.Builder
	for _, o := range ops {
		if o.rect {
			fill := "#000"
			if o.gray {
				fill = "#fff\" stroke=\"#000\" stroke-width=\"0.4"
			}
			fmt.Fprintf(&b, `<rect x="%.3f" y="%.3f" width="%.3f" height="%.3f" fill="%s"/>`, o.x, o.y, o.w, o.h, fill)
			continue
		}
		weight, fill := "400", "#000"
		if o.bold {
			weight = "700"
		}
		if o.white {
			fill = "#fff"
		}
		fmt.Fprintf(&b, `<text x="%.3f" y="%.3f" font-family="Helvetica,Arial,sans-serif" font-size="%.3f" font-weight="%s" fill="%s">%s</text>`,
			o.x, o.y, o.size, weight, fill, html.EscapeString(o.text))
	}
	return b.String()
}

// PDF is the same labels as a PDF (vector, Helvetica), one page per label, or one
// page per sheet.
func PDF(items []Data, s Size) []byte {
	const pt = 72 / 25.4
	pageW, pageH := s.W, s.H
	per := 1
	if s.Sheet() {
		pageW, pageH, per = s.PageW, s.PageH, s.Cols*s.Rows
	}
	var pages []string
	for start := 0; start < len(items); start += per {
		var c strings.Builder
		for i := start; i < min(len(items), start+per); i++ {
			ox, oy := 0.0, 0.0
			if s.Sheet() {
				k := i - start
				ox, oy = s.Left+float64(k%s.Cols)*s.PitchX, s.Top+float64(k/s.Cols)*s.PitchY
			}
			for _, o := range draw(items[i], s) {
				x := (ox + o.x) * pt
				if o.rect {
					y := (pageH - oy - o.y - o.h) * pt
					if o.gray {
						fmt.Fprintf(&c, "0.4 w %.2f %.2f %.2f %.2f re S\n", x, y, o.w*pt, o.h*pt)
					} else {
						fmt.Fprintf(&c, "0 g %.2f %.2f %.2f %.2f re f\n", x, y, o.w*pt, o.h*pt)
					}
					continue
				}
				font := "/F1"
				if o.bold {
					font = "/F2"
				}
				gray := "0 g"
				if o.white {
					gray = "1 g"
				}
				fmt.Fprintf(&c, "BT %s %s %.2f Tf %.2f %.2f Td (%s) Tj ET\n", gray, font, o.size*pt, x, (pageH-oy-o.y)*pt, pdfEscape(o.text))
			}
		}
		pages = append(pages, c.String())
	}
	if len(pages) == 0 {
		pages = []string{""}
	}
	// objects: 1 catalog, 2 pages, 3 font regular, 4 font bold, then page+content pairs
	var objs []string
	objs = append(objs, "<< /Type /Catalog /Pages 2 0 R >>")
	kids := make([]string, len(pages))
	for i := range pages {
		kids[i] = fmt.Sprintf("%d 0 R", 5+2*i)
	}
	objs = append(objs, fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(pages)))
	objs = append(objs, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>")
	objs = append(objs, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold /Encoding /WinAnsiEncoding >>")
	for i, content := range pages {
		objs = append(objs, fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %.2f %.2f] /Resources << /Font << /F1 3 0 R /F2 4 0 R >> >> /Contents %d 0 R >>",
			pageW*pt, pageH*pt, 6+2*i))
		objs = append(objs, fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(content), content))
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return b.Bytes()
}

func pdfEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `(`, `\(`, `)`, `\)`).Replace(asciiOnly(s))
}
