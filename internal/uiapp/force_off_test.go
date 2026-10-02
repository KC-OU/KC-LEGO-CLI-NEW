package uiapp

import (
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

// TestFinishForceOffReleasesLogsMessagesAndAudits drives the whole "Force off
// a job" admin action (short of the two picker steps, which only gather
// app.forceOff's fields — see forceOffKeys/forceOffTargetPick) and checks
// every side effect: the ticket itself, the activity feed, the reassurance
// message, and the audit trail. Never touches accuracy.
func TestFinishForceOffReleasesLogsMessagesAndAudits(t *testing.T) {
	app, _ := newTestEnv(t)
	tk, err := app.legoDB.AssignTicket(lego.TicketCheck, "75192-1", "75192-1 Falcon", "", "", "", app.userName())
	if err != nil {
		t.Fatal(err)
	}
	if err := app.legoDB.ClaimTicket(tk.ID, "dave"); err != nil {
		t.Fatal(err)
	}

	app.forceOff = &forceOffDraft{ticketID: tk.ID, label: tk.Label, from: "dave"}
	finishForceOff(app, "We've assigned you another task — don't worry, your accuracy won't be affected.")

	if app.forceOff != nil {
		t.Error("the draft should be cleared once the action completes")
	}
	if app.cur != scrAdminHub {
		t.Errorf("cur after force-off = %q, want back on the admin hub", app.cur)
	}

	open, err := app.legoDB.OpenTickets(lego.TicketCheck, "sam")
	if err != nil || len(open) != 1 || open[0].AssignedTo != "" {
		t.Fatalf("ticket should be back in the open queue: %+v, %v", open, err)
	}

	evs, err := app.legoDB.AdminEvents(10)
	if err != nil || len(evs) != 1 || evs[0].Kind != lego.EventForcedOff || evs[0].Target != "dave" {
		t.Fatalf("AdminEvents = %+v, %v, want one forced_off event targeting dave", evs, err)
	}

	msgs, err := app.legoDB.UndeliveredMessages("dave")
	if err != nil || len(msgs) != 1 || msgs[0].Body == "" {
		t.Fatalf("dave should have the reassurance message waiting: %+v, %v", msgs, err)
	}

	if !auditHas(t, "TICKET_FORCED_OFF") {
		t.Error("force-off must be audited")
	}
}

// TestFinishForceOffReassignSendsToNamedPerson covers the "straight to a
// named person" destination instead of the open queue.
func TestFinishForceOffReassignSendsToNamedPerson(t *testing.T) {
	app, _ := newTestEnv(t)
	tk, err := app.legoDB.AssignTicket(lego.TicketOrder, "42", "Order #42", "", "", "", app.userName())
	if err != nil {
		t.Fatal(err)
	}
	if err := app.legoDB.ClaimTicket(tk.ID, "dave"); err != nil {
		t.Fatal(err)
	}

	app.forceOff = &forceOffDraft{ticketID: tk.ID, label: tk.Label, from: "dave", to: "pat"}
	finishForceOff(app, "reassigned")

	open, err := app.legoDB.OpenTickets(lego.TicketOrder, "pat")
	if err != nil || len(open) != 1 || open[0].AssignedTo != "pat" {
		t.Fatalf("ticket should be sent straight to pat: %+v, %v", open, err)
	}
	evs, err := app.legoDB.AdminEvents(10)
	if err != nil || len(evs) != 1 || evs[0].Kind != lego.EventReassigned {
		t.Fatalf("AdminEvents = %+v, %v, want a reassigned event", evs, err)
	}
}

// TestCheckTicketStillMineNoticesAForceOff is the ~15s idle-tick side: a
// session mid-check whose ticket was force-taken by an admin (a separate
// process, so this session only finds out on the next poll) must be bounced
// back to its own hub with local state cleared — the draft itself, in the
// database, is left exactly as it was.
func TestCheckTicketStillMineNoticesAForceOff(t *testing.T) {
	app, _ := newTestEnv(t)
	tk, err := app.legoDB.AssignTicket(lego.TicketCheck, "75192-1", "Falcon", "", "", "", "someone-else")
	if err != nil {
		t.Fatal(err)
	}
	if err := app.legoDB.ClaimTicket(tk.ID, app.userName()); err != nil {
		t.Fatal(err)
	}
	app.currentTicketID = tk.ID
	app.checking = &checkState{}
	app.cur, app.stack = scrSetCheck, []string{scrPickerHub}

	// A separate admin session forces it off, same as finishForceOff would.
	if err := app.legoDB.ForceOffTicket(tk.ID, ""); err != nil {
		t.Fatal(err)
	}

	app.checkTicketStillMine()

	if app.currentTicketID != 0 {
		t.Error("currentTicketID should be cleared")
	}
	if app.checking != nil {
		t.Error("the local draft pointer should be cleared (the database draft is untouched)")
	}
	if app.cur != app.landingScreen() {
		t.Errorf("cur after a force-off = %q, want the landing screen", app.cur)
	}
}

// TestCheckTicketStillMineIsANoopWhenNothingChanged guards against a false
// positive on every idle tick while a job is legitimately still yours.
func TestCheckTicketStillMineIsANoopWhenNothingChanged(t *testing.T) {
	app, _ := newTestEnv(t)
	tk, err := app.legoDB.AssignTicket(lego.TicketCheck, "75192-1", "Falcon", "", "", "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := app.legoDB.ClaimTicket(tk.ID, app.userName()); err != nil {
		t.Fatal(err)
	}
	app.currentTicketID = tk.ID
	app.checking = &checkState{}
	app.cur = scrSetCheck

	app.checkTicketStillMine()

	if app.currentTicketID != tk.ID || app.checking == nil || app.cur != scrSetCheck {
		t.Error("nothing should change while the ticket is still this session's own")
	}
}

func TestAdminEventsScreenListsRecentEvents(t *testing.T) {
	app, _ := newTestEnv(t)
	if err := app.legoDB.LogEvent(lego.EventMessage, "admin", "sam", "hi"); err != nil {
		t.Fatal(err)
	}
	if err := app.legoDB.LogEvent(lego.EventForcedOff, "admin", "dave", "75192-1"); err != nil {
		t.Fatal(err)
	}
	scr := adminEventsScreen().(*tableScreen)
	rows, title, err := scr.fetch(app)
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows = %v, title = %q, err = %v, want 2 rows", rows, title, err)
	}
	if rows[0][1] != lego.EventForcedOff { // newest first
		t.Errorf("first row kind = %q, want %q", rows[0][1], lego.EventForcedOff)
	}
}
