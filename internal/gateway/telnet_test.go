package gateway

import (
	"strings"
	"testing"
)

func filter(t *testing.T, chunks ...[]byte) []byte {
	t.Helper()
	st := &IACState{}
	var out []byte
	for _, c := range chunks {
		out = append(out, FilterIAC(c, st)...)
	}
	return out
}

func TestFilterIAC_PlainBytesPassThrough(t *testing.T) {
	got := filter(t, []byte("hello\r\n"))
	if string(got) != "hello\r\n" {
		t.Fatalf("got %q", got)
	}
}

func TestFilterIAC_StripsDoWillTriplet(t *testing.T) {
	data := []byte{'a', iacByte, doByte, 31, 'b'}
	got := filter(t, data)
	if string(got) != "ab" {
		t.Fatalf("got %q", got)
	}
}

func TestFilterIAC_StripsSubnegotiationPayload(t *testing.T) {
	// IAC SB NAWS <4 bytes of width/height> IAC SE, surrounded by real data.
	data := []byte{'x', iacByte, sbByte, 31, 0, 80, 0, 24, iacByte, seByte, 'y'}
	got := filter(t, data)
	if string(got) != "xy" {
		t.Fatalf("got %q", got)
	}
}

func TestFilterIAC_UnescapesIACIAC(t *testing.T) {
	data := []byte{'a', iacByte, iacByte, 'b'}
	got := filter(t, data)
	want := []byte{'a', 0xFF, 'b'}
	if string(got) != string(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestFilterIAC_SequenceSplitAcrossReads(t *testing.T) {
	// IAC DO NAWS split so the option byte arrives in a second chunk.
	got := filter(t, []byte{'a', iacByte, doByte}, []byte{31, 'b'})
	if string(got) != "ab" {
		t.Fatalf("got %q", got)
	}
}

func TestFilterIAC_SubnegotiationSplitAcrossReads(t *testing.T) {
	got := filter(t,
		[]byte{'x', iacByte, sbByte, 31, 0},
		[]byte{80, 0, 24, iacByte},
		[]byte{seByte, 'y'},
	)
	if string(got) != "xy" {
		t.Fatalf("got %q", got)
	}
}

// TestChildEnvSkipsTerminalColorProbe is the regression guard for the
// startup-hang fix: TERM must start with "screen"/"tmux"/"dumb" so
// termenv's OSC background-color query (and its 5s termenv.OSCTimeout) is
// skipped for raw telnet clients that never answer it, and COLORTERM must
// still force full color so the 5250 theme doesn't silently go monochrome
// as a side effect. See childEnv's doc comment in telnet.go.
func TestChildEnvSkipsTerminalColorProbe(t *testing.T) {
	env := childEnv()

	// os/exec resolves a duplicate-key env slice by keeping the LAST
	// occurrence (childEnv appends TERM/COLORTERM after os.Environ(), which
	// may already contain them) — scan backwards to match that, not the
	// first (inherited) occurrence.
	get := func(key string) (string, bool) {
		prefix := key + "="
		for i := len(env) - 1; i >= 0; i-- {
			if strings.HasPrefix(env[i], prefix) {
				return strings.TrimPrefix(env[i], prefix), true
			}
		}
		return "", false
	}

	term, ok := get("TERM")
	if !ok {
		t.Fatal("expected TERM to be set")
	}
	if term != "screen" && term != "tmux" && term != "dumb" &&
		!strings.HasPrefix(term, "screen") && !strings.HasPrefix(term, "tmux") && !strings.HasPrefix(term, "dumb") {
		t.Errorf("TERM=%q does not start with screen/tmux/dumb — termenv will send its OSC background-color query and block for up to 5s on a telnet client that never answers it", term)
	}

	colorTerm, ok := get("COLORTERM")
	if !ok || colorTerm == "" {
		t.Error("expected COLORTERM to be set to force color despite TERM not matching a known-color terminal name")
	}
}

func TestFilterIAC_ParsesNAWSWindowSize(t *testing.T) {
	st := &IACState{}
	// IAC SB NAWS 0 160 0 45 IAC SE — a 160x45 window.
	out := FilterIAC([]byte{'x', iacByte, sbByte, 31, 0, 160, 0, 45, iacByte, seByte, 'y'}, st)
	if string(out) != "xy" {
		t.Fatalf("payload must still be stripped from the data stream, got %q", out)
	}
	cols, rows, ok := st.TakeResize()
	if !ok || cols != 160 || rows != 45 {
		t.Fatalf("TakeResize = %d x %d (ok=%v), want 160 x 45", cols, rows, ok)
	}
	if _, _, ok := st.TakeResize(); ok {
		t.Fatal("TakeResize must report a given size only once")
	}
}

func TestFilterIAC_NAWSSplitAcrossReadsAndResizedAgain(t *testing.T) {
	st := &IACState{}
	FilterIAC([]byte{iacByte, sbByte, 31, 0}, st)
	FilterIAC([]byte{200, 0, 50, iacByte}, st)
	FilterIAC([]byte{seByte}, st)
	if cols, rows, ok := st.TakeResize(); !ok || cols != 200 || rows != 50 {
		t.Fatalf("split NAWS: got %d x %d ok=%v, want 200 x 50", cols, rows, ok)
	}
	// The user resizes the window: a second report is picked up too.
	FilterIAC([]byte{iacByte, sbByte, 31, 0, 100, 0, 30, iacByte, seByte}, st)
	if cols, rows, ok := st.TakeResize(); !ok || cols != 100 || rows != 30 {
		t.Fatalf("resize: got %d x %d ok=%v, want 100 x 30", cols, rows, ok)
	}
}

// A width or height of 255 is sent as IAC IAC inside the subnegotiation.
func TestFilterIAC_NAWSEscapedFF(t *testing.T) {
	st := &IACState{}
	FilterIAC([]byte{iacByte, sbByte, 31, 0, iacByte, iacByte, 0, 40, iacByte, seByte}, st)
	if cols, rows, ok := st.TakeResize(); !ok || cols != 255 || rows != 40 {
		t.Fatalf("escaped 0xFF width: got %d x %d ok=%v, want 255 x 40", cols, rows, ok)
	}
}

func TestFilterIAC_IgnoresUnusableNAWS(t *testing.T) {
	cases := map[string][]byte{
		"zero width":    {iacByte, sbByte, 31, 0, 0, 0, 24, iacByte, seByte},
		"zero height":   {iacByte, sbByte, 31, 0, 80, 0, 0, iacByte, seByte},
		"too short":     {iacByte, sbByte, 31, 0, 80, iacByte, seByte},
		"other option":  {iacByte, sbByte, 24, 0, 80, 0, 24, iacByte, seByte},
		"oversize blob": append(append([]byte{iacByte, sbByte, 31}, make([]byte, 5000)...), iacByte, seByte),
		"no terminator": {iacByte, sbByte, 31, 0, 80, 0, 24},
	}
	for name, data := range cases {
		st := &IACState{}
		FilterIAC(data, st)
		if _, _, ok := st.TakeResize(); ok {
			t.Errorf("%s: must not produce a resize", name)
		}
		if len(st.sb) > maxSubnegPayload {
			t.Errorf("%s: subnegotiation buffer grew to %d bytes, cap is %d", name, len(st.sb), maxSubnegPayload)
		}
	}
}

func TestClampDim(t *testing.T) {
	if clampDim(65535, maxCols) != maxCols || clampDim(120, maxCols) != 120 {
		t.Fatal("clampDim must cap absurd client-reported sizes and leave sane ones alone")
	}
}
