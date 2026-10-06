package main

import (
	"strings"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

func TestFormatTeamSummaryIncludesHoursAndOnlyActualAccuracy(t *testing.T) {
	pickerPct, checkerPct := 95.0, 88.5
	out := formatTeamSummary([]lego.TeamSummaryLine{
		{Username: "dave", HoursWorked: 7.5, CheckerAccuracy: &checkerPct},
		{Username: "sam", HoursWorked: 4, PickerAccuracy: &pickerPct, CheckerAccuracy: &checkerPct},
		{Username: "ann", HoursWorked: 2},
	})
	for _, want := range []string{"dave: 7.5h, checker 88%", "sam: 4.0h, picker 95%, checker 88%", "ann: 2.0h"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected summary to contain %q, got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "ann: 2.0h, picker") || strings.Contains(out, "ann: 2.0h, checker") {
		t.Errorf("ann had no scored activity — must not show an accuracy figure, got:\n%s", out)
	}
}
