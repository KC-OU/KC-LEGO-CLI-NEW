package main

import (
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// findSub returns cmd's direct child named name (cobra.Command.Name() is
// the first word of Use), or nil.
func findSub(cmd *cobra.Command, name string) *cobra.Command {
	for _, c := range cmd.Commands() {
		if c.Name() == name {
			return c
		}
	}
	return nil
}

// TestWatchFlagIsWiredWhereExpected guards every command this app wires
// --watch into -- "anything glanced at repeatedly through a shift" -- so a
// future refactor can't silently drop one without a test failing.
func TestWatchFlagIsWiredWhereExpected(t *testing.T) {
	cases := []struct {
		name string
		cmd  *cobra.Command
	}{
		{"attendance rota show", findSub(newAttendanceRotaCmd(), "show")},
		{"lego low", newLegoLowCmd()},
		{"lego missing", newLegoMissingCmd()},
		{"lego orders list", findSub(newLegoOrdersCmd(), "list")},
		{"sys status", findSub(newSysCmd(), "status")},
	}
	for _, c := range cases {
		if c.cmd == nil {
			t.Errorf("%s: command not found", c.name)
			continue
		}
		if c.cmd.Flags().Lookup("watch") == nil {
			t.Errorf("%s: --watch flag missing", c.name)
		}
	}
}

// TestRunWatchableRunsOnceWhenOff is the default, non-watching path -- no
// goroutine, no ticker, exactly one call.
func TestRunWatchableRunsOnceWhenOff(t *testing.T) {
	calls := 0
	if err := runWatchable(0, func() error { calls++; return nil }); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

// TestRunWatchableRepeatsUntilStopped covers the actual loop: it must call
// render more than once on a short interval, and a render error must stop
// the loop and propagate rather than being swallowed.
func TestRunWatchableRepeatsUntilStopped(t *testing.T) {
	calls := 0
	done := make(chan error, 1)
	go func() {
		done <- runWatchable(5*time.Millisecond, func() error {
			calls++
			if calls >= 3 {
				return errStop
			}
			return nil
		})
	}()
	select {
	case err := <-done:
		if err != errStop {
			t.Fatalf("err = %v, want errStop", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runWatchable never returned")
	}
	if calls != 3 {
		t.Errorf("calls = %d, want exactly 3 (stopped by the render error)", calls)
	}
}

// TestRunWatchableIgnoresWatchUnderJSON: --json is one machine-readable
// response, not a human watching a terminal scroll by.
func TestRunWatchableIgnoresWatchUnderJSON(t *testing.T) {
	old := out.JSON
	out.JSON = true
	defer func() { out.JSON = old }()

	calls := 0
	if err := runWatchable(5*time.Millisecond, func() error { calls++; return nil }); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (watch should be ignored under --json)", calls)
	}
}

type stopError struct{}

func (stopError) Error() string { return "stop" }

var errStop error = stopError{}
