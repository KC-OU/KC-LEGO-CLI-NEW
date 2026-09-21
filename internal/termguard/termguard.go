// Package termguard stops the program from stalling at start-up on a terminal that does not answer.
//
// bubbletea's package init asks the terminal for its background colour (an OSC 11 query plus a cursor
// position request) and waits up to five seconds for the reply, and it does so for every program that
// links it, including `wms version`. A real terminal answers at once; a slow SSH client, a serial console
// or a screen-scraping harness does not. termenv skips the query when TERM is "dumb" (or screen/tmux),
// so this package's init sets TERM=dumb for the moment the other packages initialise, and main calls
// Restore first thing to put the real value back (colour support is decided lazily, after that).
//
// The import path sorts before github.com/charmbracelet/..., and Go initialises independent packages in
// import-path order, which is what makes this run first. Keep it imported by package main.
package termguard

import "os"

const saved = "WMS_TERMGUARD_TERM"

func init() {
	term, ok := os.LookupEnv("TERM")
	if !ok || term == "" || term == "dumb" {
		return
	}
	if fi, err := os.Stdout.Stat(); err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return // not a terminal: nothing is queried anyway
	}
	os.Setenv(saved, term)
	os.Setenv("TERM", "dumb")
}

// Restore puts the terminal type back. It is safe to call more than once.
func Restore() {
	if term, ok := os.LookupEnv(saved); ok {
		os.Setenv("TERM", term)
		os.Unsetenv(saved)
	}
}
