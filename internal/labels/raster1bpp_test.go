package labels

import "testing"

func TestRaster1bppCentersANarrowLabelWithinTheHeadWidth(t *testing.T) {
	s, _ := SizeByID("50x30")
	const printHeadBytes = 72 // the PM260's own 72mm/576px head
	data, widthBytes, heightLines, err := Raster1bpp(sample(), s, printHeadBytes)
	if err != nil {
		t.Fatal(err)
	}
	wantRowBytes := (mmToPx(50) + 7) / 8
	wantOffset := (printHeadBytes - wantRowBytes) / 2
	if widthBytes != printHeadBytes {
		t.Fatalf("widthBytes = %d, want %d", widthBytes, printHeadBytes)
	}
	if heightLines != mmToPx(30) {
		t.Fatalf("heightLines = %d, want %d", heightLines, mmToPx(30))
	}
	if len(data) != printHeadBytes*heightLines {
		t.Fatalf("data length = %d, want %d", len(data), printHeadBytes*heightLines)
	}
	// Every row's bytes before the centered offset must be blank (padding), and
	// at least one row must carry real content inside the centered label area.
	anySet := false
	for y := 0; y < heightLines; y++ {
		for x := 0; x < wantOffset; x++ {
			if data[y*printHeadBytes+x] != 0 {
				t.Fatalf("row %d byte %d (left pad) = %#x, want 0", y, x, data[y*printHeadBytes+x])
			}
		}
		for x := wantOffset; x < wantOffset+wantRowBytes; x++ {
			if data[y*printHeadBytes+x] != 0 {
				anySet = true
			}
		}
	}
	if !anySet {
		t.Fatal("the centered label area is entirely blank — nothing was actually drawn")
	}
}

func TestRaster1bppRejectsALabelWiderThanThePrintHead(t *testing.T) {
	s, _ := SizeByID("4x6") // 101.6mm wide, far wider than any small thermal head
	if _, _, _, err := Raster1bpp(sample(), s, 72); err == nil {
		t.Error("a label wider than the print head must error, not silently clip")
	}
}
