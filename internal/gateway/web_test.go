package gateway

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

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
	want := []string{"tmux", "new-session", "-A", "-s", webSessionName, "/usr/local/bin/wms-go", "tui"}
	if len(got) != len(want) {
		t.Fatalf("webCommand with tmux = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("arg %d: got %q, want %q (%v)", i, got[i], want[i], got)
		}
	}
}
