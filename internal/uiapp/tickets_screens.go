package uiapp

import (
	"fmt"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/auth"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

// Assigned work: an admin points a picker at an order or a checker at a set (see
// internal/lego/tickets.go); claiming one just opens the normal check/order screen
// — this file is the queue, the claim, and the "leave this job" 3-way prompt.

const (
	scrAssignPick   = "assign_pick"
	scrAssignTarget = "assign_target"
	scrJobQueue     = "job_queue"
	scrRequest      = "request_job"
	scrCurrentJob   = "current_job"
	scrScanClaim    = "scan_claim"
	scrQuitJob      = "quit_job"
	scrAbandonAuth  = "abandon_auth"
)

// roleForApp is which accuracy/ticket role the signed-in user works as, decided by
// which permission they actually hold — "" if neither (they see no picker/checker
// screens at all).
func roleForApp(app *App) string {
	switch {
	case app.can("sets.check"):
		return lego.AccuracyChecker
	case app.can("orders.manage"):
		return lego.AccuracyPicker
	default:
		return ""
	}
}

// isPickerOrChecker is the landing-screen routing test: a narrowly-scoped
// operational account (picker or checker) gets the dedicated hub in this file
// instead of the general ModernWMS/Part-DB hub — anyone with broader warehouse-ops
// or admin access keeps the general hub even if they also hold sets.check/
// orders.manage (an operator, for instance).
func isPickerOrChecker(app *App) bool {
	return roleForApp(app) != "" && !app.can("ops.view") && !app.can("users.view")
}

// landingScreen is where enterHub sends a freshly signed-in session.
func (a *App) landingScreen() string {
	if isPickerOrChecker(a) {
		return scrPickerHub
	}
	return scrHub
}

func ticketKindFor(role string) string {
	if role == lego.AccuracyPicker {
		return lego.TicketOrder
	}
	return lego.TicketCheck
}

// openTicket navigates into whatever a ticket points at, resuming a check draft
// or an order's lines exactly as if reached the ordinary way — the ticket only
// remembers who's on it, never a copy of the check/order data.
func openTicket(app *App, t *lego.Ticket) {
	app.currentTicketID = t.ID
	switch t.Kind {
	case lego.TicketCheck:
		kind := lego.CheckIntake
		if app.legoDB.GetSetState(t.Target).Checked() {
			kind = lego.CheckRecount
		}
		startCheck(app, t.Target, kind)
	case lego.TicketOrder:
		id, _ := strconv.ParseInt(t.Target, 10, 64)
		app.ws().orderID = id
		app.goTo(scrOrderLines)
	}
}

// ---- Request: claim an open ticket ----

func requestJobRows(app *App) ([]string, [][]string, []string) {
	role := roleForApp(app)
	tickets, err := app.legoDB.OpenTickets(ticketKindFor(role), app.userName())
	if err != nil {
		app.setMsg(err.Error(), true)
	}
	var rows [][]string
	var keys []string
	for _, t := range tickets {
		who := "open — first come, first served"
		if t.AssignedTo != "" {
			who = "assigned to you"
		}
		pri := t.Priority
		if pri == "" {
			pri = "—"
		}
		rows = append(rows, []string{t.Label, who, pri, t.Note})
		keys = append(keys, strconv.FormatInt(t.ID, 10))
	}
	return []string{"Set/Order", "Status", "Priority", "Note"}, rows, keys
}

func requestJobKeys(app *App, key string, msg tea.KeyMsg) {
	if msg.Type != tea.KeyEnter || key == "" {
		return
	}
	id, _ := strconv.ParseInt(key, 10, 64)
	if err := app.legoDB.ClaimTicket(id, app.userName()); err != nil {
		app.setMsg(err.Error(), true)
		return
	}
	tickets, _ := app.legoDB.OpenTickets(ticketKindFor(roleForApp(app)), app.userName())
	for _, t := range tickets {
		if t.ID == id {
			openTicket(app, &t)
			return
		}
	}
	// Claimed but no longer "open" (expected — claiming moves it out of OpenTickets):
	// look it up directly instead of failing the claim over a stale local list.
	if t, err := app.legoDB.CurrentTicket(app.userName()); err == nil && t != nil {
		openTicket(app, t)
	}
}

func requestJobScreen() screenModel {
	return &selectList{
		panelID:   "REQJOB",
		title:     "Request work",
		hint:      "Enter claims it and opens it",
		emptyHint: "Nothing queued right now — check back later, or ask an admin.",
		rows:      requestJobRows,
		keys:      requestJobKeys,
	}
}

// ---- My Current Job ----

func currentJobScreen() screenModel {
	return &menuScreen{
		panelID: "CURJOB",
		title:   "My Current Job",
		options: func(app *App) []menuOption {
			t, err := app.legoDB.CurrentTicket(app.userName())
			if err != nil || t == nil {
				return []menuOption{{Key: "0", Label: "Return", Go: func(app *App) { app.onBack() }}}
			}
			return []menuOption{
				{Key: "1", Label: "Open: " + t.Label, Go: func(app *App) { openTicket(app, t) }},
				{Key: "0", Label: "Return", Go: func(app *App) { app.onBack() }},
			}
		},
		intro: func(app *App) string {
			t, err := app.legoDB.CurrentTicket(app.userName())
			if err != nil {
				return app.theme.Danger.Render(err.Error())
			}
			if t == nil {
				return app.theme.Muted.Render("Nothing assigned right now — Request work to pick one up.")
			}
			note := ""
			if t.Note != "" {
				note = " — " + t.Note
			}
			return app.theme.Text.Render(fmt.Sprintf("%s (claimed %s)%s", t.Label, t.ClaimedAt.Format("Jan 2 15:04"), note))
		},
	}
}

// ---- Admin: assign work ----

func assignPickScreen() screenModel {
	return &menuScreen{
		panelID:      "ASSGNK",
		title:        "Assign Work",
		adminGated:   true,
		deniedAction: "SETTINGS_ACCESS",
		options: func(app *App) []menuOption {
			return []menuOption{
				{Key: "1", Label: "Assign a set to check", Go: func(app *App) { startAssign(app, lego.TicketCheck) }},
				{Key: "2", Label: "Assign an order to pick", Go: func(app *App) { startAssign(app, lego.TicketOrder) }},
				{Key: "3", Label: "Open queue (who's on what)", Go: func(app *App) { app.goTo(scrJobQueue) }},
				{Key: "0", Label: "Return", Go: func(app *App) { app.onBack() }},
			}
		},
	}
}

type assignDraft struct {
	kind, target, label string
}

func startAssign(app *App, kind string) {
	app.assign = &assignDraft{kind: kind}
	app.goTo(scrAssignTarget)
}

func assignTargetScreen() screenModel {
	return &formScreen{
		panelID: "ASGTGT",
		title:   "Assign — which one?",
		build: func(app *App) []ui.Field {
			label := "Set number"
			if app.assign != nil && app.assign.kind == lego.TicketOrder {
				label = "Order ID (see Set Workshop → Parts orders)"
			}
			return []ui.Field{{Label: label}}
		},
		submit: func(app *App, values []string) {
			if app.assign == nil {
				app.onBack()
				return
			}
			target := values[0]
			label := target
			if app.assign.kind == lego.TicketCheck {
				target = catalogSetNum(target)
				label = target
				if s, err := app.legoDB.GetSetByNum(target); err == nil && s != nil && s.Name != "" {
					label = target + " " + s.Name
				}
			} else if id, err := strconv.ParseInt(target, 10, 64); err == nil {
				if o, err := app.legoDB.GetOrder(id); err == nil && o != nil {
					label = fmt.Sprintf("Order #%d (%s %s)", id, o.SupplierKind, o.Supplier)
				}
			}
			app.assign.target, app.assign.label = target, label
			startPick(app, assignWhoPick(app))
		},
	}
}

// assignWhoPick offers every known user plus "leave open for anyone".
func assignWhoPick(app *App) *pickState {
	rows, _ := app.users.ListAll(app.ctx())
	items := []pickItem{{Key: "", Label: "(open queue — first qualified person to claim it)"}}
	seen := map[string]bool{}
	for _, r := range rows {
		if r.Username == "" || seen[r.Username] {
			continue
		}
		seen[r.Username] = true
		items = append(items, pickItem{Key: r.Username, Label: r.Username + "  (id " + r.ID + ", " + r.RoleOrGroup + ")"})
	}
	return &pickState{
		Header: "Assign " + app.assign.label + " to:",
		Prompt: "Choose",
		Items:  items,
		OnPick: func(app *App, it pickItem) {
			tk, err := app.legoDB.AssignTicket(app.assign.kind, app.assign.target, app.assign.label, it.Key, "", "", app.userName())
			app.assign = nil
			if err != nil {
				app.setMsg(err.Error(), true)
				app.onBack()
				return
			}
			app.audit.Log(app.userName(), "", "TICKET_ASSIGNED", "SUCCESS", fmt.Sprintf("%s -> %s (%s)", tk.Label, orDash(it.Key), tk.Kind))
			who := it.Key
			if who == "" {
				who = "the open queue"
			}
			if it.Key != "" {
				_ = app.legoDB.SendMessage(app.userName(), it.Key, "You've been assigned: "+tk.Label)
			}
			app.setMsg("Assigned "+tk.Label+" to "+who+".", false)
			app.stack = nil
			app.cur = scrAdminHub
		},
	}
}

func jobQueueScreen() screenModel {
	return &tableScreen{
		panelID: "JOBQUE",
		title:   "Open Assignments",
		columns: []string{"Kind", "Target", "Assigned To", "Status", "Created"},
		fetch: func(app *App) ([][]string, string, error) {
			ts, err := app.legoDB.AllOpenTickets()
			rows := make([][]string, len(ts))
			for i, t := range ts {
				who := orDash(t.AssignedTo)
				if t.AssignedTo == "" {
					who = "(open queue)"
				}
				rows[i] = []string{t.Kind, t.Label, who, t.Status, t.CreatedAt.Format("Jan 2 15:04")}
			}
			return rows, fmt.Sprintf("%d open assignment(s)", len(rows)), err
		},
	}
}

// ---- Leave this job: save / finish / abandon ----

func quitJobScreen() screenModel {
	return &menuScreen{
		panelID: "QUITJB",
		title:   "Leave this job?",
		options: func(app *App) []menuOption {
			return []menuOption{
				{Key: "1", Label: "Save and come back later", Go: quitSaveAndLeave},
				{Key: "2", Label: "Finish now and see the summary", Go: quitFinishNow},
				{Key: "3", Label: "Abandon without saving (needs admin sign-off)", Go: func(app *App) { app.goTo(scrAbandonAuth) }},
				{Key: "0", Label: "Cancel — keep working on it", Go: func(app *App) { app.onBack() }},
			}
		},
	}
}

// quitSaveAndLeave is the check/order screen's own existing "leave" behavior — the
// ticket stays claimed and reserved for this person (see access-control docs).
func quitSaveAndLeave(app *App) {
	if app.checking != nil {
		app.setMsg("Check not saved (S saves it for later, F finishes).", false)
		app.checking = nil
	}
	app.stack = nil
	app.cur = app.landingScreen()
}

func quitFinishNow(app *App) {
	if app.checking != nil {
		app.stack = nil
		app.cur = scrSetCheck
		finishCheck(app)
		return
	}
	// An order has no single "finish" action of its own — mark it received.
	if app.ws().orderID > 0 {
		app.stack = nil
		app.cur = scrOrderLines
		setOrderStatus(app, app.ws().orderID, "received")
	}
}

func abandonAuthScreen() screenModel {
	return &formScreen{
		panelID: "ABNAUT",
		title:   "Admin sign-off needed",
		preamble: func(app *App) string {
			return app.theme.Muted.Render("Abandoning without saving discards this attempt's progress entirely. An admin has to confirm it.")
		},
		build: func(app *App) []ui.Field {
			return []ui.Field{{Label: "Admin username"}, {Label: "Admin password", Password: true}}
		},
		submit: func(app *App, values []string) {
			if !verifyAdmin(app, values[0], values[1]) {
				app.setMsg("That doesn't check out as an admin sign-in.", true)
				return
			}
			app.abandonAdmin = values[0]
			startAbandonWhy(app)
		},
	}
}

var abandonReasons = []pickItem{
	{Key: "messed_up", Label: "Messed up and want to redo"},
	{Key: "reassigned", Label: "Been assigned a more important task"},
}

func startAbandonWhy(app *App) {
	startPick(app, &pickState{
		Header:    "Why are you abandoning this?",
		Prompt:    "Choose, or type your own reason",
		Items:     abandonReasons,
		AllowFree: true,
		FreeHint:  "your own reason",
		OnPick:    func(app *App, it pickItem) { finishAbandon(app, it.Label) },
		OnFree:    func(app *App, text string) { finishAbandon(app, text) },
	})
}

func finishAbandon(app *App, reason string) {
	user, role := app.userName(), ""
	if app.session != nil {
		role = app.session.Role
	}
	ticketID := app.currentTicketID
	if app.checking != nil {
		_ = app.legoDB.DeleteDraftCheck(app.checking.check.ID)
		app.checking = nil
	}
	if ticketID != 0 {
		_ = app.legoDB.AbandonTicket(ticketID, user)
	}
	app.currentTicketID = 0
	app.audit.Log(user, role, "TICKET_ABANDONED", "SUCCESS", fmt.Sprintf("by %s (admin sign-off: %s): %s", user, app.abandonAdmin, reason))
	_ = app.legoDB.SendMessage(user, app.abandonAdmin, user+" abandoned a job (admin sign-off given): "+reason)
	app.abandonAdmin = ""
	app.stack = nil
	app.cur = app.landingScreen()
	app.setMsg("Abandoned. Back on your own screen — this one's back in the open queue.", false)
}

// verifyAdmin re-authenticates a separate admin identity for the abandon
// sign-off — a real password check, not just "type any admin's name".
func verifyAdmin(app *App, username, password string) bool {
	session, err := auth.AuthenticateUser(app.ctx(), app.wms, app.pdb, username, password)
	if err != nil || session == nil {
		return false
	}
	if session.Permissions != nil && session.Permissions.IsAdmin {
		return true
	}
	if p, err := access.Load(); err == nil {
		if p.Effective(session.Source, session.Username).Can("access.manage") {
			return true
		}
	}
	return false
}

// ---- Scan-to-claim ----

func scanClaimScreen() screenModel {
	return &formScreen{
		panelID: "SCNCLM",
		title:   "Scan a job ticket",
		build: func(app *App) []ui.Field {
			return []ui.Field{{Label: "Scan or type the ticket's barcode"}}
		},
		submit: func(app *App, values []string) {
			t, err := app.legoDB.TicketByToken(values[0])
			if err != nil || t == nil {
				app.setMsg("That ticket wasn't found or is no longer open.", true)
				return
			}
			if t.AssignedTo == "" {
				if err := app.legoDB.ClaimTicket(t.ID, app.userName()); err != nil {
					app.setMsg(err.Error(), true)
					return
				}
				t.AssignedTo = app.userName()
			} else if t.AssignedTo != app.userName() {
				app.setMsg("That ticket is assigned to someone else.", true)
				return
			}
			app.stack = nil
			openTicket(app, t)
		},
	}
}
