package gateway

import (
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPromptLiveOrTestReadsTheAnswer(t *testing.T) {
	cases := map[string]string{"2\r\n": "2", "1\r\n": "1", "\r\n": ""}
	for input, want := range cases {
		server, client := net.Pipe()
		done := make(chan struct{})
		go func() {
			defer close(done)
			buf := make([]byte, 256)
			client.Read(buf)            // the prompt
			client.Write([]byte(input)) // the answer
			client.Read(buf)            // the trailing "\r\n"
		}()
		got := promptLiveOrTest(server)
		server.Close()
		<-done
		client.Close()
		if got != want {
			t.Errorf("input %q: promptLiveOrTest = %q, want %q", input, got, want)
		}
	}
}

func TestPromptLiveOrTestDefaultsToLiveOnEOF(t *testing.T) {
	server, client := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 256)
		client.Read(buf) // the prompt
		client.Close()   // hang up without answering
	}()
	if got := promptLiveOrTest(server); got != "" {
		t.Errorf("a dropped connection should default to Live (empty answer), got %q", got)
	}
	server.Close()
	<-done
}

func writeFakeScript(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0755); err != nil {
		t.Fatal(err)
	}
}

// TestHandleTelnetSessionPicksTheRightBinary is the end-to-end guard for the
// Live/Test picker: a real TCP connection into acceptLoop, backed by two fake
// "binaries" that each just announce who they are and exit, so the test can
// tell from the connection's own output which one actually ran.
func TestHandleTelnetSessionPicksTheRightBinary(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, "live")
	testBin := filepath.Join(dir, "test")
	writeFakeScript(t, live, "echo LIVE-BINARY\nexit 0\n")
	writeFakeScript(t, testBin, "echo TEST-BINARY\nexit 0\n")

	connect := func(t *testing.T, testBinaryPath string, send func(c net.Conn)) string {
		t.Helper()
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer ln.Close()
		th := NewThrottle()
		th.KeepLoopback = true
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go acceptLoop(ctx, ln, live, testBinaryPath, nil, th, newReconnectRegistry(0))

		c, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		send(c)
		_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
		out, _ := io.ReadAll(c)
		return string(out)
	}

	answer := func(s string) func(net.Conn) {
		return func(c net.Conn) { _, _ = c.Write([]byte(s)) }
	}

	// "Default to Live when nothing legible came back" (a timeout or a
	// dropped connection) is covered precisely and deterministically by
	// TestPromptLiveOrTestDefaultsToLiveOnEOF above — reproducing it here
	// would race the session's own teardown-on-EOF logic, which isn't what
	// this test is for.
	if out := connect(t, testBin, answer("2\r\n")); !strings.Contains(out, "TEST-BINARY") {
		t.Errorf("choosing 2 should run the test binary, got %q", out)
	}
	if out := connect(t, testBin, answer("1\r\n")); !strings.Contains(out, "LIVE-BINARY") {
		t.Errorf("choosing 1 should run the live binary, got %q", out)
	}
	if out := connect(t, "", answer("2\r\n")); strings.Contains(out, "TEST-BINARY") || strings.Contains(out, "1) Live") {
		t.Errorf("with no test binary configured, there must be no prompt and the test binary must never run: %q", out)
	}
}
