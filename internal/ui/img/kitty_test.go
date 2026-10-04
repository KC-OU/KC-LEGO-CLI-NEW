package img

import (
	"image"
	"image/color"
	"math/rand"
	"strings"
	"testing"
)

func TestKittySupportedGatesOnGatewaySession(t *testing.T) {
	t.Setenv("WMS_GATEWAY_SESSION", "1")
	t.Setenv("KITTY_WINDOW_ID", "1")
	if kittySupported() {
		t.Fatal("a gateway session (telnet or the web/ttyd terminal) must never be treated as Kitty-capable")
	}
}

func TestKittySupportedDetectsLocalEmulators(t *testing.T) {
	t.Setenv("WMS_GATEWAY_SESSION", "")
	cases := []struct {
		name, key, val string
	}{
		{"kitty", "KITTY_WINDOW_ID", "1"},
		{"ghostty", "GHOSTTY_RESOURCES_DIR", "/usr/share/ghostty"},
		{"wezterm", "WEZTERM_EXECUTABLE", "/usr/bin/wezterm"},
		{"konsole", "KONSOLE_VERSION", "23.08"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("KITTY_WINDOW_ID", "")
			t.Setenv("GHOSTTY_RESOURCES_DIR", "")
			t.Setenv("WEZTERM_EXECUTABLE", "")
			t.Setenv("KONSOLE_VERSION", "")
			t.Setenv("TERM_PROGRAM", "")
			t.Setenv(c.key, c.val)
			if !kittySupported() {
				t.Fatalf("expected %s to be detected as Kitty-capable", c.name)
			}
		})
	}
}

func TestKittySupportedFalseForPlainTerminal(t *testing.T) {
	t.Setenv("WMS_GATEWAY_SESSION", "")
	t.Setenv("KITTY_WINDOW_ID", "")
	t.Setenv("GHOSTTY_RESOURCES_DIR", "")
	t.Setenv("WEZTERM_EXECUTABLE", "")
	t.Setenv("KONSOLE_VERSION", "")
	t.Setenv("TERM_PROGRAM", "Apple_Terminal")
	if kittySupported() {
		t.Fatal("a plain terminal with no Kitty-protocol signal must not be treated as supported")
	}
}

func solidImage(w, h int, c color.Color) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	return img
}

func TestKittyEncodeSingleChunk(t *testing.T) {
	out, err := kittyEncode(solidImage(4, 4, color.RGBA{255, 0, 0, 255}), 10, 5)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "\x1b_Ga=d,d=i,i=9009\x1b\\") {
		t.Fatalf("expected a delete-first prefix for the fixed image id, got %q", out[:min(60, len(out))])
	}
	if !strings.Contains(out, "a=T,f=100,t=d,i=9009,c=10,r=5,m=0;") {
		t.Fatalf("expected a single-chunk transmit-and-display control block, got %q", out)
	}
	if strings.Count(out, "\x1b_G") != 2 { // the delete escape + one transmit escape
		t.Fatalf("expected exactly 2 escape sequences for a small image, got %d in %q", strings.Count(out, "\x1b_G"), out)
	}
}

func TestKittyEncodeChunksLargePayload(t *testing.T) {
	// Large and random enough (defeats PNG's compression) that the base64
	// payload must exceed kittyChunkSize and span multiple escape sequences.
	rng := rand.New(rand.NewSource(1))
	img := image.NewRGBA(image.Rect(0, 0, 300, 300))
	for y := 0; y < 300; y++ {
		for x := 0; x < 300; x++ {
			img.Set(x, y, color.RGBA{uint8(rng.Intn(256)), uint8(rng.Intn(256)), uint8(rng.Intn(256)), 255})
		}
	}
	out, err := kittyEncode(img, 40, 20)
	if err != nil {
		t.Fatal(err)
	}
	chunks := strings.Count(out, "\x1b_G")
	if chunks < 3 { // delete + at least 2 transmit chunks
		t.Fatalf("expected a multi-chunk transmission for a large image, got %d escape sequences", chunks)
	}
	if !strings.Contains(out, ",m=1;") {
		t.Fatalf("expected a non-final chunk marked m=1, got %q", out)
	}
	if !strings.HasSuffix(out, "\x1b\\") {
		t.Fatalf("expected the final chunk to end with the ST terminator, got suffix %q", out[len(out)-10:])
	}
	// Exactly one chunk should be the final one (m=0) within the transmit set.
	if strings.Count(out, "m=0;") != 1 {
		t.Fatalf("expected exactly one final chunk (m=0), got %d", strings.Count(out, "m=0;"))
	}
}
