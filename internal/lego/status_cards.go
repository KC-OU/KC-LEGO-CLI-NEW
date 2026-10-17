package lego

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Status cards: a quick, visible "why someone looks idle right now" so a
// supervisor doesn't have to guess (or get nagged by ticket_idle when the
// answer is already known) -- Delay (something outside the worker's
// control) or No Work Available (nothing queued for them), set by an admin
// on a worker's behalf. Clears itself the instant there's evidence the
// reason no longer applies: claiming a ticket (ClaimTicket) or clocking out
// (ClockOut).
//
// Break is deliberately NOT a status_cards row -- it's the existing paid-
// break feature (StartBreak/EndBreak, clock_breaks), reached through the
// same "scan a card" idea via ScanStatusCard so there's exactly one break
// concept in this app, not two that could drift apart. It's also the one
// reason a worker can set on themself, by scanning its card -- Delay/No
// Work are admin-applied only.

const (
	CardDelay  = "delay"
	CardNoWork = "no_work"
)

// cardBreakCode is the one self-serve card's fixed scannable code: a
// printed label carrying this exact barcode toggles the scanning user's
// own break on/off (see ScanStatusCard) -- no admin, no menu, just a scan.
const cardBreakCode = "CARD-BREAK"

// StatusCard is one admin-applied Delay/No-Work card.
type StatusCard struct {
	ID                 int64
	Username, Reason   string
	Note               string
	CreatedBy          string
	StartedAt, EndedAt time.Time // EndedAt zero = still active
}

func (c StatusCard) Active() bool { return c.EndedAt.IsZero() }

// CardReasonName is a reason key's display label, shared by every surface
// (CLI, TUI, mobile) so the wording can't drift between them.
func CardReasonName(reason string) string {
	switch reason {
	case CardDelay:
		return "Delay"
	case CardNoWork:
		return "No work available"
	default:
		return reason
	}
}

// SetStatusCard puts an admin-applied card on username, closing any card
// they already have active first -- one current status at a time. Caller
// enforces admin-only, same as every other admin action in this package.
func (d *DB) SetStatusCard(username, reason, note, by string) (*StatusCard, error) {
	username = strings.TrimSpace(username)
	reason = strings.TrimSpace(reason)
	if username == "" {
		return nil, errors.New("a username is required")
	}
	if reason != CardDelay && reason != CardNoWork {
		return nil, fmt.Errorf("%q isn't a valid card reason (delay or no_work)", reason)
	}
	if err := d.ClearStatusCard(username, by); err != nil {
		return nil, err
	}
	note = strings.TrimSpace(note)
	now := time.Now().UTC()
	res, err := d.Exec(`INSERT INTO status_cards (username, reason, note, created_by, started_at) VALUES (?,?,?,?,?)`,
		username, reason, note, by, now.Format(timeLayout))
	if err != nil {
		return nil, fmt.Errorf("setting a %s card on %s: %w", reason, username, err)
	}
	id, _ := res.LastInsertId()
	return &StatusCard{ID: id, Username: username, Reason: reason, Note: note, CreatedBy: by, StartedAt: now}, nil
}

// ClearStatusCard ends username's active card, if any -- a no-op (not an
// error) when there isn't one, since every auto-clear path (ClaimTicket,
// ClockOut) calls this unconditionally rather than checking first.
func (d *DB) ClearStatusCard(username, by string) error {
	_, err := d.Exec(`UPDATE status_cards SET ended_at = ? WHERE username = ? AND ended_at = ''`,
		time.Now().UTC().Format(timeLayout), username)
	return err
}

// ActiveStatusCard is username's current admin-applied card, nil if they
// don't have one (never set for a reason of "break" -- see OnBreak instead).
func (d *DB) ActiveStatusCard(username string) (*StatusCard, error) {
	var c StatusCard
	var started string
	err := d.QueryRow(`SELECT id, username, reason, note, created_by, started_at FROM status_cards
		WHERE username = ? AND ended_at = '' ORDER BY id DESC LIMIT 1`, username).
		Scan(&c.ID, &c.Username, &c.Reason, &c.Note, &c.CreatedBy, &started)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c.StartedAt, _ = time.Parse(timeLayout, started)
	return &c, nil
}

// ActiveStatusCards lists everyone with an admin-applied card on right now,
// oldest first -- the admin queue view's data.
func (d *DB) ActiveStatusCards() ([]StatusCard, error) {
	rows, err := d.Query(`SELECT id, username, reason, note, created_by, started_at FROM status_cards WHERE ended_at = '' ORDER BY started_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StatusCard
	for rows.Next() {
		var c StatusCard
		var started string
		if err := rows.Scan(&c.ID, &c.Username, &c.Reason, &c.Note, &c.CreatedBy, &started); err != nil {
			return nil, err
		}
		c.StartedAt, _ = time.Parse(timeLayout, started)
		out = append(out, c)
	}
	return out, rows.Err()
}

// CurrentStatus is a one-line, worker-facing label for whatever's currently
// explaining someone's idle time, "" if nothing is -- break always wins
// over an admin card, since an open break means neither delay nor no-work
// logically still applies. Used by the admin queue screen and the idle-
// nudge suppression below alike, so the wording and the logic can't drift
// apart from each other.
func (d *DB) CurrentStatus(username string) (string, error) {
	onBreak, err := d.OnBreak(username)
	if err != nil {
		return "", err
	}
	if onBreak {
		return "On a break", nil
	}
	c, err := d.ActiveStatusCard(username)
	if err != nil || c == nil {
		return "", err
	}
	return CardReasonName(c.Reason), nil
}

// HasActiveCard reports whether username currently has ANY reason active
// (on a break, or an admin-applied Delay/No Work card) -- what the
// supervisor idle-ticket nudge checks before nagging about someone whose
// idle time is already explained (see ticketWatch in cmd/wms/gateway.go).
func (d *DB) HasActiveCard(username string) (bool, error) {
	onBreak, err := d.OnBreak(username)
	if err != nil || onBreak {
		return onBreak, err
	}
	c, err := d.ActiveStatusCard(username)
	return c != nil, err
}

// ScanStatusCard resolves a scanned/typed card code for the scanning user
// themself -- today that's only the break card (cardBreakCode), toggling
// their own break via the existing StartBreak/EndBreak. Any other code is
// unknown here on purpose: Delay/No Work are admin-applied only (see
// SetStatusCard), never something a worker scans onto themself.
func (d *DB) ScanStatusCard(username, code string) (label string, err error) {
	if strings.TrimSpace(code) != cardBreakCode {
		return "", fmt.Errorf("unknown card code: %s", code)
	}
	onBreak, err := d.OnBreak(username)
	if err != nil {
		return "", err
	}
	if onBreak {
		if err := d.EndBreak(username); err != nil {
			return "", err
		}
		return "Break ended.", nil
	}
	if err := d.StartBreak(username); err != nil {
		return "", err
	}
	return "Break started.", nil
}
