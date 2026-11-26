package gateway

import (
	"bytes"
	"context"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type sink struct {
	net.Conn
	buf bytes.Buffer
}

func (s *sink) Write(p []byte) (int, error) { return s.buf.Write(p) }

func TestAsciiConnRewritesGlyphsAndKeepsEscapes(t *testing.T) {
	s := &sink{}
	c := &asciiConn{Conn: s}
	in := "\x1b[1;32m┌──┐\x1b[0m\r\n│ • Part-DB — online │\r\n└──┘"
	n, err := c.Write([]byte(in))
	if err != nil || n != len(in) {
		t.Fatalf("Write returned %d, %v; want %d, nil", n, err, len(in))
	}
	want := "\x1b[1;32m+--+\x1b[0m\r\n| * Part-DB - online |\r\n+--+"
	if got := s.buf.String(); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if strings.ContainsFunc(s.buf.String(), func(r rune) bool { return r > 127 }) {
		t.Fatal("non-ASCII byte leaked through")
	}
}

func TestAsciiConnHandlesRuneSplitAcrossWrites(t *testing.T) {
	s := &sink{}
	c := &asciiConn{Conn: s}
	b := []byte("a─b") // ─ is E2 94 80
	for _, part := range [][]byte{b[:2], b[2:3], b[3:]} {
		if _, err := c.Write(part); err != nil {
			t.Fatal(err)
		}
	}
	if got := s.buf.String(); got != "a-b" {
		t.Fatalf("got %q want %q", got, "a-b")
	}
}

// A real connection to a handheld listener: the child prints box glyphs and its window size;
// the client must get ASCII only and the 40x16 starting size.
func TestHandheldListenerSendsAsciiAndSmallWindow(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "fake")
	writeFakeScript(t, bin, "printf '┌─┐ • ok\\n'; stty size; sleep 1\n")

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	th := NewThrottle()
	th.KeepLoopback = true
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go acceptLoopMode(ctx, ln, bin, "", nil, th, newReconnectRegistry(0), true)

	c, out := dialAndRead(t, ln.Addr().String(), 300*time.Millisecond, 2*time.Second)
	defer c.Close()
	if !strings.Contains(out, "+-+ * ok") || !strings.Contains(out, "16 40") {
		t.Fatalf("want ASCII glyphs and \"16 40\", got %q", out)
	}
	for _, r := range out[len(negotiationPreamble):] {
		if r > 127 {
			t.Fatalf("non-ASCII output reached a handheld client: %q", out)
		}
	}
}
