package uiapp

import (
	"fmt"
	"strings"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

// The shift handover note: one free-text note anyone can leave for whoever's on
// next, shown once at sign-in (see showHandoverNoteOnce) and editable any time
// from the Admin hub.
const scrHandover = "handover"

// showHandoverNoteOnce queues the shift handover note (if any) into the same
// full-screen message queue pollMessages uses — the caller (enterHub) decides
// whether to actually navigate to scrMessagesFull, once, after both have had a
// chance to add something.
func (a *App) showHandoverNoteOnce() bool {
	if !isPickerOrChecker(a) {
		return false
	}
	n, err := a.legoDB.GetHandoverNote()
	if err != nil || n == nil || n.Body == "" {
		return false
	}
	a.queueMessage(0, "Handover note ("+n.CreatedBy+")", n.Body)
	return true
}

// shiftSummaryRecency is how long after a shift ends its auto-generated
// summary is still worth showing the next person to sign in — a day-old
// "what got done" is still handover-relevant; a week-old one is just noise.
// Unlike the manual note (deliberately evergreen until someone changes it),
// this one ages out.
const shiftSummaryRecency = 24 * time.Hour

// showShiftSummaryOnce queues the auto shift-handover summary (what got
// done, what's still open) for the most recently closed shift, system-wide
// — the automatic counterpart to showHandoverNoteOnce's manual note, shown
// alongside it rather than instead of it. See lego.ShiftHandoverSummary's
// own doc comment for why this is computed fresh here rather than
// generated and stored the instant ClockOut runs.
func (a *App) showShiftSummaryOnce() bool {
	if !isPickerOrChecker(a) {
		return false
	}
	e, err := a.legoDB.LastClosedShift()
	if err != nil || e == nil || time.Since(e.ClockOutAt) > shiftSummaryRecency {
		return false
	}
	s, err := a.legoDB.ShiftHandoverSummary(*e)
	if err != nil {
		return false
	}
	if len(s.Done) == 0 && len(s.StillOpen) == 0 {
		return false
	}
	a.queueMessage(0, fmt.Sprintf("Shift summary (%s, %s)", s.Username, roughDuration(s.ShiftEnd.Sub(s.ShiftStart))), formatShiftSummary(s))
	return true
}

func formatShiftSummary(s *lego.ShiftHandoverSummary) string {
	var b strings.Builder
	if len(s.Done) == 0 {
		b.WriteString("Nothing finished this shift.\n")
	} else {
		fmt.Fprintf(&b, "Done (%d):\n", len(s.Done))
		for _, t := range s.Done {
			fmt.Fprintf(&b, "  • %s\n", t.Label)
		}
	}
	if len(s.StillOpen) == 0 {
		b.WriteString("\nNothing still open.")
	} else {
		fmt.Fprintf(&b, "\nStill open (%d):\n", len(s.StillOpen))
		for _, t := range s.StillOpen {
			who := t.AssignedTo
			if who == "" {
				who = "open queue"
			}
			fmt.Fprintf(&b, "  • %s (%s)\n", t.Label, who)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func handoverScreen() screenModel {
	return &formScreen{
		panelID: "HANDOV",
		title:   "Shift Handover Note",
		build: func(app *App) []ui.Field {
			cur := ""
			if n, err := app.legoDB.GetHandoverNote(); err == nil && n != nil {
				cur = n.Body
			}
			return []ui.Field{{Label: "Note for whoever's on next (blank clears it)", Value: cur}}
		},
		submit: func(app *App, values []string) {
			if err := app.legoDB.SetHandoverNote(values[0], app.userName()); err != nil {
				app.setMsg(err.Error(), true)
				return
			}
			app.audit.Log(app.userName(), "", "HANDOVER_NOTE", "SUCCESS", "")
			app.setMsg("Saved.", false)
			app.onBack()
		},
	}
}
