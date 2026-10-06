package lego

import (
	"database/sql"
	"errors"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

// ErrAlreadyClockedIn/ErrNotClockedIn are ClockIn/ClockOut's specific
// failures — distinct from a generic query error so callers (CLI/TUI/mobile)
// can show "you're already clocked in" rather than a bare error string.
var (
	ErrAlreadyClockedIn = errors.New("already clocked in")
	ErrNotClockedIn     = errors.New("not clocked in")
	// ErrNotScheduled is RequireClockedIn's second failure mode: clocked in,
	// but today isn't on the rota and nobody's given a quick NS override.
	ErrNotScheduled = errors.New("not scheduled to work today")
)

// ClockEvent is one clock-in, with or without a matching clock-out yet.
type ClockEvent struct {
	ID                    int64
	Username              string
	ClockInAt, ClockOutAt time.Time // ClockOutAt is the zero value while still clocked in
}

func (e ClockEvent) Open() bool { return e.ClockOutAt.IsZero() }

// ClockIn starts a new open clock event for username — an error
// (ErrAlreadyClockedIn) if one is already open, rather than silently
// layering a second one.
func (d *DB) ClockIn(username string) (*ClockEvent, error) {
	in, err := d.IsClockedIn(username)
	if err != nil {
		return nil, err
	}
	if in {
		return nil, ErrAlreadyClockedIn
	}
	now := time.Now()
	res, err := d.Exec(`INSERT INTO clock_events (username, clock_in_at, clock_out_at) VALUES (?, ?, '')`,
		username, now.Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &ClockEvent{ID: id, Username: username, ClockInAt: now}, nil
}

// ClockOut closes username's open clock event, if any (ErrNotClockedIn if not).
func (d *DB) ClockOut(username string) error {
	res, err := d.Exec(`UPDATE clock_events SET clock_out_at = ? WHERE username = ? AND clock_out_at = ''
		AND id = (SELECT id FROM clock_events WHERE username = ? AND clock_out_at = '' ORDER BY id DESC LIMIT 1)`,
		time.Now().Format(time.RFC3339), username, username)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotClockedIn
	}
	return nil
}

// IsClockedIn reports whether username has an open clock event right now.
func (d *DB) IsClockedIn(username string) (bool, error) {
	var n int
	err := d.QueryRow(`SELECT COUNT(*) FROM clock_events WHERE username = ? AND clock_out_at = ''`, username).Scan(&n)
	return n > 0, err
}

// CurrentShift is username's open clock event, or nil if they're not clocked in.
func (d *DB) CurrentShift(username string) (*ClockEvent, error) {
	var e ClockEvent
	var in string
	err := d.QueryRow(`SELECT id, clock_in_at FROM clock_events WHERE username = ? AND clock_out_at = '' ORDER BY id DESC LIMIT 1`,
		username).Scan(&e.ID, &in)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	e.Username = username
	e.ClockInAt, _ = time.Parse(time.RFC3339, in)
	return &e, nil
}

// RotaEntry is one user's schedule for one date.
type RotaEntry struct {
	Username, Date, StartTime, EndTime, Note string
	EmergencyOverride                        bool
	CreatedBy                                string
	CreatedAt                                time.Time
}

// SetRota schedules username to work date (upserting any existing entry for
// that day) — start/end are free text ("09:00"), shown as-is, never parsed.
func (d *DB) SetRota(username, date, start, end, note, createdBy string) error {
	_, err := d.Exec(`INSERT INTO rota_entries (username, date, start_time, end_time, note, emergency_override, created_by, created_at)
		VALUES (?,?,?,?,?,0,?,?)
		ON CONFLICT(username, date) DO UPDATE SET
			start_time = excluded.start_time, end_time = excluded.end_time, note = excluded.note,
			emergency_override = 0, created_by = excluded.created_by, created_at = excluded.created_at`,
		username, date, start, end, note, createdBy, time.Now().Format(time.RFC3339))
	return err
}

// ClearRota removes username's schedule for date, if any.
func (d *DB) ClearRota(username, date string) error {
	_, err := d.Exec(`DELETE FROM rota_entries WHERE username = ? AND date = ?`, username, date)
	return err
}

// QuickNSOverride lets username work date even though they're not on the
// rota for it — emergency cover, a quick admin action rather than planning
// a shift. A no-op (not an error) if they're already scheduled that day.
func (d *DB) QuickNSOverride(username, date, createdBy string) error {
	_, err := d.Exec(`INSERT INTO rota_entries (username, date, note, emergency_override, created_by, created_at)
		VALUES (?, ?, 'Emergency cover', 1, ?, ?)
		ON CONFLICT(username, date) DO NOTHING`,
		username, date, createdBy, time.Now().Format(time.RFC3339))
	return err
}

// GetRota returns username's rota entry for date, or nil if they're not scheduled.
func (d *DB) GetRota(username, date string) (*RotaEntry, error) {
	var e RotaEntry
	var createdAt string
	err := d.QueryRow(`SELECT username, date, start_time, end_time, note, emergency_override, created_by, created_at
		FROM rota_entries WHERE username = ? AND date = ?`, username, date).
		Scan(&e.Username, &e.Date, &e.StartTime, &e.EndTime, &e.Note, &e.EmergencyOverride, &e.CreatedBy, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	e.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return &e, nil
}

// IsScheduled reports whether username is on the rota for date (including a
// quick NS override — both just mean "allowed to work today" to this check;
// GetRota's EmergencyOverride flag is what tells them apart for the record).
func (d *DB) IsScheduled(username, date string) (bool, error) {
	var n int
	err := d.QueryRow(`SELECT COUNT(*) FROM rota_entries WHERE username = ? AND date = ?`, username, date).Scan(&n)
	return n > 0, err
}

// RotaForDate is every username scheduled on date, for the weekly rota view
// ("who's doing what, when" — sorted by start time, unscheduled-time entries last).
func (d *DB) RotaForDate(date string) ([]RotaEntry, error) {
	rows, err := d.Query(`SELECT username, date, start_time, end_time, note, emergency_override, created_by, created_at
		FROM rota_entries WHERE date = ? ORDER BY (start_time = '') ASC, start_time ASC, username ASC`, date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RotaEntry
	for rows.Next() {
		var e RotaEntry
		var createdAt string
		if err := rows.Scan(&e.Username, &e.Date, &e.StartTime, &e.EndTime, &e.Note, &e.EmergencyOverride, &e.CreatedBy, &createdAt); err != nil {
			return nil, err
		}
		e.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		out = append(out, e)
	}
	return out, rows.Err()
}

// RequireClockedIn is the gate for starting or continuing pick/check work:
// clocked in, AND (scheduled today OR a quick NS override covers today).
// Viewing things (history, messages, the handover note) is never gated by
// this — only actually claiming or advancing a pick/check is. A no-op
// (config.RequireClockIn unset) until an admin turns it on — see that
// constant's doc comment for why this isn't on by default.
func (d *DB) RequireClockedIn(username string) error {
	if config.Get(config.RequireClockIn) != "1" {
		return nil
	}
	in, err := d.IsClockedIn(username)
	if err != nil {
		return err
	}
	if !in {
		return ErrNotClockedIn
	}
	sched, err := d.IsScheduled(username, today())
	if err != nil {
		return err
	}
	if !sched {
		return ErrNotScheduled
	}
	return nil
}
