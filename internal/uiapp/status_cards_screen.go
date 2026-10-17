package uiapp

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

// Status cards: a quick, visible "why this person looks idle right now" --
// Delay or No Work Available, put on someone by an admin (never self-
// applied, unlike the break card a worker scans themself -- see
// lego.ScanStatusCard) -- so the supervisor idle-ticket nudge doesn't nag
// about something already explained (see ticketWatch in cmd/wms/gateway.go)
// and the Open Assignments queue (tickets_screens.go) shows it next to
// their name. Reached from the Admin hub, same module gate ("user_mgmt") as
// Live Sessions / Weekly Rota alongside it -- no separate gate on this
// screen itself.
const (
	scrStatusCards    = "status_cards"
	scrStatusCardNote = "status_card_note"
)

func statusCardsScreen() screenModel {
	return &selectList{
		panelID:   "STCARD",
		title:     "Status Cards (Delay / No Work)",
		hint:      "↑/↓ choose  N new card  C clear  Esc back",
		emptyHint: "Nobody has a card on right now — N puts one on someone.",
		rows:      statusCardRows,
		keys:      statusCardKeys,
	}
}

func statusCardRows(app *App) ([]string, [][]string, []string) {
	cards, err := app.legoDB.ActiveStatusCards()
	if err != nil {
		app.setMsg(err.Error(), true)
	}
	var rows [][]string
	var keys []string
	for _, c := range cards {
		rows = append(rows, []string{c.Username, lego.CardReasonName(c.Reason), orDash(c.Note), c.StartedAt.Format("15:04"), c.CreatedBy})
		keys = append(keys, c.Username)
	}
	return []string{"Username", "Reason", "Note", "Since", "Set by"}, rows, keys
}

func statusCardKeys(app *App, username string, msg tea.KeyMsg) {
	switch {
	case isKey(msg, 'n'):
		startStatusCardUserPick(app)
	case isKey(msg, 'c') && username != "":
		if err := app.legoDB.ClearStatusCard(username, app.userName()); err != nil {
			app.setMsg(err.Error(), true)
			return
		}
		app.audit.Log(app.userName(), "", "STATUS_CARD_CLEARED", "SUCCESS", username)
		app.setMsg(username+"'s card cleared.", false)
	}
}

type statusCardDraft struct {
	username, reason string
}

func startStatusCardUserPick(app *App) {
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
		Header: "Put a card on whom?",
		Prompt: "Choose",
		Items:  items,
		OnPick: func(app *App, it pickItem) {
			startStatusCardReasonPick(app, it.Key)
		},
	})
}

func startStatusCardReasonPick(app *App, username string) {
	startPick(app, &pickState{
		Header: "Which reason?",
		Prompt: "Choose",
		Items:  []pickItem{{Key: lego.CardDelay, Label: "Delay"}, {Key: lego.CardNoWork, Label: "No work available"}},
		OnPick: func(app *App, it pickItem) {
			app.statusCard = &statusCardDraft{username: username, reason: it.Key}
			app.goTo(scrStatusCardNote)
		},
	})
}

func statusCardNoteScreen() screenModel {
	return &formScreen{
		panelID: "STCNOT",
		title:   "Status Cards (Delay / No Work)",
		preamble: func(app *App) string {
			if app.statusCard == nil {
				return ""
			}
			return app.theme.Muted.Render("Putting a " + lego.CardReasonName(app.statusCard.reason) + " card on " + app.statusCard.username + ".")
		},
		build: func(app *App) []ui.Field {
			return []ui.Field{{Label: "Note (optional)"}}
		},
		submit: func(app *App, values []string) {
			d := app.statusCard
			if d == nil {
				app.onBack()
				return
			}
			c, err := app.legoDB.SetStatusCard(d.username, d.reason, values[0], app.userName())
			if err != nil {
				app.setMsg(err.Error(), true)
				return
			}
			app.audit.Log(app.userName(), "", "STATUS_CARD_SET", "SUCCESS", c.Username+": "+lego.CardReasonName(c.Reason))
			app.statusCard = nil
			app.back()
			app.setMsg(c.Username+" now shows "+lego.CardReasonName(c.Reason)+".", false)
		},
	}
}
