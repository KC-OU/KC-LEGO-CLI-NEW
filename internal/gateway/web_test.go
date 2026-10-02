package gateway

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
)

// panesOf polls tmux list-panes briefly before giving up — the session is
// already guaranteed to exist by the time this is called (see the blocking
// create in TestWebCommandRealTmuxCannotEscapeViaThePrefixKey), this is just
// a safety margin against a transient, loaded-machine hiccup talking to the
// server.
func panesOf(t *testing.T, sess string) string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var out []byte
	var err error
	for time.Now().Before(deadline) {
		out, err = exec.Command("tmux", "list-panes", "-t", sess, "-F", "#{window_index} #{pane_current_command}").CombinedOutput()
		if err == nil {
			return strings.TrimSpace(string(out))
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("tmux list-panes: %v (%s)", err, out)
	return ""
}

// ptyStart runs argv[0] with the rest as args, attached to a real pseudo-tty
// — the same way ttyd attaches a browser's websocket to whatever it spawns
// (see telnet.go's pty.Start for the telnet side of the same pattern) — so a
// test can type real keystrokes into it and have tmux's own key-table logic
// see them exactly as it would from a real terminal.
func ptyStart(argv []string) (*os.File, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	return pty.Start(cmd)
}

// withoutTmux points $PATH at a directory holding nothing (or every real tool
// except tmux), so exec.LookPath("tmux") fails, without touching the real PATH.
func withoutTmux(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	for _, real := range filepath.SplitList(os.Getenv("PATH")) {
		entries, err := os.ReadDir(real)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.Name() == "tmux" {
				continue
			}
			_ = os.Symlink(filepath.Join(real, e.Name()), filepath.Join(dir, e.Name()))
		}
	}
	t.Setenv("PATH", dir)
}

func withTmux(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("tmux fixture assumes a Linux-style PATH")
	}
	dir := t.TempDir()
	stub := filepath.Join(dir, "tmux")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestWebCommandFallsBackWithoutTmux(t *testing.T) {
	withoutTmux(t)
	got := webCommand("/usr/local/bin/wms-go")
	want := []string{"/usr/local/bin/wms-go", "tui"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("webCommand without tmux = %v, want %v", got, want)
	}
}

func TestWebCommandWrapsInAPersistentSessionWithTmux(t *testing.T) {
	withTmux(t)
	got := webCommand("/usr/local/bin/wms-go")
	want := []string{
		"tmux", "new-session", "-A", "-s", webSessionName, "/usr/local/bin/wms-go", "tui",
		";", "set-option", "-t", webSessionName, "prefix", "None",
		";", "set-option", "-t", webSessionName, "mouse", "off",
	}
	if len(got) != len(want) {
		t.Fatalf("webCommand with tmux = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("arg %d: got %q, want %q (%v)", i, got[i], want[i], got)
		}
	}
}

// TestWebCommandRealTmuxCannotEscapeViaThePrefixKey is the end-to-end
// regression guard for a real escape-to-root-shell bug: tmux's own prefix
// key (Ctrl-B by default), typed from inside the browser's shared
// web-terminal session, reached tmux itself before wms tui ever saw it —
// "c" opened a brand-new window running a root shell (whatever this process
// itself runs as), with none of the wrapped app's own permission checks
// ever consulted. This drives webCommand's actual argv through a real tmux,
// attached via a pty exactly as ttyd attaches a browser, and types the real
// bytes Ctrl-B then 'c' sends — the fix must mean no second window appears.
func TestWebCommandRealTmuxCannotEscapeViaThePrefixKey(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	if runtime.GOOS != "linux" {
		t.Skip("pty.Open assumes Linux")
	}
	sess := fmt.Sprintf("wms_web_test_%d", os.Getpid())
	t.Cleanup(func() { _ = exec.Command("tmux", "kill-session", "-t", sess).Run() })

	argv := webCommand("sleep")
	// webCommand always targets webSessionName; retarget this one invocation
	// at a throwaway session name so this test can't collide with (or kill)
	// a real wms-web session if one happens to be running on this box.
	for i, a := range argv {
		if a == webSessionName {
			argv[i] = sess
		}
	}
	// webCommand's trailing shell-command is "sleep tui" (from "sleep" above,
	// which isn't a valid sleep argument) — swap in a real sleep duration so
	// the pane has something to run and stay alive, and create it detached
	// (-d, not -A) with a plain blocking Run(): under the full test suite's
	// own heavy parallel load, starting tmux's server can be slow, and
	// polling for a session that a pty-attached client is still in the
	// middle of creating was flaky — a blocking create has no such race,
	// whatever it takes, it's done (or clearly failed) by the time Run()
	// returns.
	for i, a := range argv {
		if a == "-A" {
			argv[i] = "-d"
		}
		if a == "tui" {
			argv[i] = "600"
		}
	}
	if out, err := exec.Command(argv[0], argv[1:]...).CombinedOutput(); err != nil {
		t.Fatalf("creating the locked-down session: %v (%s)", err, out)
	}

	// Now attach a second, pty-backed client — exactly as ttyd attaches a
	// browser tab to the already-running session — and type the real bytes
	// Ctrl-B then 'c' sends. The pty is a buffer: these bytes sit there
	// until the attaching client reads them, so no race with however long
	// the attach itself takes to get scheduled.
	ptmx, err := ptyStart([]string{"tmux", "attach-session", "-t", sess})
	if err != nil {
		t.Fatalf("attaching under a pty: %v", err)
	}
	defer ptmx.Close()
	if _, err := ptmx.Write([]byte{0x02, 'c'}); err != nil { // Ctrl-B, c
		t.Fatalf("writing the keystrokes: %v", err)
	}
	// This is an absence check (no new window), so there's no earlier event
	// to poll for — give tmux a moment to have acted on the keystrokes one
	// way or the other before looking.
	time.Sleep(600 * time.Millisecond)

	lines := panesOf(t, sess)
	if strings.Count(lines, "\n") > 0 || strings.Count(lines, " ") > 1 {
		t.Errorf("Ctrl-B c must not reach tmux's own key table (no second window/pane), got:\n%s", lines)
	}
	if strings.Contains(lines, "bash") || strings.Contains(lines, "sh") {
		t.Errorf("Ctrl-B c must never land in a shell, got:\n%s", lines)
	}
}

// TestSoloCommandNeverWrapsInTmux guards the whole point of /solo: it must stay
// a fresh, unshared process every time, with or without tmux on the box — unlike
// webCommand, which wraps in the shared session whenever tmux is available.
func TestSoloCommandNeverWrapsInTmux(t *testing.T) {
	want := []string{"/usr/local/bin/wms-go", "tui"}
	assertEqual := func(t *testing.T, got []string) {
		t.Helper()
		if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
			t.Errorf("soloCommand = %v, want %v", got, want)
		}
	}
	t.Run("without tmux", func(t *testing.T) {
		withoutTmux(t)
		assertEqual(t, soloCommand("/usr/local/bin/wms-go"))
	})
	t.Run("with tmux", func(t *testing.T) {
		withTmux(t)
		assertEqual(t, soloCommand("/usr/local/bin/wms-go"))
	})
}
