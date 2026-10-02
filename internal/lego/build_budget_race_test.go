//go:build race

package lego

import "time"

// canBuildBudget is generous here: modernc.org/sqlite is pure Go, so -race
// instruments every memory access inside the SQL engine itself, not just
// this package's own code — a well-documented, large (2-20x, workload
// dependent) and variable slowdown, on top of whatever else is contending
// for CPU in the same run (e.g. the full test suite running every package's
// tests at once). TestCanBuildIsFastAtScale's query is a single, already
// well-formed SQL statement (see CanBuild in build.go) scanning real rows at
// real scale — this budget exists to catch an accidental algorithmic
// regression (something turning quadratic, say), not to hold the race
// detector's own instrumentation overhead to the same bar as a normal run.
const canBuildBudget = 15 * time.Second
