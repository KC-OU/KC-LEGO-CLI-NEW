package labels

import (
	"bytes"
	"fmt"
	stdpng "image/png"
)

// Raster1bpp renders d onto s as a 1-bit-per-pixel bitmap at pngDPI, MSB-first,
// row-major, each row padded and centered to printWidthBytes -- the packed raster
// format Phomemo/Labelnize M-series thermal printers (the PM260 and similar)
// expect as their print data, sent after a GS v 0 raster header. Reuses PNG's own
// rendering (decoding its own output back into pixels) so the two can never drift
// apart -- same pixels, just packed differently -- instead of a second renderer
// that would quietly diverge from it over time.
//
// printWidthBytes is the printer's own full head width (72 for the PM260, i.e.
// 72mm/576px at 203dpi); a narrower label (down to 30mm) is centered within it,
// matching how the printer's own app lays out anything narrower than the head.
func Raster1bpp(d Data, s Size, printWidthBytes int) (data []byte, widthBytes, heightLines int, err error) {
	body, err := PNG([]Data{d}, s)
	if err != nil {
		return nil, 0, 0, err
	}
	img, err := stdpng.Decode(bytes.NewReader(body))
	if err != nil {
		return nil, 0, 0, err
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	rowBytes := (w + 7) / 8
	if printWidthBytes < rowBytes {
		return nil, 0, 0, fmt.Errorf("this label is %d bytes wide, wider than the printer's %d-byte head", rowBytes, printWidthBytes)
	}
	offset := (printWidthBytes - rowBytes) / 2
	out := make([]byte, printWidthBytes*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, bl, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			bright := 0.299*float64(r>>8) + 0.587*float64(g>>8) + 0.114*float64(bl>>8)
			if bright < 128 {
				out[y*printWidthBytes+offset+x/8] |= 1 << (7 - uint(x%8))
			}
		}
	}
	return out, printWidthBytes, h, nil
}
