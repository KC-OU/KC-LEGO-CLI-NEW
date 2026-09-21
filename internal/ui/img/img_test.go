package img

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func pngBytes(t *testing.T, w, h int, c color.Color) []byte {
	t.Helper()
	m := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			m.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, m); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func TestRenderBlocksUsesHalfBlocksWithTopAndBottomColours(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)
	m := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			if y%2 == 0 { // even pixel rows red, odd blue: each text row is red over blue
				m.Set(x, y, color.NRGBA{255, 0, 0, 255})
			} else {
				m.Set(x, y, color.NRGBA{0, 0, 255, 255})
			}
		}
	}
	out := Render(m, 4, 2, ModeBlocks)
	lines := strings.Split(out, "\n")
	if len(lines) != 2 || strings.Count(ansi.ReplaceAllString(lines[0], ""), "▀") != 4 {
		t.Fatalf("a 4x4 picture in a 4x2 box is 4 columns by 2 rows of half-blocks:\n%q", out)
	}
	if !strings.Contains(lines[0], "38;2;255;0;0") || !strings.Contains(lines[0], "48;2;0;0;255") {
		t.Errorf("row 0 has red above blue: %q", lines[0])
	}
}

func TestRenderKeepsTheAspectRatioAndTheBox(t *testing.T) {
	wide := image.NewNRGBA(image.Rect(0, 0, 200, 50))
	tall := image.NewNRGBA(image.Rect(0, 0, 50, 200))
	for _, m := range []*image.NRGBA{wide, tall} {
		for y := 0; y < m.Bounds().Dy(); y++ {
			for x := 0; x < m.Bounds().Dx(); x++ {
				m.Set(x, y, color.NRGBA{10, 200, 30, 255})
			}
		}
	}
	for name, m := range map[string]image.Image{"wide": wide, "tall": tall} {
		out := Render(m, 40, 12, ModeASCII)
		lines := strings.Split(out, "\n")
		if len(lines) > 12 {
			t.Errorf("%s: %d rows exceed the 12-row box", name, len(lines))
		}
		for _, l := range lines {
			if len([]rune(l)) > 40 {
				t.Errorf("%s: a row is %d columns wide", name, len([]rune(l)))
			}
		}
	}
	w := Render(wide, 40, 12, ModeASCII)
	if l := strings.Split(w, "\n"); len(l[0]) != 40 || len(l) != 5 { // 200x50 -> 40 columns, 50*40/200=10 pixel rows = 5 text rows
		t.Errorf("wide picture: %d cols x %d rows", len(l[0]), len(l))
	}
}

func TestASCIIModeIsPlainASCIIAndDarkerMeansDenser(t *testing.T) {
	dark := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	light := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			dark.Set(x, y, color.NRGBA{10, 10, 10, 255})
			light.Set(x, y, color.NRGBA{240, 240, 240, 255})
		}
	}
	d, l := Render(dark, 8, 4, ModeASCII), Render(light, 8, 4, ModeASCII)
	for _, s := range []string{d, l} {
		for _, r := range s {
			if r > 127 || r == 0x1b {
				t.Fatalf("ASCII mode must contain only plain ASCII, got %q", r)
			}
		}
	}
	if strings.IndexByte(ramp, d[0]) <= strings.IndexByte(ramp, l[0]) {
		t.Errorf("dark %q must be denser than light %q", d[0], l[0])
	}
}

func TestTransparentPixelsLeaveTheBackground(t *testing.T) {
	m := image.NewNRGBA(image.Rect(0, 0, 4, 4)) // all transparent
	if out := Render(m, 4, 2, ModeASCII); strings.TrimSpace(strings.ReplaceAll(out, "\n", "")) != "" {
		t.Errorf("a transparent picture draws nothing: %q", out)
	}
	if out := Render(m, 4, 2, ModeBlocks); strings.TrimSpace(ansi.ReplaceAllString(strings.ReplaceAll(out, "\n", ""), "")) != "" {
		t.Errorf("blocks: %q", out)
	}
	if Render(nil, 4, 2, ModeBlocks) != "" || Render(m, 0, 0, ModeBlocks) != "" || Render(m, 4, 2, ModeOff) != "" {
		t.Error("nothing to draw = empty string")
	}
}

func TestParseMode(t *testing.T) {
	cases := []struct {
		in   string
		mono bool
		want Mode
	}{{"", false, ModeBlocks}, {"auto", false, ModeBlocks}, {"auto", true, ModeASCII}, {"blocks", true, ModeASCII}, {"ascii", false, ModeASCII}, {"off", false, ModeOff}, {"sixel", false, ModeBlocks}, {"KITTY", false, ModeBlocks}}
	for _, c := range cases {
		if got := ParseMode(c.in, c.mono); got != c.want {
			t.Errorf("ParseMode(%q, mono=%v) = %s, want %s", c.in, c.mono, got, c.want)
		}
	}
}

func TestOnlyAllowListedHTTPSHosts(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://cdn.rebrickable.com/media/sets/75192-1.jpg": true,
		"//img.bricklink.com/ItemImage/PN/5/3001.png":        true,
		"http://cdn.rebrickable.com/x.png":                   false,
		"https://evil.example.com/x.png":                     false,
		"https://cdn.rebrickable.com.evil.com/x.png":         false,
		"https://user@cdn.rebrickable.com/x.png":             false,
		"https://cdn.rebrickable.com:8443/x.png":             false,
		"file:///etc/passwd":                                 false,
		"":                                                   false,
	} {
		if got := Allowed(raw); got != want {
			t.Errorf("Allowed(%q) = %v, want %v", raw, got, want)
		}
	}
	f := NewFetcher("")
	if _, err := f.Get(context.Background(), "https://evil.example.com/x.png"); !errors.Is(err, ErrNoPicture) {
		t.Errorf("a blocked host = %v", err)
	}
}

// fetcherFor points the allow-list at a local TLS test server for the duration of a test.

func TestFetchCachesDecodesAndBoundsWhatItAccepts(t *testing.T) {
	// The real allow-list forbids ports, so test the download path through a fetcher whose
	// client is a TLS test server and whose URL check we exercise separately above.
	good := pngBytes(t, 8, 8, color.NRGBA{200, 0, 0, 255})
	var hits atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch r.URL.Path {
		case "/good.png":
			w.Header().Set("Content-Type", "image/png")
			w.Write(good)
		case "/html.png":
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte("<html>"))
		case "/fake.png": // claims PNG, is not
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("not a png at all"))
		case "/huge.png":
			w.Header().Set("Content-Type", "image/png")
			w.Write(bytes.Repeat([]byte{0}, maxDownload+10))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	dir := t.TempDir()
	f := NewFetcher(dir)
	f.HTTP = srv.Client()
	get := func(path string) ([]byte, error) { return f.getURL(context.Background(), srv.URL+path) }

	if b, err := get("/good.png"); err != nil || !bytes.Equal(b, good) {
		t.Fatalf("good = %v", err)
	}
	if b, err := get("/good.png"); err != nil || len(b) == 0 || hits.Load() != 1 {
		t.Errorf("the second fetch is served from the cache (hits=%d, err=%v)", hits.Load(), err)
	}
	for _, p := range []string{"/html.png", "/fake.png", "/huge.png"} {
		if _, err := get(p); !errors.Is(err, ErrNoPicture) {
			t.Errorf("%s must be refused: %v", p, err)
		}
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*.img")); len(left) != 1 {
		t.Errorf("only the good picture is cached: %v", left)
	}
	before := hits.Load()
	get("/missing.png")
	get("/missing.png")
	get("/missing.png")
	if hits.Load() != before+1 {
		t.Errorf("a 404 is remembered so it is not asked again: %d extra hits", hits.Load()-before)
	}
	f.Offline = true
	if _, err := get("/never-fetched.png"); !errors.Is(err, ErrNoPicture) || hits.Load() != before+1 {
		t.Errorf("offline must not download: %v", err)
	}
	if b, err := get("/good.png"); err != nil || len(b) == 0 {
		t.Errorf("offline still serves the cache: %v", err)
	}
}

func TestDecodeRefusesPixelBombs(t *testing.T) {
	// A valid PNG header claiming 20000 x 20000 pixels.
	bomb := pngBytes(t, 1, 1, color.White)
	copy(bomb[16:20], []byte{0x00, 0x00, 0x4e, 0x20})
	copy(bomb[20:24], []byte{0x00, 0x00, 0x4e, 0x20})
	if _, err := Decode(bomb); !errors.Is(err, ErrNoPicture) {
		t.Errorf("a decompression bomb must be refused: %v", err)
	}
	var j bytes.Buffer
	jpeg.Encode(&j, image.NewRGBA(image.Rect(0, 0, 4, 4)), nil)
	if _, err := Decode(j.Bytes()); err != nil {
		t.Errorf("JPEG decodes: %v", err)
	}
	if _, err := Decode([]byte("GIF89a")); !errors.Is(err, ErrNoPicture) {
		t.Errorf("GIF is not supported: %v", err)
	}
}

func TestCachePrunesLeastRecentlyUsedFirst(t *testing.T) {
	dir := t.TempDir()
	f := NewFetcher(dir)
	f.MaxDir = 250
	write := func(name string, age time.Duration) {
		p := filepath.Join(dir, name+".img")
		os.WriteFile(p, bytes.Repeat([]byte{1}, 100), 0o600)
		ts := time.Now().Add(-age)
		os.Chtimes(p, ts, ts)
	}
	write("oldest", 3*time.Hour)
	write("middle", 2*time.Hour)
	write("newest", time.Hour)
	f.prune()
	left, _ := filepath.Glob(filepath.Join(dir, "*.img"))
	if len(left) != 2 {
		t.Fatalf("300 bytes over a 250 cap leaves two files: %v", left)
	}
	if _, err := os.Stat(filepath.Join(dir, "oldest.img")); err == nil {
		t.Error("the least recently used file goes first")
	}
}

func TestPartURLsTryTheRequestedColourThenCommonOnes(t *testing.T) {
	got := PartURLs("3023", 1, []int{4, 15, 72, 1, 0})
	if len(got) < 3 || !strings.HasSuffix(got[0], "/ldraw/1/3023.png") || !strings.HasSuffix(got[1], "/ldraw/15/3023.png") || !strings.HasSuffix(got[2], "/ldraw/0/3023.png") {
		t.Errorf("urls = %v", got)
	}
	if len(got) > 5 {
		t.Errorf("at most five attempts: %v", got)
	}
	if PartURLs("", 1, nil) != nil || PartURLs("a/b", 1, nil) != nil || PartURLs("a b", 1, nil) != nil {
		t.Error("an invalid part number gives no URLs")
	}
	if got := PartURLs("3001", -1, []int{4}); len(got) != 1 || !strings.Contains(got[0], "/ldraw/4/") {
		t.Errorf("no colour: %v", got)
	}
	for _, u := range PartURLs("970c00", 0, nil) {
		if !Allowed(u) {
			t.Errorf("generated URL is not allowed: %s", u)
		}
	}
}

func FuzzDecodeAndRender(f *testing.F) {
	f.Add(pngBytesForFuzz(3, 3))
	f.Add([]byte("\x89PNG\r\n\x1a\n"))
	f.Add([]byte("\xff\xd8\xff\xe0"))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		m, err := Decode(data)
		if err != nil {
			return
		}
		for _, mode := range []Mode{ModeBlocks, ModeASCII} {
			out := Render(m, 30, 10, mode)
			if mode == ModeASCII {
				for _, r := range out {
					if r > 127 || r == 0x1b {
						t.Fatalf("ASCII output has %q", r)
					}
				}
			}
			if n := strings.Count(out, "\n") + 1; out != "" && n > 10 {
				t.Fatalf("%d rows exceed the box", n)
			}
		}
	})
}

func pngBytesForFuzz(w, h int) []byte {
	m := image.NewNRGBA(image.Rect(0, 0, w, h))
	var b bytes.Buffer
	_ = png.Encode(&b, m)
	return b.Bytes()
}
