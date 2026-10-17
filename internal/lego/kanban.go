package lego

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

// kanbanQtyThreshold is the per-line quantity at or above which a check line
// gets flagged "moved to the back" (see CheckLine.Deprioritized, saveCheck).
// Admin-tunable via config.KanbanBacklogQtyThreshold; falls back to 50 for
// anything unset or not a positive number, same convention as this project's
// other numeric settings (e.g. config.IdleLockMinutes).
func kanbanQtyThreshold() int {
	if n, err := strconv.Atoi(strings.TrimSpace(config.Get(config.KanbanBacklogQtyThreshold))); err == nil && n > 0 {
		return n
	}
	return 50
}

// Kanban board columns -- derived from a ticket's status (or a backlog set
// with no ticket yet), never stored directly: see DB.KanbanBoard/KanbanBoardAll.
const (
	KanbanColBacklog = "backlog"
	KanbanColActive  = "active"
	KanbanColDone    = "done"
)

// KanbanNote is a checker/picker's own free-text note on a job ticket, and
// optionally a specific part+colour line or a specific bag within it.
// Append-only, like this project's other work-record data -- there is no
// update/delete path, a correction is a follow-up note, not an edit.
type KanbanNote struct {
	ID                   int64
	TicketID             int64
	PartNum              string
	ColorID              int
	BagID                int64
	Body, Visibility     string // visibility: "private" or "shared"
	CreatedBy, CreatedAt string
}

// AddKanbanNote records a new note against ticketID, optionally scoped to a
// part+colour line (partNum != "") or a bag (bagID != 0) within it. visibility
// anything other than "shared" is stored as "private".
func (d *DB) AddKanbanNote(ticketID int64, partNum string, colorID int, bagID int64, body, visibility, by string) (*KanbanNote, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, errors.New("a note needs some text")
	}
	if visibility != "shared" {
		visibility = "private"
	}
	now := time.Now().UTC().Format(timeLayout)
	res, err := d.Exec(`INSERT INTO kanban_notes (ticket_id, part_num, color_id, bag_id, body, visibility, created_by, created_at)
		VALUES (?,?,?,?,?,?,?,?)`, ticketID, partNum, colorID, bagID, body, visibility, by, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &KanbanNote{ID: id, TicketID: ticketID, PartNum: partNum, ColorID: colorID, BagID: bagID,
		Body: body, Visibility: visibility, CreatedBy: by, CreatedAt: now}, nil
}

// KanbanNotes lists ticketID's notes, newest first. A non-admin (forAdmin
// false) only ever sees their own notes plus anything marked shared -- never
// another user's private note.
func (d *DB) KanbanNotes(ticketID int64, forUser string, forAdmin bool) ([]KanbanNote, error) {
	q := `SELECT id, ticket_id, part_num, color_id, bag_id, body, visibility, created_by, created_at FROM kanban_notes WHERE ticket_id = ?`
	args := []any{ticketID}
	if !forAdmin {
		q += ` AND (created_by = ? OR visibility = 'shared')`
		args = append(args, forUser)
	}
	q += ` ORDER BY id DESC`
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []KanbanNote
	for rows.Next() {
		var n KanbanNote
		if err := rows.Scan(&n.ID, &n.TicketID, &n.PartNum, &n.ColorID, &n.BagID, &n.Body, &n.Visibility, &n.CreatedBy, &n.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// TouchKanbanColumn records that ticketID just entered column -- the board's
// card-aging indicator (see KanbanColumnAge). A no-op if it's already marked
// as being in that column, so repeated calls from an unchanged card don't
// reset its age.
func (d *DB) TouchKanbanColumn(ticketID int64, column string) error {
	_, err := d.Exec(`INSERT INTO kanban_card_moves (ticket_id, column_name, entered_at) VALUES (?,?,?)
		ON CONFLICT(ticket_id) DO UPDATE SET column_name = excluded.column_name, entered_at = excluded.entered_at
		WHERE kanban_card_moves.column_name != excluded.column_name`,
		ticketID, column, time.Now().UTC().Format(timeLayout))
	return err
}

// KanbanColumnAge is how long ticketID has sat in its current column, zero if
// never recorded (e.g. a ticket older than this feature).
func (d *DB) KanbanColumnAge(ticketID int64) (time.Duration, error) {
	var enteredAt string
	err := d.QueryRow(`SELECT entered_at FROM kanban_card_moves WHERE ticket_id = ?`, ticketID).Scan(&enteredAt)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	t, err := time.Parse(timeLayout, enteredAt)
	if err != nil {
		return 0, nil
	}
	return time.Since(t), nil
}

// KanbanCard is one board card -- either a ticket (a pick or a check, active/
// done/abandoned) or a backlog set with no ticket yet.
type KanbanCard struct {
	Ticket    Ticket
	Column    string
	Lane      string // Ticket.Kind: TicketCheck or TicketOrder
	Summary   string // "M3 E1" style for a check, "" when not applicable
	Age       time.Duration
	Abandoned bool
}

// kanbanSummary is a check ticket's missing/extra badge, matching the table
// view's own "M3 E1" convention (see internal/uiapp/set_check.go). "" for a
// pick ticket, or a check with nothing to report yet.
func (d *DB) kanbanSummary(t Ticket) string {
	if t.Kind != TicketCheck {
		return ""
	}
	c, err := d.CurrentOrLastCheck(t.Target)
	if err != nil || c == nil {
		return ""
	}
	_, _, missing, extra, _ := c.Totals()
	if missing == 0 && extra == 0 {
		return "clean"
	}
	var parts []string
	if missing > 0 {
		parts = append(parts, fmt.Sprintf("M%d", missing))
	}
	if extra > 0 {
		parts = append(parts, fmt.Sprintf("E%d", extra))
	}
	return strings.Join(parts, " ")
}

func (d *DB) buildCard(t Ticket) KanbanCard {
	col, abandoned := KanbanColActive, false
	switch t.Status {
	case TicketDone:
		col = KanbanColDone
	case TicketAbandoned:
		col, abandoned = KanbanColBacklog, true
	}
	age, _ := d.KanbanColumnAge(t.ID)
	return KanbanCard{Ticket: t, Column: col, Lane: t.Kind, Summary: d.kanbanSummary(t), Age: age, Abandoned: abandoned}
}

// backlogCard is a set still queued in the kanban backlog -- it has no ticket
// yet (a ticket only exists once an admin assigns the check), so this
// synthesizes a minimal stand-in Ticket carrying just the set number, for a
// uniform KanbanCard shape.
func backlogCard(s SetState) KanbanCard {
	return KanbanCard{
		Ticket: Ticket{Kind: TicketCheck, Target: s.SetNum, Label: s.SetNum, Status: BacklogQueued},
		Column: KanbanColBacklog, Lane: TicketCheck,
	}
}

// KanbanBoard is forUser's own kanban cards: their current job (if any, at
// most one by design), their recent finished jobs, and the shared backlog
// list (visible to everyone, not actionable until an admin releases it --
// see DB.SetBacklogStatus). Never another user's cards.
func (d *DB) KanbanBoard(forUser string) ([]KanbanCard, error) {
	var out []KanbanCard

	backlog, err := d.BacklogSets()
	if err != nil {
		return nil, err
	}
	for _, s := range backlog {
		out = append(out, backlogCard(s))
	}

	cur, err := d.CurrentTicket(forUser)
	if err != nil {
		return nil, err
	}
	if cur != nil {
		out = append(out, d.buildCard(*cur))
	}

	done, err := d.TicketHistory(forUser, 10)
	if err != nil {
		return nil, err
	}
	for _, t := range done {
		out = append(out, d.buildCard(t))
	}
	return out, nil
}

// KanbanBoardAll is the admin board: the shared backlog, every open (queued
// or claimed) ticket across every user, recently abandoned tickets (folded
// into the Backlog column with an Abandoned badge -- no separate 4th column),
// and recently finished work.
func (d *DB) KanbanBoardAll() ([]KanbanCard, error) {
	var out []KanbanCard

	backlog, err := d.BacklogSets()
	if err != nil {
		return nil, err
	}
	for _, s := range backlog {
		out = append(out, backlogCard(s))
	}

	open, err := d.AllOpenTickets()
	if err != nil {
		return nil, err
	}
	for _, t := range open {
		out = append(out, d.buildCard(t))
	}

	abandoned, err := d.recentTickets(TicketAbandoned, 25)
	if err != nil {
		return nil, err
	}
	for _, t := range abandoned {
		out = append(out, d.buildCard(t))
	}

	done, err := d.recentTickets(TicketDone, 25)
	if err != nil {
		return nil, err
	}
	for _, t := range done {
		out = append(out, d.buildCard(t))
	}
	return out, nil
}

// recentTickets is every ticket in status across every user, most recently
// created first -- the admin board's Done/Abandoned columns, which (unlike
// TicketHistory) aren't scoped to one user.
func (d *DB) recentTickets(status string, limit int) ([]Ticket, error) {
	rows, err := d.Query(`SELECT id, kind, target, label, assigned_to, status, priority, note, token, created_by, created_at, claimed_at, done_at
		FROM job_tickets WHERE status = ? ORDER BY id DESC LIMIT ?`, status, limit)
	if err != nil {
		return nil, err
	}
	return scanTickets(rows)
}
