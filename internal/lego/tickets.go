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

// ReopenTicket is "Admin: reopen a wrong check/order": a fresh ticket
// against the same set/order, left open for anyone to claim (claiming a
// check ticket for a set that's already been checked naturally becomes a
// recount — see loadSortedCheck/CheckRecount — so there's no separate
// "redo" code path to maintain). It's a thin wrapper over AssignTicket
// rather than new mechanics: the audit trail is a new row either way, which
// is the whole point — "this was redone" stays visible next to the
// original, not overwritten. reason is required (unlike a plain assign)
// so there's always a stated why next to it.
func (d *DB) ReopenTicket(kind, target, label, reason, createdBy string) (*Ticket, error) {
	if strings.TrimSpace(reason) == "" {
		return nil, errors.New("a reason is required to reopen a ticket")
	}
	return d.AssignTicket(kind, target, label, "", "", "reopened: "+reason, createdBy)
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

// TicketHistory is forUser's own past finished tickets, most recent first —
// the check/pick history screen's data. Deliberately just the ticket
// itself (what, when, how long): like EstimatePace, there's no reliable
// link from a ticket back to the specific set_checks/order row it produced
// (a set can be recounted many times), so outcome detail (missing/extra
// counts) isn't attempted here rather than guessed at.
func (d *DB) TicketHistory(forUser string, limit int) ([]Ticket, error) {
	rows, err := d.Query(`SELECT id, kind, target, label, assigned_to, status, priority, note, token, created_by, created_at, claimed_at, done_at
		FROM job_tickets WHERE assigned_to = ? AND status = ? ORDER BY done_at DESC LIMIT ?`, forUser, TicketDone, limit)
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

// TicketPace is how long tickets of a kind have recently taken, claim to
// done — "how long it will take", beside a ticket's own ClaimedAt for "how
// long it has taken" (see mobileapi's lineView and the admin ticket list).
// Deliberately a per-kind average, not per-line or per-piece: job_tickets
// has no link back to which specific set_checks/order row it produced (a
// set can be recounted many times over), so there's no robust join to
// normalize by size without guessing which check a ticket matches. A rough
// "a check like this usually takes about this long" is what's actually
// available; Samples says how much that's worth.
type TicketPace struct {
	AvgSeconds int64
	Samples    int
}

// EstimatePace averages claimed_at -> done_at over the most recent
// sampleSize finished tickets of kind. Samples 0 (a zero TicketPace) means
// no history yet — a fresh install, or nobody's finished one of this kind —
// callers show nothing rather than a meaningless estimate.
func (d *DB) EstimatePace(kind string, sampleSize int) (TicketPace, error) {
	rows, err := d.Query(`SELECT claimed_at, done_at FROM job_tickets WHERE kind = ? AND status = ? AND claimed_at != '' AND done_at != ''
		ORDER BY id DESC LIMIT ?`, kind, TicketDone, sampleSize)
	if err != nil {
		return TicketPace{}, err
	}
	defer rows.Close()
	var total float64
	var n int
	for rows.Next() {
		var claimedS, doneS string
		if err := rows.Scan(&claimedS, &doneS); err != nil {
			return TicketPace{}, err
		}
		claimed, err1 := time.Parse(time.RFC3339, claimedS)
		done, err2 := time.Parse(time.RFC3339, doneS)
		if err1 != nil || err2 != nil {
			continue
		}
		if dur := done.Sub(claimed); dur > 0 {
			total += dur.Seconds()
			n++
		}
	}
	if err := rows.Err(); err != nil {
		return TicketPace{}, err
	}
	if n == 0 {
		return TicketPace{}, nil
	}
	return TicketPace{AvgSeconds: int64(total / float64(n)), Samples: n}, nil
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

// ForceOffTicket takes a claimed ticket away from whoever has it — an admin
// action, not the holder's own choice, so unlike AbandonTicket there's no
// ownership check. reassignTo "" releases it to the open queue; otherwise it
// goes straight to that person, same as a fresh AssignTicket target. The
// underlying check/order draft is untouched either way (see openTicket).
func (d *DB) ForceOffTicket(id int64, reassignTo string) error {
	res, err := d.Exec(`UPDATE job_tickets SET status = ?, assigned_to = ?, claimed_at = '' WHERE id = ? AND status = ?`,
		TicketQueued, reassignTo, id, TicketClaimed)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("that ticket isn't claimed by anyone right now")
	}
	return nil
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
