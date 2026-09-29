package lego

import (
	"crypto/rand"
	"database/sql"
	"encoding/base32"
	"errors"
	"strings"
	"time"
)

// A job ticket is assigned work: an admin points a picker at an order or a checker
// at a set, by name or left open for whoever claims it first. Claiming a ticket
// doesn't touch the check/order itself — it only means the underlying screen (a
// normal set check, a normal order) opens as it always has; this table is purely
// the queue and who's on what.
const (
	TicketCheck = "check" // target is a set_num
	TicketOrder = "order" // target is an order id, as text

	TicketQueued    = "queued"
	TicketClaimed   = "claimed"
	TicketDone      = "done"
	TicketAbandoned = "abandoned"
)

type Ticket struct {
	ID                                 int64
	Kind, Target, Label                string
	AssignedTo, Status, Priority, Note string
	Token                              string
	CreatedBy                          string
	CreatedAt, ClaimedAt, DoneAt       time.Time
}

func newToken() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)), nil
}

// AssignTicket creates a new ticket. assignedTo "" leaves it in the open queue for
// anyone who can claim that kind of work.
func (d *DB) AssignTicket(kind, target, label, assignedTo, priority, note, createdBy string) (*Ticket, error) {
	if kind != TicketCheck && kind != TicketOrder {
		return nil, errors.New("kind must be check or order")
	}
	token, err := newToken()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	res, err := d.Exec(`INSERT INTO job_tickets (kind, target, label, assigned_to, status, priority, note, token, created_by, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		kind, target, label, assignedTo, TicketQueued, priority, note, token, createdBy, now.Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &Ticket{ID: id, Kind: kind, Target: target, Label: label, AssignedTo: assignedTo, Status: TicketQueued,
		Priority: priority, Note: note, Token: token, CreatedBy: createdBy, CreatedAt: now}, nil
}

// OpenTickets lists tickets a user could see in "Request": queued and either open
// (assigned_to "") or already assigned to them, for the given kind.
func (d *DB) OpenTickets(kind, forUser string) ([]Ticket, error) {
	rows, err := d.Query(`SELECT id, kind, target, label, assigned_to, status, priority, note, token, created_by, created_at, claimed_at, done_at
		FROM job_tickets WHERE kind = ? AND status = ? AND (assigned_to = '' OR assigned_to = ?) ORDER BY id`, kind, TicketQueued, forUser)
	if err != nil {
		return nil, err
	}
	return scanTickets(rows)
}

// CurrentTicket is whatever forUser has claimed and not finished — at most one, by
// design (see "one job at a time" in the picker/checker workflow).
func (d *DB) CurrentTicket(forUser string) (*Ticket, error) {
	rows, err := d.Query(`SELECT id, kind, target, label, assigned_to, status, priority, note, token, created_by, created_at, claimed_at, done_at
		FROM job_tickets WHERE assigned_to = ? AND status = ? ORDER BY claimed_at LIMIT 1`, forUser, TicketClaimed)
	if err != nil {
		return nil, err
	}
	ts, err := scanTickets(rows)
	if err != nil || len(ts) == 0 {
		return nil, err
	}
	return &ts[0], nil
}

// AllOpenTickets is every queued or claimed ticket, for the admin queue screen.
func (d *DB) AllOpenTickets() ([]Ticket, error) {
	rows, err := d.Query(`SELECT id, kind, target, label, assigned_to, status, priority, note, token, created_by, created_at, claimed_at, done_at
		FROM job_tickets WHERE status IN (?, ?) ORDER BY id`, TicketQueued, TicketClaimed)
	if err != nil {
		return nil, err
	}
	return scanTickets(rows)
}

func scanTickets(rows *sql.Rows) ([]Ticket, error) {
	defer rows.Close()
	var out []Ticket
	for rows.Next() {
		var t Ticket
		var claimedAt, doneAt string
		var createdAt string
		if err := rows.Scan(&t.ID, &t.Kind, &t.Target, &t.Label, &t.AssignedTo, &t.Status, &t.Priority, &t.Note, &t.Token,
			&t.CreatedBy, &createdAt, &claimedAt, &doneAt); err != nil {
			return nil, err
		}
		t.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		if claimedAt != "" {
			t.ClaimedAt, _ = time.Parse(time.RFC3339, claimedAt)
		}
		if doneAt != "" {
			t.DoneAt, _ = time.Parse(time.RFC3339, doneAt)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ClaimTicket assigns an open ticket to by (if it wasn't already named) and marks
// it claimed. Refuses a ticket already claimed by someone else.
func (d *DB) ClaimTicket(id int64, by string) error {
	res, err := d.Exec(`UPDATE job_tickets SET assigned_to = ?, status = ?, claimed_at = ?
		WHERE id = ? AND status = ? AND (assigned_to = '' OR assigned_to = ?)`,
		by, TicketClaimed, time.Now().Format(time.RFC3339), id, TicketQueued, by)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("that ticket isn't open to claim anymore")
	}
	return nil
}

// TicketByToken looks up an open ticket by its barcode claim token — scan-to-claim.
func (d *DB) TicketByToken(token string) (*Ticket, error) {
	rows, err := d.Query(`SELECT id, kind, target, label, assigned_to, status, priority, note, token, created_by, created_at, claimed_at, done_at
		FROM job_tickets WHERE token = ? AND status IN (?, ?)`, token, TicketQueued, TicketClaimed)
	if err != nil {
		return nil, err
	}
	ts, err := scanTickets(rows)
	if err != nil || len(ts) == 0 {
		return nil, err
	}
	return &ts[0], nil
}

// FinishTicket marks a claimed ticket done — called when the underlying check/order
// it points at actually completes.
func (d *DB) FinishTicket(kind, target, by string) error {
	_, err := d.Exec(`UPDATE job_tickets SET status = ?, done_at = ? WHERE kind = ? AND target = ? AND assigned_to = ? AND status = ?`,
		TicketDone, time.Now().Format(time.RFC3339), kind, target, by, TicketClaimed)
	return err
}

// AbandonTicket releases a claimed ticket back to the open queue (unassigned) —
// "abandon without saving": the underlying check/order's own progress is a
// separate decision (see set_check.go's abandon path), this only frees the ticket.
func (d *DB) AbandonTicket(id int64, by string) error {
	res, err := d.Exec(`UPDATE job_tickets SET status = ?, assigned_to = '', claimed_at = '' WHERE id = ? AND assigned_to = ? AND status = ?`,
		TicketQueued, id, by, TicketClaimed)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("no claimed ticket to abandon")
	}
	return nil
}
