package labels

import (
	"bytes"
	"fmt"
	"image"
	idraw "image/draw"
	"image/png"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"

	xdraw "golang.org/x/image/draw"
)

// pngDPI matches the 203 dpi thermal head every size's barcode/QR module
// width already assumes (modules(), labels.go) — a pixel-exact image a
// "Print Picture" dialog can send straight to the printer, with no
// page-size-matching step (unlike a PDF viewer's scaling options) to get
// wrong.
const pngDPI = 203.0

func mmToPx(mm float64) int {
	if mm <= 0 {
		return 0
	}
	return int(mm/25.4*pngDPI + 0.5)
}

// PNG renders exactly one label as a rasterized image. Unlike HTML/PDF, a
// PNG can't hold more than one page — a Brother QL/thermal label is
// physically one continuous strip anyway, printed one at a time, so this
// deliberately covers that case rather than inventing a multi-file return
// type; a batch (or sheet stock, which is genuinely multiple separate
// pages) stays PDF/HTML.
func PNG(items []Data, s Size) ([]byte, error) {
	if len(items) != 1 {
		return nil, fmt.Errorf("PNG export is one label at a time (got %d) — use --format pdf or html for a batch", len(items))
	}
	if s.Sheet() {
		return nil, fmt.Errorf("PNG export doesn't support sheet stock (%s) — use --format pdf or html", s.ID)
	}
	w, h := mmToPx(s.W), mmToPx(s.H)
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	idraw.Draw(img, img.Bounds(), image.White, image.Point{}, idraw.Src)
	for _, o := range draw(items[0], s) {
		if o.rect {
			drawRect(img, o)
		} else {
			drawText(img, o)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func drawRect(img *image.RGBA, o op) {
	x0, y0 := mmToPx(o.x), mmToPx(o.y)
	x1, y1 := mmToPx(o.x+o.w), mmToPx(o.y+o.h)
	rect := image.Rect(x0, y0, x1, y1)
	if !o.gray {
		idraw.Draw(img, rect, image.Black, image.Point{}, idraw.Src)
		return
	}
	// The gray status band: white fill, a thin black border — matches the
	// SVG/PDF renderers' white-fill-plus-stroke treatment of the same flag.
	idraw.Draw(img, rect, image.White, image.Point{}, idraw.Src)
	border := max(1, mmToPx(0.4))
	edges := []image.Rectangle{
		image.Rect(x0, y0, x1, y0+border),
		image.Rect(x0, y1-border, x1, y1),
		image.Rect(x0, y0, x0+border, y1),
		image.Rect(x1-border, y0, x1, y1),
	}
	for _, e := range edges {
		idraw.Draw(img, e, image.Black, image.Point{}, idraw.Src)
	}
}

// drawText rasterizes one text op with golang.org/x/image's built-in bitmap
// font (no TrueType file to embed), at its native size, then scales that
// onto the canvas at o.size's target size — simplest way to get one bitmap
// font to render legibly at the many sizes a label actually uses (3mm
// status text up to an 18mm set number). o.size is treated as the nominal
// font size in mm, matching how the SVG/PDF renderers already use it
// (font-size in SVG, Tf in PDF) — an approximation, not a typographic
// match for Helvetica; a real test print is the way to judge and tune it,
// the same as the Brother .lbx export.
func drawText(img *image.RGBA, o op) {
	if o.text == "" || o.size <= 0 {
		return
	}
	face := basicfont.Face7x13
	nativeW := font.MeasureString(face, o.text).Ceil()
	if nativeW <= 0 {
		return
	}
	m := face.Metrics()
	ascent, descent := m.Ascent.Ceil(), m.Descent.Ceil()
	nativeH := ascent + descent

	col := image.Black
	if o.white {
		col = image.White
	}
	scratch := image.NewRGBA(image.Rect(0, 0, nativeW, nativeH))
	(&font.Drawer{Dst: scratch, Src: col, Face: face, Dot: fixed.P(0, ascent)}).DrawString(o.text)

	destH := mmToPx(o.size)
	if destH <= 0 {
		return
	}
	destW := nativeW * destH / nativeH
	destX := mmToPx(o.x)
	destY := mmToPx(o.y) - destH*ascent/nativeH // o.y is the text baseline
	xdraw.NearestNeighbor.Scale(img, image.Rect(destX, destY, destX+destW, destY+destH), scratch, scratch.Bounds(), xdraw.Over, nil)
}
