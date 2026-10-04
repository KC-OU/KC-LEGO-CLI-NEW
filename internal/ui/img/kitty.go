package img

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"os"
	"strings"
)

// kittyChunkSize is the protocol's own per-escape-sequence limit on base64
// payload bytes; a larger image is split across several escape sequences,
// each tagged m=1 except the last (m=0).
const kittyChunkSize = 4096

// kittyImageID is fixed, not generated per call: every kittyEncode deletes
// this id's placement before retransmitting (a=d, below), so reusing one id
// is both simpler and race-free (no shared mutable counter) than minting a
// fresh one each time would be, and is exactly as correct — the delete is
// what actually clears a stale image, not a different id.
const kittyImageID = 9009

// kittySupported is whether this session can plausibly render Kitty
// graphics: a genuinely local terminal (never telnet — it never carries
// terminal graphics escape sequences meaningfully — and never the web/ttyd
// gateway either, since xterm.js, what that gateway actually renders into,
// does not implement this protocol at all), identifying itself as one of
// the few emulators that do.
func kittySupported() bool {
	if os.Getenv("WMS_GATEWAY_SESSION") == "1" {
		return false
	}
	if os.Getenv("KITTY_WINDOW_ID") != "" {
		return true
	}
	if os.Getenv("GHOSTTY_RESOURCES_DIR") != "" {
		return true
	}
	if os.Getenv("WEZTERM_EXECUTABLE") != "" {
		return true
	}
	term := strings.ToLower(os.Getenv("TERM_PROGRAM"))
	if strings.Contains(term, "wezterm") || strings.Contains(term, "ghostty") {
		return true
	}
	return os.Getenv("KONSOLE_VERSION") != ""
}

// kittyEncode renders src as a Kitty graphics protocol escape sequence sized
// to roughly cols x rows terminal cells — the terminal does its own scaling
// from that hint, so src goes across at full resolution (a real PNG), not
// pre-downsampled the way the ANSI block renderer's own sampling has to.
//
// Every call deletes image id kittyImageID first, then transmits-and-
// displays fresh (a=T) — the simplest correct way to live inside the TUI's
// own full-screen redraws: a screen that includes a picture re-sends it, in
// full, as part of that same redraw's output every time (bubbletea has no
// separate "only if changed" hook this could use instead), so there is
// never a stale placement left showing an old part's picture over a new
// one, or still visible after navigating away.
func kittyEncode(src image.Image, cols, rows int) (string, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		return "", err
	}
	encoded := base64.StdEncoding.EncodeToString(buf.Bytes())

	var out strings.Builder
	fmt.Fprintf(&out, "\x1b_Ga=d,d=i,i=%d\x1b\\", kittyImageID)

	first := true
	for len(encoded) > 0 {
		chunk := encoded
		if len(chunk) > kittyChunkSize {
			chunk = encoded[:kittyChunkSize]
		}
		encoded = encoded[len(chunk):]
		more := 0
		if len(encoded) > 0 {
			more = 1
		}
		if first {
			fmt.Fprintf(&out, "\x1b_Ga=T,f=100,t=d,i=%d,c=%d,r=%d,m=%d;%s\x1b\\", kittyImageID, cols, rows, more, chunk)
			first = false
		} else {
			// A continuation chunk carries only m — id/action/format/cols/
			// rows belong to the transmission the first chunk already opened.
			fmt.Fprintf(&out, "\x1b_Gm=%d;%s\x1b\\", more, chunk)
		}
	}
	return out.String(), nil
}
