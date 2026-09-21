package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/creack/pty"
)

// bubbletea asks the terminal for its background colour when the program starts and waits for the answer;
// on a terminal that never answers every command stalled. The built program must not ask.
func TestTheProgramDoesNotProbeTheTerminalAtStartup(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the program")
	}
	bin := filepath.Join(t.TempDir(), "wms")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	cmd := exec.Command(bin, "version")
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor")
	ptmx, err := pty.Start(cmd)
	if err != nil {
		t.Skip("no pty here: " + err.Error())
	}
	defer ptmx.Close()
	var buf bytes.Buffer
	done := make(chan struct{})
	go func() { io.Copy(&buf, ptmx); close(done) }() // nobody answers a query, as on a terminal that ignores them
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		cmd.Process.Kill()
		t.Fatalf("`wms version` did not finish within 3 s: it is waiting for the terminal to answer a query\n%q", buf.String())
	}
	cmd.Wait()
	if bytes.Contains(buf.Bytes(), []byte("\x1b]11;?")) || bytes.Contains(buf.Bytes(), []byte("\x1b[6n")) {
		t.Errorf("the program queried the terminal: %q", buf.String())
	}
}
