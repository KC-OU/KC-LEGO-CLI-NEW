package uiapp

import (
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

// TestAccuracyIsNSOnlyAppliesOnceTheGateIsOn covers the "day off" display:
// accuracyIsNS must stay false (never show NS) until an admin has actually
// turned on config.RequireClockIn — before that, nobody has rota data, so
// every day would otherwise wrongly read as "not scheduled".
func TestAccuracyIsNSOnlyAppliesOnceTheGateIsOn(t *testing.T) {
	app := newTestApp(t)

	if accuracyIsNS(app, "dave", "2026-10-10") {
		t.Fatal("the gate is off by default — nothing should ever show NS yet")
	}

	t.Setenv(config.RequireClockIn, "1")
	if !accuracyIsNS(app, "dave", "2026-10-10") {
		t.Fatal("gate on, no rota entry for that day: should show NS")
	}

	if err := app.legoDB.SetRota("dave", "2026-10-10", "09:00", "17:00", "", "admin"); err != nil {
		t.Fatal(err)
	}
	if accuracyIsNS(app, "dave", "2026-10-10") {
		t.Fatal("scheduled that day: should not show NS")
	}
	// A different day with no entry still shows NS even though dave has one for 10-10.
	if !accuracyIsNS(app, "dave", "2026-10-11") {
		t.Fatal("not scheduled on a different day: should show NS")
	}
}
