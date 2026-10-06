package gateway

import (
	"context"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// dialAndRead opens a new connection to ln and returns whatever arrives
// within the deadline — enough to see the fake binary's banner line plus
// anything it echoes back.
func dialAndRead(t *testing.T, addr string, send, deadline time.Duration) (net.Conn, string) {
	t.Helper()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	_ = c.SetReadDeadline(time.Now().Add(deadline))
	buf := make([]byte, 4096)
	n, _ := c.Read(buf) // whatever's arrived so far (preamble + banner)
	time.Sleep(send)    // let any immediately-following output land too
	n2, _ := c.Read(buf[n:])
	return c, string(buf[:n+n2])
}

// TestTelnetReconnectResumesTheSameChildProcess is the end-to-end guard for
// the reconnect grace window: a client's first connection gets a session
// whose fake "binary" prints its own PID once and then echoes stdin forever
// (standing in for a long-running TUI). Dropping and reconnecting from the
// same address within the grace window must land back on that same PID with
// no fresh banner — proof it's the same process, not a new login.
func TestTelnetReconnectResumesTheSameChildProcess(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "echoer")
	writeFakeScript(t, bin, "echo \"PID:$$\"\nexec cat\n")

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	th := NewThrottle()
	th.KeepLoopback = true
	rc := newReconnectRegistry(2 * time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go acceptLoop(ctx, ln, bin, "", nil, th, rc)

	c1, out1 := dialAndRead(t, ln.Addr().String(), 100*time.Millisecond, time.Second)
	if !strings.Contains(out1, "PID:") {
		t.Fatalf("expected a PID banner on first connect, got %q", out1)
	}
	pid1 := out1[strings.Index(out1, "PID:")+len("PID:"):]
	pid1 = strings.TrimSpace(strings.SplitN(pid1, "\n", 2)[0])
	if _, err := strconv.Atoi(pid1); err != nil {
		t.Fatalf("couldn't parse a PID out of %q: %v", out1, err)
	}

	_ = c1.Close() // simulate a dropped Wi-Fi link

	c2, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	if _, err := c2.Write([]byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	_ = c2.SetReadDeadline(time.Now().Add(5 * time.Second)) // generous: full-suite parallel runs can starve this PTY round trip
	buf := make([]byte, 4096)
	var out2 string
	for !strings.Contains(out2, "hello") {
		n, err := c2.Read(buf)
		out2 += string(buf[:n])
		if err != nil {
			break
		}
	}

	if strings.Contains(out2, "PID:") {
		t.Errorf("reconnect within the grace window should resume the same process (no new banner), got %q", out2)
	}
	if !strings.Contains(out2, "hello") {
		t.Errorf("expected the resumed session to echo what we sent, got %q", out2)
	}
}

// TestTelnetReconnectExpiresAfterGrace confirms a reconnect attempt after the
// grace window gets a brand new session (fresh PID banner) instead of
// hanging onto a parked child forever.
func TestTelnetReconnectExpiresAfterGrace(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "echoer")
	writeFakeScript(t, bin, "echo \"PID:$$\"\nexec cat\n")

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	th := NewThrottle()
	th.KeepLoopback = true
	rc := newReconnectRegistry(150 * time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go acceptLoop(ctx, ln, bin, "", nil, th, rc)

	c1, out1 := dialAndRead(t, ln.Addr().String(), 50*time.Millisecond, time.Second)
	if !strings.Contains(out1, "PID:") {
		t.Fatalf("expected a PID banner on first connect, got %q", out1)
	}
	_ = c1.Close()

	time.Sleep(400 * time.Millisecond) // well past the 150ms grace window

	c2, out2 := dialAndRead(t, ln.Addr().String(), 50*time.Millisecond, time.Second)
	defer c2.Close()
	if !strings.Contains(out2, "PID:") {
		t.Errorf("reconnecting after the grace window should start a fresh session (a new banner), got %q", out2)
	}
}

// TestTelnetReconnectOffByDefaultKillsImmediately confirms the grace<=0 case
// (the out-of-the-box default) behaves exactly like before this feature
// existed: a dropped connection's child is gone, not parked.
func TestTelnetReconnectOffByDefaultKillsImmediately(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "echoer")
	writeFakeScript(t, bin, "echo \"PID:$$\"\nexec cat\n")

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	th := NewThrottle()
	th.KeepLoopback = true
	rc := newReconnectRegistry(0) // off
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go acceptLoop(ctx, ln, bin, "", nil, th, rc)

	c1, out1 := dialAndRead(t, ln.Addr().String(), 50*time.Millisecond, time.Second)
	if !strings.Contains(out1, "PID:") {
		t.Fatalf("expected a PID banner on first connect, got %q", out1)
	}
	_ = c1.Close()
	time.Sleep(50 * time.Millisecond) // let the server-side teardown run

	c2, out2 := dialAndRead(t, ln.Addr().String(), 50*time.Millisecond, time.Second)
	defer c2.Close()
	if !strings.Contains(out2, "PID:") {
		t.Errorf("with reconnect off, every connection must be a fresh session, got %q", out2)
	}
	if len(rc.parked) != 0 {
		t.Errorf("reconnect off must never park anything, got %d parked", len(rc.parked))
	}
}
