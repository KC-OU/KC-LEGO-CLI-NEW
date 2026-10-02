//go:build !race

package lego

import "time"

// canBuildBudget: see build_budget_race_test.go for why this differs under
// -race. This is the real, meaningful regression budget for a normal run.
const canBuildBudget = 3 * time.Second
