package labels

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestPNGDimensionsMatchTheSizeAt203DPI(t *testing.T) {
	s, _ := SizeByID("38x90")
	b, err := PNG([]Data{sample()}, s)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	wantW, wantH := mmToPx(38), mmToPx(90)
	bounds := img.Bounds()
	if bounds.Dx() != wantW || bounds.Dy() != wantH {
		t.Errorf("PNG is %dx%d px, want %dx%d (38x90mm at %v dpi)", bounds.Dx(), bounds.Dy(), wantW, wantH, pngDPI)
	}
}

// TestPNGActuallyDrawsSomething guards against a silently blank image — a
// real regression this renderer could have (e.g. every op's colour computed
// wrong): most of the canvas should still be white (it's a label, not a
// black square), but some of it must not be.
func TestPNGActuallyDrawsSomething(t *testing.T) {
	s, _ := SizeByID("62x100")
	b, err := PNG([]Data{sample()}, s)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	bounds := img.Bounds()
	dark, total := 0, 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			total++
			r, g, b, _ := img.At(x, y).RGBA()
			if r < 0x8000 && g < 0x8000 && b < 0x8000 {
				dark++
			}
		}
	}
	if dark == 0 {
		t.Fatal("PNG is entirely blank")
	}
	if frac := float64(dark) / float64(total); frac < 0.01 || frac > 0.6 {
		t.Errorf("dark pixel fraction = %.3f, want roughly between 1%% and 60%% for a real label", frac)
	}
}

func TestPNGRejectsMoreThanOneLabel(t *testing.T) {
	s, _ := SizeByID("62x29")
	if _, err := PNG([]Data{sample(), sample()}, s); err == nil {
		t.Error("PNG with more than one item should be refused, pointing at pdf/html")
	}
}

func TestPNGRejectsSheetStock(t *testing.T) {
	s, _ := SizeByID("a4")
	if _, err := PNG([]Data{sample()}, s); err == nil {
		t.Error("PNG on sheet stock should be refused, pointing at pdf/html")
	}
}

func TestDrawTextSkipsEmptyOrZeroSize(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	drawText(img, op{text: "", size: 5})
	drawText(img, op{text: "x", size: 0})
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			if img.At(x, y) != (color.RGBA{}) {
				t.Fatal("drawText with no text or zero size must not touch the canvas")
			}
		}
	}
}
