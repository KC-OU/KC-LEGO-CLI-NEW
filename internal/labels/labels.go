// Package labels makes printable set labels for thermal label printers (Labelnize
// PM260 and similar 4x6 / 100x150 mm, small 50x30 / 40x30 mm, Brother QL 62 mm)
// and sheet labels for inkjet/laser printers (A4 3x7 Avery L7160, Letter 3x10
// Avery 5160). Each label is drawn once as a list of operations in millimetres —
// text and filled rectangles (the Code 128 barcode and QR code are just rectangles)
// — and that list becomes either an HTML page of exact-size SVGs (print it from a
// browser with margins off) or a PDF (vector, no fonts to embed: Helvetica).
package labels

import (
	"fmt"
	"strings"
	"time"

	"github.com/boombuler/barcode"
	"github.com/boombuler/barcode/code128"
	"github.com/boombuler/barcode/qr"
)

// Data is what a set label says.
type Data struct {
	SetNum, Name string
	Year         int
	Pieces       int
	Missing      int
	OnOrder      int
	Checked      bool
	CheckedBy    string
	CheckedAt    time.Time
	Location     string
	QR           string // encoded in the QR code (a link); "" = none
	Barcode      string // Code 128 content; "" = the set number
	InstanceName string // shown small at the bottom, e.g. "KC-PARTS"
}

// Status is the label's one-word verdict.
func (d Data) Status() string {
	switch {
	case !d.Checked:
		return "NOT CHECKED"
	case d.Missing > 0 && d.OnOrder > 0:
		return fmt.Sprintf("INCOMPLETE - %d missing, %d on order", d.Missing, d.OnOrder)
	case d.Missing > 0:
		return fmt.Sprintf("INCOMPLETE - %d missing", d.Missing)
	}
	return "COMPLETE"
}

// Size is a label stock.
type Size struct {
	ID, Name string
	W, H     float64 // one label, mm
	// Sheet layout (0 = one label per page)
	PageW, PageH, Left, Top, PitchX, PitchY float64
	Cols, Rows                              int
}

// Sizes are the supported stocks, by ID.
var Sizes = []Size{
	{ID: "4x6", Name: "4 x 6 in thermal (Labelnize PM260, Zebra, Rollo…)", W: 101.6, H: 152.4},
	{ID: "100x150", Name: "100 x 150 mm thermal", W: 100, H: 150},
	{ID: "62x100", Name: "Brother QL 62 mm, 100 mm long", W: 62, H: 100},
	{ID: "62x29", Name: "Brother QL 62 x 29 mm", W: 62, H: 29},
	{ID: "50x30", Name: "50 x 30 mm thermal", W: 50, H: 30},
	{ID: "40x30", Name: "40 x 30 mm thermal", W: 40, H: 30},
	{ID: "a4", Name: "A4 sheet, 21 labels (Avery L7160, 63.5 x 38.1 mm)", W: 63.5, H: 38.1, PageW: 210, PageH: 297, Left: 7.2, Top: 15.1, PitchX: 66.04, PitchY: 38.1, Cols: 3, Rows: 7},
	{ID: "letter", Name: "US Letter sheet, 30 labels (Avery 5160, 66.7 x 25.4 mm)", W: 66.7, H: 25.4, PageW: 215.9, PageH: 279.4, Left: 4.8, Top: 12.7, PitchX: 69.85, PitchY: 25.4, Cols: 3, Rows: 10},
}

// SizeByID finds a stock ("4x6" also as "4X6", "100x150mm").
func SizeByID(id string) (Size, error) {
	id = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(id)), "mm")
	for _, s := range Sizes {
		if s.ID == id {
			return s, nil
		}
	}
	ids := make([]string, len(Sizes))
	for i, s := range Sizes {
		ids[i] = s.ID
	}
	return Size{}, fmt.Errorf("unknown label size %q: use %s", id, strings.Join(ids, ", "))
}

func (s Size) Sheet() bool { return s.Cols > 0 }

// ---- drawing ----

type op struct {
	rect       bool
	x, y, w, h float64 // mm from the label's top-left
	text       string
	size       float64 // font size, mm (cap height is about 0.72 of it)
	bold       bool
	gray       bool // a filled band drawn grey (the status bar background)
	white      bool // white text (on a dark band)
}

// charW is an estimate of Helvetica's average character width for a font size.
func charW(size float64, bold bool) float64 {
	if bold {
		return size * 0.58
	}
	return size * 0.52
}

func fit(s string, size, width float64, bold bool) string {
	s = asciiOnly(s)
	n := int(width / charW(size, bold))
	if n < 1 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return strings.TrimSpace(s[:n-1]) + "~"
}

// wrap splits s into at most lines lines that fit width.
func wrap(s string, size, width float64, bold bool, lines int) []string {
	s = asciiOnly(s)
	n := int(width / charW(size, bold))
	var out []string
	cur := ""
	for _, w := range strings.Fields(s) {
		switch {
		case cur == "":
			cur = w
		case len(cur)+1+len(w) <= n:
			cur += " " + w
		default:
			out = append(out, cur)
			cur = w
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	if len(out) > lines {
		out = out[:lines]
		out[lines-1] = fit(out[lines-1]+" ...", size, width, bold)
	}
	for i := range out {
		out[i] = fit(out[i], size, width, bold)
	}
	return out
}

// asciiOnly keeps text printable in Helvetica's WinAnsi encoding without a font file.
func asciiOnly(s string) string {
	r := strings.NewReplacer("—", "-", "–", "-", "·", "-", "’", "'", "“", "\"", "”", "\"", "…", "...", "×", "x")
	s = r.Replace(s)
	var b strings.Builder
	for _, c := range s {
		if c >= 32 && c < 127 {
			b.WriteRune(c)
		}
	}
	return b.String()
}

// modules draws a barcode's dark modules as rectangles in box (x, y, w, h).
func modules(bc barcode.Barcode, x, y, w, h float64, square bool) []op {
	b := bc.Bounds()
	cols, rows := b.Dx(), b.Dy()
	mw := w / float64(cols)
	mh := h / float64(rows)
	if square {
		mw = min(w/float64(cols), h/float64(rows))
		mh = mw
	}
	var out []op
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; {
			if dark(bc, b.Min.X+c, b.Min.Y+r) {
				start := c
				for c < cols && dark(bc, b.Min.X+c, b.Min.Y+r) {
					c++
				}
				o := op{rect: true, x: x + float64(start)*mw, y: y + float64(r)*mh, w: float64(c-start) * mw, h: mh}
				if square { // overlap neighbours a hair, or rasterisers leave white seams between rows
					o.w, o.h = o.w+0.02, o.h+0.02
				}
				out = append(out, o)
				continue
			}
			c++
		}
	}
	// A 1-D barcode is one row stretched to the full height.
	if rows == 1 && !square {
		for i := range out {
			out[i].h = h
		}
	}
	return out
}

func dark(bc barcode.Barcode, x, y int) bool {
	r, g, b, _ := bc.At(x, y).RGBA()
	return r+g+b < 3*0x8000
}

func qrOps(text string, x, y, side float64) []op {
	code, err := qr.Encode(text, qr.M, qr.Auto)
	if err != nil {
		return nil
	}
	return modules(code, x, y, side, side, true)
}

func barcodeOps(text string, x, y, w, h float64) []op {
	code, err := code128.Encode(asciiOnly(text))
	if err != nil {
		return nil
	}
	// Wide enough bars for a 203 dpi thermal head (a module of 0.375-0.5 mm is 3-4 dots),
	// narrower only when the label is too small; the rest is quiet zone.
	mods := float64(code.Bounds().Dx())
	bw := min(w*0.9, mods*0.5)
	return modules(code, x+(w-bw)/2, y, bw, h, false)
}

// draw lays out one label.
func draw(d Data, s Size) []op {
	W, H := s.W, s.H
	pad := max(1.2, min(W, H)*0.05)
	var ops []op
	txt := func(x, y, size float64, bold bool, t string) {
		ops = append(ops, op{x: x, y: y, size: size, bold: bold, text: t})
	}
	bc := d.Barcode
	if bc == "" {
		bc = d.SetNum
	}
	checked := "not checked yet"
	if d.Checked {
		checked = "Checked " + d.CheckedAt.Local().Format("2 Jan 2006") + " by " + d.CheckedBy
	}
	status := d.Status()
	info := fmt.Sprintf("%d  -  %d pcs", d.Year, d.Pieces)
	if d.Year == 0 {
		info = fmt.Sprintf("%d pcs", d.Pieces)
	}
	if H >= 90 { // large: 4x6, 100x150, 62x100
		big := min(W*0.16, 18)
		y := pad + big
		txt(pad, y, big, true, fit(d.SetNum, big, W-2*pad, true))
		nameSize := min(W*0.075, 7)
		for _, l := range wrap(d.Name, nameSize, W-2*pad, true, 2) {
			y += nameSize * 1.25
			txt(pad, y, nameSize, true, l)
		}
		body := min(W*0.055, 5)
		y += body * 1.6
		txt(pad, y, body, false, fit(info, body, W-2*pad, false))
		// status band
		y += body * 0.9
		bandH := body * 1.9
		ops = append(ops, op{rect: true, x: pad, y: y, w: W - 2*pad, h: bandH, gray: d.Missing > 0 || !d.Checked})
		ss := min(body, (W-2*pad-3)/(float64(len(status))*charW(1, true)))
		txt(pad+1.5, y+bandH*0.5+ss*0.36, ss, true, fit(status, ss, W-2*pad-3, true))
		ops[len(ops)-1].white = !(d.Missing > 0 || !d.Checked)
		y += bandH + body*1.4
		if d.Location != "" {
			txt(pad, y, body, false, fit("Location: "+d.Location, body, W-2*pad, false))
			y += body * 1.4
		}
		txt(pad, y, body*0.85, false, fit(checked, body*0.85, W-2*pad, false))
		bcH := min(H*0.1, 14)
		qrSide := min(W-2*pad, H-y-bcH-4*pad) * 0.9
		if d.QR != "" && qrSide > 15 {
			ops = append(ops, qrOps(d.QR, (W-qrSide)/2, y+pad, qrSide)...)
		}
		ops = append(ops, barcodeOps(bc, pad, H-pad-bcH-3, W-2*pad, bcH)...)
		txt(W/2-float64(len(bc))*charW(3, false)/2, H-pad, 3, false, bc)
		return ops
	}
	// small and sheet labels: text left, QR right, barcode under the text when it fits
	short := "COMPLETE"
	switch {
	case !d.Checked:
		short = "NOT CHECKED"
	case d.Missing > 0 && d.OnOrder > 0:
		short = fmt.Sprintf("%d MISSING, %d ORDERED", d.Missing, d.OnOrder)
	case d.Missing > 0:
		short = fmt.Sprintf("%d MISSING", d.Missing)
	}
	by := ""
	if d.Checked {
		by = d.CheckedBy + " " + d.CheckedAt.Local().Format("02.01.06")
	}
	// bottom band: the barcode across the label; above it, text left and QR right
	bandH := min(7, max(4.5, H*0.19))
	top := H - pad - bandH
	ops = append(ops, barcodeOps(bc, pad, top+0.4, W-2*pad, bandH-0.4)...)
	qrSide := 0.0
	if d.QR != "" {
		qrSide = min(top-pad, W*0.36)
	}
	tw := W - 2*pad - qrSide
	if qrSide > 0 {
		tw -= pad * 0.6
		ops = append(ops, qrOps(d.QR, W-pad-qrSide, pad, qrSide)...)
	}
	big := min((top-pad)*0.3, tw*0.17)
	y := pad + big*0.8
	txt(pad, y, big, true, fit(d.SetNum, big, tw, true))
	lines := []struct {
		t    string
		bold bool
	}{{d.Name, true}, {info, false}, {short, true}, {by, false}, {d.Location, false}}
	room := top - y
	small := max(1.6, min(big*0.5, room/(float64(len(lines))*1.15)))
	for _, l := range lines {
		if l.t == "" || y+small*1.15 > top-0.3 {
			continue
		}
		y += small * 1.15
		txt(pad, y, small, l.bold, fit(l.t, small, tw, l.bold))
	}
	return ops
}
