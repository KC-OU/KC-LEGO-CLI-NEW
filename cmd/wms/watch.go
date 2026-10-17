package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

// addWatchFlag adds --watch <interval> (e.g. --watch 5s) to cmd, off by
// default (zero duration) -- "for anything glanced at repeatedly through a
// shift" (the rota, low stock, open orders) without needing a separate
// terminal re-running the command by hand, same idea as the Unix `watch`
// command, built in rather than assuming everyone has it.
func addWatchFlag(cmd *cobra.Command, watch *time.Duration) {
	cmd.Flags().DurationVar(watch, "watch", 0, "keep re-running and clearing the screen every interval (e.g. --watch 5s); 0 (default) runs once")
}

// runWatchable calls render once, or repeatedly (clearing the screen each
// time) until Ctrl-C/SIGTERM if watch is positive. --json is machine output
// for one call, not a human watching a terminal, so watch is ignored when
// out.JSON is set -- render runs exactly once either way.
func runWatchable(watch time.Duration, render func() error) error {
	if watch <= 0 || out.JSON {
		return render()
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	for {
		fmt.Print("\033[H\033[2J")
		if err := render(); err != nil {
			return err
		}
		fmt.Printf("\n(refreshing every %s — Ctrl-C to stop)\n", watch)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(watch):
		}
	}
}
