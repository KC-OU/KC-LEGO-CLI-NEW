package uiapp

import "github.com/KC-OU/KC-LEGO-CLI-NEW/internal/notify"

// notifyEvent sends an alert without making the user wait.
func (a *App) notifyEvent(event, title, body string) {
	if a.hooksSync { // tests
		notify.Dispatch(event, title, body)
		return
	}
	go notify.Dispatch(event, title, body)
}
