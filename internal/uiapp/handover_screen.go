package uiapp

import (
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
