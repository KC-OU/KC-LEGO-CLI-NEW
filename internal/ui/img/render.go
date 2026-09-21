package img

import (
	"bytes"
	"image"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func bytesReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }

// Mode is how a picture is drawn.
type Mode string

const (
	ModeBlocks Mode = "blocks" // Unicode half-blocks in colour (the terminal downgrades to what it supports)
	ModeASCII  Mode = "ascii"  // plain ASCII shading, no colour, no UTF-8 needed
	ModeOff    Mode = "off"
)

// ParseMode reads MODERNWMS_TUI_IMAGES: auto, blocks, ascii or off. mono says colour
// is off (NO_COLOR), where half-blocks would show as solid bars, so auto picks ASCII.
// Kitty and Sixel are not offered: they do not work over telnet and do not survive
// the TUI's screen redraws.
func ParseMode(value string, mono bool) Mode {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "off", "none", "no", "0":
		return ModeOff
	case "ascii", "text":
		return ModeASCII
	case "blocks", "block", "color", "colour":
		if mono {
			return ModeASCII
		}
		return ModeBlocks
	}
	if mono {
		return ModeASCII
	}
	return ModeBlocks
}

// pixel is a colour composited onto the terminal (alpha kept, for transparent parts).
type pixel struct {
	r, g, b uint8
	solid   bool
}

// sample averages the source area of one output cell (a box filter), which keeps
// thin details visible when a large picture is shrunk to a few dozen columns.
func sample(src image.Image, x0, y0, x1, y1 float64) pixel {
	b := src.Bounds()
	ix0, iy0 := int(x0), int(y0)
	ix1, iy1 := max(int(x1), ix0+1), max(int(y1), iy0+1)
	var r, g, bl, a, n uint64
	for y := iy0; y < iy1; y++ {
		for x := ix0; x < ix1; x++ {
			if x < 0 || y < 0 || x >= b.Dx() || y >= b.Dy() {
				continue
			}
			pr, pg, pb, pa := src.At(b.Min.X+x, b.Min.Y+y).RGBA()
			r, g, bl, a, n = r+uint64(pr), g+uint64(pg), bl+uint64(pb), a+uint64(pa), n+1
		}
	}
	if n == 0 || a == 0 {
		return pixel{}
	}
	avgA := a / n
	if avgA < 0x4000 { // mostly transparent: leave the terminal background showing
		return pixel{}
	}
	// premultiplied sums -> straight colour
	return pixel{uint8(r * 0xffff / a >> 8), uint8(g * 0xffff / a >> 8), uint8(bl * 0xffff / a >> 8), true}
}

// fit returns the output size (columns, pixel rows) that keeps the picture's shape
// inside maxCols x maxRows text cells; a cell is twice as tall as wide, and each
// row holds two pixels in block mode.
func fit(w, h, maxCols, maxRows int) (cols, pixRows int) {
	if w <= 0 || h <= 0 || maxCols <= 0 || maxRows <= 0 {
		return 0, 0
	}
	maxPixRows := maxRows * 2
	cols = maxCols
	pixRows = h * cols / w
	if pixRows > maxPixRows {
		pixRows = maxPixRows
		cols = w * pixRows / h
	}
	return max(cols, 1), max(pixRows, 2)
}

// Render draws src into at most maxCols x maxRows character cells.
func Render(src image.Image, maxCols, maxRows int, mode Mode) string {
	if mode == ModeOff || src == nil {
		return ""
	}
	b := src.Bounds()
	cols, pixRows := fit(b.Dx(), b.Dy(), maxCols, maxRows)
	if cols == 0 {
		return ""
	}
	if pixRows%2 == 1 {
		pixRows++
	}
	sx, sy := float64(b.Dx())/float64(cols), float64(b.Dy())/float64(pixRows)
	at := func(cx, py int) pixel {
		return sample(src, float64(cx)*sx, float64(py)*sy, float64(cx+1)*sx, float64(py+1)*sy)
	}
	var out strings.Builder
	for row := 0; row < pixRows/2; row++ {
		for cx := 0; cx < cols; cx++ {
			top, bot := at(cx, row*2), at(cx, row*2+1)
			if mode == ModeASCII {
				out.WriteByte(asciiCell(top, bot))
				continue
			}
			out.WriteString(blockCell(top, bot))
		}
		out.WriteByte('\n')
	}
	return strings.TrimRight(out.String(), "\n")
}

func hexColor(p pixel) lipgloss.Color {
	const d = "0123456789abcdef"
	return lipgloss.Color("#" + string([]byte{d[p.r>>4], d[p.r&15], d[p.g>>4], d[p.g&15], d[p.b>>4], d[p.b&15]}))
}

func blockCell(top, bot pixel) string {
	switch {
	case !top.solid && !bot.solid:
		return " "
	case top.solid && bot.solid:
		return lipgloss.NewStyle().Foreground(hexColor(top)).Background(hexColor(bot)).Render("▀")
	case top.solid:
		return lipgloss.NewStyle().Foreground(hexColor(top)).Render("▀")
	}
	return lipgloss.NewStyle().Foreground(hexColor(bot)).Render("▄")
}

const ramp = " .:-=+*#%@"

func asciiCell(top, bot pixel) byte {
	solid := 0.0
	lum := 0.0
	for _, p := range []pixel{top, bot} {
		if p.solid {
			solid++
			lum += 0.2126*float64(p.r) + 0.7152*float64(p.g) + 0.0722*float64(p.b)
		}
	}
	if solid == 0 {
		return ' '
	}
	dark := 1 - lum/solid/255 // darker parts print denser, like ink on paper
	idx := int(dark*float64(len(ramp)-2)) + 1
	return ramp[min(max(idx, 1), len(ramp)-1)]
}
