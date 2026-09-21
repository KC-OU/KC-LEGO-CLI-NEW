package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/auth"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/partdb"
)

// Scripting conventions for the commands people put in scripts (users, lego,
// backup, receive, sync status, audit): --json prints one JSON document on
// stdout, --quiet prints nothing on success, --yes skips a confirmation,
// --dry-run says what would happen and changes nothing, and the exit status
// says why a command failed.
const (
	exitFailure  = 1 // anything not covered below
	exitUsage    = 2 // bad flags or arguments, or a confirmation that needed --yes
	exitAuth     = 3 // wrong credentials, a rejected token, not allowed
	exitNetwork  = 4 // a service could not be reached or answered with an error
	exitNotFound = 5 // the user, part or set asked for does not exist
)

var out struct {
	JSON  bool
	Quiet bool
}

// addOutputFlags puts --json and --quiet on the root command, for every subcommand.
func addOutputFlags(root *cobra.Command) {
	root.PersistentFlags().BoolVar(&out.JSON, "json", false, "print machine-readable JSON on stdout (errors go to stderr as JSON)")
	root.PersistentFlags().BoolVarP(&out.Quiet, "quiet", "q", false, "print nothing on success (the exit status still tells the result)")
}

type exitError struct {
	Code int
	Err  error
}

func (e *exitError) Error() string { return e.Err.Error() }
func (e *exitError) Unwrap() error { return e.Err }

func withCode(code int, err error) error {
	if err == nil {
		return nil
	}
	return &exitError{Code: code, Err: err}
}

func usageError(format string, a ...any) error { return withCode(exitUsage, fmt.Errorf(format, a...)) }

// exitCodeFor maps an error to the documented exit status.
func exitCodeFor(err error) int {
	var ee *exitError
	if errors.As(err, &ee) {
		return ee.Code
	}
	var api *partdb.APIError
	var netErr net.Error
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials), errors.Is(err, auth.ErrAccountDisabled), errors.Is(err, partdb.ErrNoToken):
		return exitAuth
	case errors.As(err, &api):
		if api.Status == 401 || api.Status == 403 {
			return exitAuth
		}
		if api.Status == 404 {
			return exitNotFound
		}
		return exitNetwork
	case errors.As(err, &netErr), errors.Is(err, context.DeadlineExceeded):
		return exitNetwork
	}
	if msg := strings.ToLower(err.Error()); strings.Contains(msg, "not found") || strings.Contains(msg, "no such") {
		return exitNotFound
	}
	if msg := strings.ToLower(err.Error()); strings.HasPrefix(msg, "unknown command") || strings.HasPrefix(msg, "unknown flag") ||
		strings.HasPrefix(msg, "accepts ") || strings.HasPrefix(msg, "requires at least") || strings.HasPrefix(msg, "invalid argument") ||
		strings.HasPrefix(msg, "required flag") || strings.HasPrefix(msg, "unknown shorthand") || strings.HasPrefix(msg, "flag needs") {
		return exitUsage
	}
	return exitFailure
}

// reportError prints err the way the mode asks and returns the exit status.
func reportError(err error) int {
	code := exitCodeFor(err)
	if out.JSON {
		b, _ := json.Marshal(map[string]any{"error": err.Error(), "code": code})
		fmt.Fprintln(os.Stderr, string(b))
	} else {
		fmt.Fprintln(os.Stderr, err)
		if code == exitUsage {
			fmt.Fprintln(os.Stderr, "Run the command with --help for its usage.")
		}
	}
	return code
}

// say prints a human-readable line, unless a script asked for JSON or silence.
func say(line string) {
	if !out.JSON && !out.Quiet {
		fmt.Println(line)
	}
}

// emit prints v as the command's JSON result when --json is on.
func emit(v any) error {
	if !out.JSON {
		return nil
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}

// confirm asks a yes/no question; anything but y/yes is no. With --yes it does
// not ask. Without a terminal to answer on (a script) it refuses rather than
// guess, so a destructive command never runs unattended by accident.
func confirm(question string, yes bool) error {
	if yes {
		return nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) || out.JSON || out.Quiet {
		return usageError("%s — pass --yes to confirm when not running interactively", strings.TrimSuffix(question, "?"))
	}
	fmt.Print(question + " [y/N] ")
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	if l := strings.ToLower(strings.TrimSpace(line)); l != "y" && l != "yes" {
		return withCode(exitFailure, errors.New("cancelled — nothing was changed"))
	}
	return nil
}

// planned is the --dry-run result: what would have happened.
func planned(action string, details map[string]any) error {
	say("⚠ --dry-run: would " + action + " — nothing was changed.")
	return emit(map[string]any{"dry_run": true, "action": action, "details": details})
}
