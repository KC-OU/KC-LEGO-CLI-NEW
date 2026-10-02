package labels

import (
	"fmt"
	"strings"
)

// zplDots uses the same mm-to-dots math as PNG's 203 dpi (most Zebra
// printers are 203 or 300 dpi) — one consistent dot-based scale across
// every raster/dot format in this package, not a separate constant to drift.
func zplDots(mm float64) int { return mmToPx(mm) }

// ZPL renders every label as Zebra Printer Language commands — one
// ^XA...^XZ format per label, concatenated, so a batch prints as separate
// labels off the same continuous roll (unlike PNG, ZPL text format has no
// one-page-per-file limit). Plain text, zero new dependencies. Walks the
// same []op list HTML/PDF/PNG do: op.rect becomes a solid ^GB graphic box,
// op.text an ^A0 scalable-font field, with ^FR ("field reverse") for
// white-on-dark text so it still prints correctly over an already-drawn
// black band. Only useful on Zebra-style hardware (raw socket/USB), not a
// Brother QL — for printing on different hardware too.
func ZPL(items []Data, s Size) ([]byte, error) {
	if s.Sheet() {
		return nil, fmt.Errorf("ZPL export doesn't support sheet stock (%s) — use --format pdf or html", s.ID)
	}
	w, h := zplDots(s.W), zplDots(s.H)
	var b strings.Builder
	for _, d := range items {
		fmt.Fprintf(&b, "^XA\n^PW%d\n^LL%d\n", w, h)
		for _, o := range draw(d, s) {
			if o.rect {
				writeZPLRect(&b, o)
			} else {
				writeZPLText(&b, o)
			}
		}
		b.WriteString("^XZ\n")
	}
	return []byte(b.String()), nil
}

func writeZPLRect(b *strings.Builder, o op) {
	x, y := zplDots(o.x), zplDots(o.y)
	w, h := max(1, zplDots(o.w)), max(1, zplDots(o.h))
	if o.gray {
		// White fill with a thin black border, same as the other renderers'
		// treatment of the status band when it isn't solid-filled.
		thick := max(1, zplDots(0.4))
		fmt.Fprintf(b, "^FO%d,%d^GB%d,%d,%d,B,0^FS\n", x, y, w, h, thick)
		return
	}
	thick := max(1, min(w, h)/2+1) // thick enough that the border meets in the middle: a solid fill
	fmt.Fprintf(b, "^FO%d,%d^GB%d,%d,%d,B,0^FS\n", x, y, w, h, thick)
}

func writeZPLText(b *strings.Builder, o op) {
	if o.text == "" || o.size <= 0 {
		return
	}
	height := zplDots(o.size)
	ascent := height * 8 / 10 // ^FO's y is the field's top, not a baseline like the other renderers' o.y
	x, y := zplDots(o.x), zplDots(o.y)-ascent
	width := int(float64(height) * 0.6) // ^A0 takes one width for the whole field, not per-glyph
	if o.white {
		b.WriteString("^FR\n") // reverse-prints the next field: white where it overlaps existing black
	}
	fmt.Fprintf(b, "^FO%d,%d^A0N,%d,%d^FD%s^FS\n", x, y, height, width, zplEscape(o.text))
}

// zplEscape neutralises ZPL's own control characters inside ^FD field data
// (^ starts a new command, ~ a printer control sequence) — text ops are
// already ASCII-only (see asciiOnly in labels.go) before they ever reach
// here, so this only ever has these two characters to worry about.
func zplEscape(s string) string {
	return strings.NewReplacer("^", "_", "~", "_").Replace(s)
}
