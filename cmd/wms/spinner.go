package main

import (
	"fmt"
	"os"
	"time"

	"golang.org/x/term"
)

// withSpinner runs work with a spinning "label…" indicator on stderr, so a slow
// one-shot command (a BrickLink price refresh, a big import) doesn't sit silent.
// Only drawn when stderr is a real terminal and output isn't --json/--quiet;
// otherwise work just runs, since control codes would corrupt a script's output.
func withSpinner(label string, work func() error) error {
	if !term.IsTerminal(int(os.Stderr.Fd())) || out.JSON || out.Quiet {
		return work()
	}
	done := make(chan error, 1)
	go func() { done <- work() }()
	frames := [...]string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	t := time.NewTicker(120 * time.Millisecond)
	defer t.Stop()
	for i := 0; ; i++ {
		select {
		case err := <-done:
			fmt.Fprint(os.Stderr, "\r\033[K")
			return err
		case <-t.C:
			fmt.Fprintf(os.Stderr, "\r\033[K%s %s", frames[i%len(frames)], label)
		}
	}
}
