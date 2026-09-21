package termguard

import (
	"os"
	"testing"
)

func TestRestorePutsTheRealTerminalTypeBack(t *testing.T) {
	t.Setenv("TERM", "dumb")
	t.Setenv(saved, "xterm-256color")
	Restore()
	if os.Getenv("TERM") != "xterm-256color" {
		t.Errorf("TERM = %q", os.Getenv("TERM"))
	}
	if _, ok := os.LookupEnv(saved); ok {
		t.Error("the marker must be removed")
	}
	Restore() // a second call changes nothing
	if os.Getenv("TERM") != "xterm-256color" {
		t.Error("idempotent")
	}
}
