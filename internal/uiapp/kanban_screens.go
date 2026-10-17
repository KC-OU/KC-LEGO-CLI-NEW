package uiapp

import (
	"fmt"
	"strings"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

// The kanban board: a read-only view over the existing ticket queue and the
// new backlog gate (internal/lego/kanban.go), plus notes — it never changes
// ticket state itself (claiming/finishing a job still happens on the normal
// check/order screens, reached here via openTicket, exactly as "My Current
// Jobs" already did). See docs/guides/kanban.md.

const (
	scrKanbanBoard    = "kanban_board"     // picker/checker: their own board
	scrKanbanAdmin    = "kanban_admin"     // admin entry menu
	scrKanbanBoardAll = "kanban_board_all" // admin: everyone's board, or one user's (app.kanbanFilterUser)
	scrKanbanBacklog  = "kanban_backlog"   // the shared backlog list
	scrKanbanNoteAdd  = "kanban_note_add"  // note-add form
)

type kanbanNoteDraft struct {
	ticketID int64
	label    string
}

func kanbanScreens() map[string]screenModel {
	return map[string]screenModel{
		scrKanbanBoard:    kanbanBoardScreen(),
		scrKanbanAdmin:    kanbanAdminScreen(),
		scrKanbanBoardAll: kanbanBoardAllScreen(),
		scrKanbanBacklog:  kanbanBacklogScreen(),
		scrKanbanNoteAdd:  kanbanNoteAddScreen(),
	}
}

// headerIdentity is what every screen's header shows for the signed-in user:
// just the username normally, but "<id> - <FullName> (<accuracy>%)" for a
// numeric-login Picker/Checker account that has both a recorded display name
// (access.User.FullName, see cmd/wms/access.go's `set-name`) and a scored
// role (roleForApp) — e.g. "7354 - Kieran Collins (100%)". Either piece
// missing just falls back to less: a name with no accuracy role shows "<id> -
// <FullName>", a scored role with no name shows "<id> (<accuracy>%)", and
// plain "<id>" covers everyone else (admins, operators, ...). Accuracy here
// is always the current, automatically-computed figure (internal/lego's
// accuracy.go) — there is no admin override to show instead.
func (a *App) headerIdentity() string {
	if a.session == nil {
		return ""
	}
	name := a.session.Username
	if a.pUser != nil && a.pUser.FullName != "" {
		name += " - " + a.pUser.FullName
	}
	if role := roleForApp(a); role != "" {
		if acc, err := a.legoDB.AccuracyToday(a.session.Username, role); err == nil {
			name += fmt.Sprintf(" (%.0f%%)", acc)
		}
	}
	return name
}

// openKanban is the Shift+F1/F13 shortcut's destination (app.go's
// handleGlobalKey, hub screens only): the admin entry point from the general
// hub, the own-job board from the picker/checker hub. A non-admin somehow
// reaching this from scrHub just gets scrKanbanAdmin's usual denial — no
// special-casing needed here.
func (a *App) openKanban() {
	if a.cur == scrPickerHub {
		a.goTo(scrKanbanBoard)
		return
	}
	a.goTo(scrKanbanAdmin)
}

// ---- Picker/checker: own board ----

func kanbanBoardScreen() screenModel {
	return &menuScreen{
		panelID: "KANBAN",
		title:   "Kanban",
		options: func(app *App) []menuOption {
			var opts []menuOption
			if t, _ := app.legoDB.CurrentTicket(app.userName()); t != nil {
				tk := t
				opts = append(opts,
					menuOption{Key: "1", Label: "Open: " + tk.Label, Go: func(app *App) { openTicket(app, tk) }},
					menuOption{Key: "2", Label: "Add a note to this job", Go: func(app *App) { startKanbanNote(app, tk.ID, tk.Label) }},
				)
			}
			opts = append(opts,
				menuOption{Key: "8", Label: "View the backlog", Go: func(app *App) { app.goTo(scrKanbanBacklog) }},
				menuOption{Key: "0", Label: "Return", Go: func(app *App) { app.onBack() }},
			)
			return opts
		},
		intro: func(app *App) string {
			board, err := app.legoDB.KanbanBoard(app.userName())
			if err != nil {
				return app.theme.Danger.Render(err.Error())
			}
			return renderKanbanCards(app, board, false)
		},
	}
}

func startKanbanNote(app *App, ticketID int64, label string) {
	app.kanbanNote = &kanbanNoteDraft{ticketID: ticketID, label: label}
	app.goTo(scrKanbanNoteAdd)
}

func kanbanNoteAddScreen() screenModel {
	return &formScreen{
		panelID: "KBNOTE",
		title:   "Add a Note",
		build: func(app *App) []ui.Field {
			label := ""
			if app.kanbanNote != nil {
				label = app.kanbanNote.label
			}
			return []ui.Field{
				{Label: "Job", Value: label, Protected: true},
				{Label: "Note"},
				{Label: "Share with admin? (yes/no)", Value: "no"},
			}
		},
		submit: func(app *App, v []string) {
			d := app.kanbanNote
			if d == nil {
				app.onBack()
				return
			}
			body := strings.TrimSpace(v[1])
			if body == "" {
				app.setMsg("A note needs some text.", true)
				return
			}
			shared := strings.EqualFold(strings.TrimSpace(v[2]), "yes") || strings.EqualFold(strings.TrimSpace(v[2]), "y")
			visibility := "private"
			if shared {
				visibility = "shared"
			}
			if _, err := app.legoDB.AddKanbanNote(d.ticketID, "", -1, 0, body, visibility, app.userName()); err != nil {
				app.setMsg(err.Error(), true)
				return
			}
			if shared {
				detail := fmt.Sprintf("on %s: %s", d.label, body)
				_ = app.legoDB.LogEvent(lego.EventKanbanNoteShared, app.userName(), d.label, detail)
				app.notifyEvent(lego.EventKanbanNoteShared, app.userName()+" shared a note on "+d.label, body)
			}
			app.audit.Log(app.userName(), "", "KANBAN_NOTE_ADDED", "SUCCESS", fmt.Sprintf("ticket=%d shared=%v", d.ticketID, shared))
			app.kanbanNote = nil
			app.setMsg("Note added.", false)
			app.onBack()
		},
	}
}

// ---- Admin ----

func kanbanAdminScreen() screenModel {
	return &menuScreen{
		panelID:      "KANBAN-A",
		title:        "Admin: Kanban Board",
		adminGated:   true,
		deniedAction: "KANBAN_ADMIN",
		options: func(app *App) []menuOption {
			return []menuOption{
				{Key: "1", Label: "View all boards", Go: func(app *App) { app.kanbanFilterUser = ""; app.goTo(scrKanbanBoardAll) }},
				{Key: "2", Label: "View a specific user's board…", Go: startKanbanUserPick},
				{Key: "3", Label: "Release a backlog set…", Go: startKanbanRelease},
				{Key: "4", Label: "View the backlog", Go: func(app *App) { app.goTo(scrKanbanBacklog) }},
				{Key: "0", Label: "Return", Go: func(app *App) { app.onBack() }},
			}
		},
	}
}

func startKanbanUserPick(app *App) {
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
		Header: "View whose board?",
		Prompt: "Choose",
		Items:  items,
		OnPick: func(app *App, it pickItem) {
			app.kanbanFilterUser = it.Key
			app.cur, app.stack = scrKanbanBoardAll, nil
		},
	})
}

func startKanbanRelease(app *App) {
	backlog, _ := app.legoDB.BacklogSets()
	if len(backlog) == 0 {
		app.setMsg("The backlog is empty — nothing to release.", false)
		app.onBack()
		return
	}
	items := make([]pickItem, len(backlog))
	for i, s := range backlog {
		items[i] = pickItem{Key: s.SetNum, Label: s.SetNum}
	}
	startPick(app, &pickState{
		Header: "Release which set from the backlog?",
		Prompt: "Choose",
		Items:  items,
		OnPick: func(app *App, it pickItem) {
			if err := app.legoDB.SetBacklogStatus(it.Key, lego.BacklogReleased, app.userName()); err != nil {
				app.setMsg(err.Error(), true)
				app.cur = scrKanbanAdmin
				return
			}
			detail := "set " + it.Key
			_ = app.legoDB.LogEvent(lego.EventKanbanReleased, app.userName(), it.Key, detail)
			app.notifyEvent(lego.EventKanbanReleased, "Set "+it.Key+" released from the kanban backlog by "+app.userName(), "")
			app.audit.Log(app.userName(), "", "KANBAN_RELEASED", "SUCCESS", detail)
			app.setMsg("Released "+it.Key+" — it's now checkable.", false)
			app.cur = scrKanbanAdmin
		},
	})
}

func kanbanBoardAllScreen() screenModel {
	return &menuScreen{
		panelID:      "KANBAN-ALL",
		title:        "Kanban: All Boards",
		adminGated:   true,
		deniedAction: "KANBAN_ADMIN",
		options: func(app *App) []menuOption {
			return []menuOption{
				{Key: "0", Label: "Return", Go: func(app *App) { app.kanbanFilterUser = ""; app.onBack() }},
			}
		},
		intro: func(app *App) string {
			var board []lego.KanbanCard
			var err error
			title := "Everyone"
			if app.kanbanFilterUser != "" {
				board, err = app.legoDB.KanbanBoard(app.kanbanFilterUser)
				title = app.kanbanFilterUser
			} else {
				board, err = app.legoDB.KanbanBoardAll()
			}
			if err != nil {
				return app.theme.Danger.Render(err.Error())
			}
			return app.theme.Muted.Render("Viewing: "+title) + "\n" + renderKanbanCards(app, board, app.kanbanFilterUser == "")
		},
	}
}

// ---- Backlog (shared, read-only) ----

func kanbanBacklogScreen() screenModel {
	return &tableScreen{
		panelID: "BACKLOG",
		title:   "Kanban Backlog",
		columns: []string{"Set"},
		fetch: func(app *App) ([][]string, string, error) {
			backlog, err := app.legoDB.BacklogSets()
			rows := make([][]string, len(backlog))
			for i, s := range backlog {
				rows[i] = []string{s.SetNum}
			}
			note := fmt.Sprintf("%d set(s) waiting for an admin to release", len(rows))
			if app.isAdmin() {
				note += " — see Admin: Kanban Board to release one"
			}
			return rows, note, err
		},
	}
}

// ---- rendering ----

var kanbanColumns = []struct{ key, label string }{
	{lego.KanbanColBacklog, "Backlog"},
	{lego.KanbanColActive, "Active"},
	{lego.KanbanColDone, "Done"},
}

func renderKanbanCards(app *App, cards []lego.KanbanCard, showAssignee bool) string {
	if len(cards) == 0 {
		return app.theme.Muted.Render("Nothing on the board right now.")
	}
	t := app.theme
	byCol := map[string][]lego.KanbanCard{}
	for _, c := range cards {
		byCol[c.Column] = append(byCol[c.Column], c)
	}
	var b strings.Builder
	for _, col := range kanbanColumns {
		rows := byCol[col.key]
		if len(rows) == 0 {
			continue
		}
		b.WriteString(t.Accent.Render(fmt.Sprintf("%s (%d)", col.label, len(rows))) + "\n")
		for _, c := range rows {
			name := c.Ticket.Label
			if name == "" {
				name = c.Ticket.Target
			}
			line := "  " + name
			if showAssignee && c.Ticket.AssignedTo != "" {
				line += "  [" + c.Ticket.AssignedTo + "]"
			}
			if c.Summary != "" {
				badge := c.Summary
				switch {
				case strings.Contains(badge, "M"):
					badge = t.Danger.Render(badge)
				case strings.Contains(badge, "E"):
					badge = t.Info.Render(badge)
				}
				line += "  " + badge
			}
			if c.Abandoned {
				line += "  " + t.Warning.Render("(abandoned)")
			}
			if c.Age > 0 {
				line += "  " + t.Muted.Render(formatKanbanAge(c.Age))
			}
			b.WriteString(line + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func formatKanbanAge(d time.Duration) string {
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
