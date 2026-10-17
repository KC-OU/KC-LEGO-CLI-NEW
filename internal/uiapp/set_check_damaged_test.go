package uiapp

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

// TestSetCheckDamagedKeyFlagsConditionWithoutChangingHaveOrMissing is
// roadmap item 6's TUI half (D key, set_check.go) -- a condition flag, not
// a quantity: marking 2 damaged on an already-complete line must leave
// Have/Missing alone, and show up in the summary line and the row's note.
func TestSetCheckDamagedKeyFlagsConditionWithoutChangingHaveOrMissing(t *testing.T) {
	app, _ := flowApp(t)
	buildCheckFixture(t, app)
	app.cur, app.stack = scrLegoHub, nil
	startCheck(app, "75192-1", lego.CheckIntake)
	if app.cur != scrSetCheck {
		t.Fatalf("check did not open: %q", app.message)
	}

	// First line (red 3001) starts "have all" already -- mark 2 damaged.
	typeKeys(app, "d")
	typeKeys(app, "2")
	key(app, tea.KeyEnter)

	if got := app.checking.check.Lines[0].Damaged; got != 2 {
		t.Fatalf("Damaged = %d, want 2", got)
	}
	if app.checking.check.Lines[0].Have != app.checking.check.Lines[0].Need {
		t.Errorf("Have should be unchanged (still full) after flagging damage")
	}
	if app.checking.check.Lines[0].Missing() != 0 {
		t.Errorf("Missing should be unchanged (still 0) after flagging damage")
	}

	v := plain(app.View())
	if !strings.Contains(v, "COMPLETE") {
		t.Errorf("a damaged-but-fully-present line must still read COMPLETE:\n%s", v)
	}
	if !strings.Contains(v, "2 damaged") {
		t.Errorf("summary line should mention the damaged count:\n%s", v)
	}
	if !strings.Contains(v, "D2 damaged") {
		t.Errorf("the line's own row should show its damaged count:\n%s", v)
	}
}
