package labels

import (
	"testing"

	"github.com/boombuler/barcode/code128"
)

// TestBarcodeOpsNeverGoesBelowTheMinimumScannableModuleWidth is the direct
// regression test for the bug behind "the barcode didn't work": a long
// value (a set number like "75192-1") on small stock (40x30mm) used to be
// squeezed to whatever width*0.9 allowed, with no floor — well under what a
// 203 dpi thermal head can resolve. Now it's allowed to run wider than its
// box instead of thinner than the floor.
func TestBarcodeOpsNeverGoesBelowTheMinimumScannableModuleWidth(t *testing.T) {
	// 40x30 stock, pad ~1.5mm -> about 37mm of nominal box width, same
	// shape draw() actually passes for a small/sheet label.
	const text = "75192-1"
	ops := barcodeOps(text, 0, 0, 37, 6)
	if len(ops) == 0 {
		t.Fatal("barcodeOps returned no bars")
	}

	code, err := code128.Encode(text)
	if err != nil {
		t.Fatal(err)
	}
	mods := float64(code.Bounds().Dx())

	minX, maxX := ops[0].x, ops[0].x+ops[0].w
	for _, o := range ops {
		minX = min(minX, o.x)
		maxX = max(maxX, o.x+o.w)
	}
	totalWidth := maxX - minX
	gotModuleWidth := totalWidth / mods

	if gotModuleWidth < minModuleWidth-0.001 { // float slack
		t.Fatalf("effective module width = %.3fmm, want at least the %.3fmm scannable floor (box was too narrow at 37mm for %d modules)",
			gotModuleWidth, minModuleWidth, int(mods))
	}
}

// TestBarcodeOpsStaysWithinTheOldCapWhenTheBoxIsRoomyEnough confirms the
// floor only kicks in when actually needed — a short value in a generous
// box still gets the original width*0.9-vs-mods*0.5 behavior, not blown
// out to 1.4x for no reason.
func TestBarcodeOpsStaysWithinTheOldCapWhenTheBoxIsRoomyEnough(t *testing.T) {
	const text = "3001"
	const w = 60.0
	ops := barcodeOps(text, 0, 0, w, 6)
	if len(ops) == 0 {
		t.Fatal("barcodeOps returned no bars")
	}
	minX, maxX := ops[0].x, ops[0].x+ops[0].w
	for _, o := range ops {
		minX = min(minX, o.x)
		maxX = max(maxX, o.x+o.w)
	}
	if totalWidth := maxX - minX; totalWidth > w*0.95 {
		t.Errorf("total width = %.2fmm, want it to stay near the old width*0.9 cap (%.2fmm) when there's plenty of room", totalWidth, w*0.9)
	}
}
