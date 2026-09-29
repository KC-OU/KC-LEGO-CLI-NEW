package uiapp

import (
	"fmt"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"
)

// A one-way admin note, shown as a full-screen page — the same "read it, press a
// key to dismiss it" treatment the first-run tour uses (tour.go) — rather than a
// small corner popup, so a long message is never cramped or cut off. Reached the
// same way the tour is: App.goTo, pushing whatever screen you were on, so
// dismissing returns you exactly there. Sessions are separate OS processes (see
// gateway/telnet.go), so delivery is a poll (enterHub, and the 15s idle tick),
// not a push.

const scrMessagesFull = "messages_full"

type pendingMessage struct {
	ID         int64
	From, Body string
}

func (a *App) queueMessage(id int64, from, body string) {
	a.toasts = append(a.toasts, pendingMessage{ID: id, From: from, Body: body})
}

// pollMessages checks for messages addressed to the signed-in user not yet shown
// by any of their sessions, queueing each and marking it delivered — the caller
// decides whether to interrupt with scrMessagesFull (see enterHub, app.go's idle
// tick) based on whether anything new actually showed up.
func (a *App) pollMessages() bool {
	if a.session == nil {
		return false
	}
	msgs, err := a.legoDB.UndeliveredMessages(a.session.Username)
	if err != nil || len(msgs) == 0 {
		return false
	}
	for _, m := range msgs {
		a.queueMessage(m.ID, m.From, m.Body)
		_ = a.legoDB.MarkDelivered(m.ID)
	}
	return true
}

func messagesFullScreen() screenModel { return &messagesScreenImpl{} }

type messagesScreenImpl struct{ base }

func (s *messagesScreenImpl) PanelID() string  { return "MSG" }
func (s *messagesScreenImpl) OnEnter(app *App) {}
func (s *messagesScreenImpl) Title() string {
	return "Message"
}
func (s *messagesScreenImpl) FKeys() [][2]string {
	return [][2]string{{"Enter", "Next"}, {"Q", "Dismiss all"}}
}

func (s *messagesScreenImpl) HandleKey(app *App, msg tea.KeyMsg) {
	switch {
	case msg.Type == tea.KeyEsc, isKey(msg, 'q'):
		app.toasts = nil
		app.onBack()
	case msg.Type == tea.KeyEnter:
		if len(app.toasts) > 0 {
			app.toasts = app.toasts[1:]
		}
		if len(app.toasts) == 0 {
			app.onBack()
		}
	}
}

func (s *messagesScreenImpl) Body(app *App) string {
	t := app.theme
	if len(app.toasts) == 0 {
		return t.Muted.Render("No messages.")
	}
	m := app.toasts[0]
	header := t.Strong.Render(fmt.Sprintf("From %s  (%d of %d)", m.From, 1, len(app.toasts)))
	footer := "Enter: "
	if len(app.toasts) > 1 {
		footer += "next message"
	} else {
		footer += "done"
	}
	footer += "    Q: dismiss all"
	return header + "\n\n" + t.Text.Render(m.Body) + "\n\n" + t.Muted.Render(footer)
}

// ---- Admin: send a message ----

const scrMessagePick = "message_pick"

type messageDraft struct {
	to string
}

func messagePickScreen() screenModel {
	return &menuScreen{
		panelID:      "MSGPIK",
		title:        "Message a User",
		adminGated:   true,
		deniedAction: "SETTINGS_ACCESS",
		options: func(app *App) []menuOption {
			return []menuOption{
				{Key: "1", Label: "Pick a user…", Go: startMessagePick},
				{Key: "0", Label: "Return", Go: func(app *App) { app.onBack() }},
			}
		},
	}
}

func startMessagePick(app *App) {
	rows, _ := app.users.ListAll(app.ctx())
	var items []pickItem
	seen := map[string]bool{}
	for _, r := range rows {
		if r.Username == "" || seen[r.Username] {
			continue
		}
		seen[r.Username] = true
		items = append(items, pickItem{Key: r.Username, Label: r.Username + "  (id " + r.ID + ", " + r.RoleOrGroup + ")"})
	}
	startPick(app, &pickState{
		Header: "Message who?",
		Prompt: "Choose",
		Items:  items,
		OnPick: func(app *App, it pickItem) {
			app.message2 = &messageDraft{to: it.Key}
			app.goTo(scrMessageCompose)
		},
	})
}

var quickMessages = []pickItem{
	{Key: "double_check", Label: "Please double check your last set — accuracy dipped"},
	{Key: "finish_today", Label: "Can you finish what's outstanding today?"},
	{Key: "nice_work", Label: "Nice work — keep it up"},
}

const scrMessageCompose = "message_compose"

func messageComposeScreen() screenModel {
	return &menuScreen{
		panelID: "MSGCMP",
		title:   "Message",
		options: func(app *App) []menuOption {
			var opts []menuOption
			for i, q := range quickMessages {
				body := q.Label
				opts = append(opts, menuOption{Key: strconv.Itoa(i + 1), Label: body, Go: func(app *App) { sendAdminMessage(app, body) }})
			}
			opts = append(opts, menuOption{Key: "w", Label: "Write your own…", Go: func(app *App) {
				startPick(app, &pickState{
					Header:    "Message",
					Prompt:    "Type it",
					AllowFree: true,
					FreeHint:  "your message",
					OnFree:    func(app *App, text string) { sendAdminMessage(app, text) },
				})
			}})
			opts = append(opts, menuOption{Key: "0", Label: "Cancel", Go: func(app *App) { app.message2 = nil; app.onBack() }})
			return opts
		},
		intro: func(app *App) string {
			if app.message2 == nil {
				return ""
			}
			return app.theme.Muted.Render("To: " + app.message2.to)
		},
	}
}

func sendAdminMessage(app *App, body string) {
	if app.message2 == nil || body == "" {
		app.onBack()
		return
	}
	if err := app.legoDB.SendMessage(app.userName(), app.message2.to, body); err != nil {
		app.setMsg(err.Error(), true)
		return
	}
	app.audit.Log(app.userName(), "", "MESSAGE_SENT", "SUCCESS", "to="+app.message2.to)
	app.setMsg("Sent to "+app.message2.to+".", false)
	app.message2 = nil
	app.stack = nil
	app.cur = scrAdminHub
}
