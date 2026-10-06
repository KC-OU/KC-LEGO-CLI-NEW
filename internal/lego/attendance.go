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
	// ErrOnBreak is RequireClockedIn's third failure mode: clocked in and
	// scheduled, but a declared paid break is currently open.
	ErrOnBreak        = errors.New("on a break")
	ErrAlreadyOnBreak = errors.New("already on a break")
	ErrNotOnBreak     = errors.New("not on a break")
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
	Username          string    `json:"username"`
	Date              string    `json:"date"`
	StartTime         string    `json:"start_time"`
	EndTime           string    `json:"end_time"`
	Note              string    `json:"note"`
	EmergencyOverride bool      `json:"emergency_override"`
	CreatedBy         string    `json:"created_by"`
	CreatedAt         time.Time `json:"created_at"`
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
	onBreak, err := d.OnBreak(username)
	if err != nil {
		return err
	}
	if onBreak {
		return ErrOnBreak
	}
	return nil
}

// StartBreak opens a paid break within username's current shift
// (ErrNotClockedIn if they don't have one open, ErrAlreadyOnBreak if one's
// already running). RequireClockedIn refuses pick/check work while it's
// open — "accuracy/time-tracking pauses" means exactly that: no claimable
// work happens during a break, so nothing gets logged against them for it.
func (d *DB) StartBreak(username string) error {
	shift, err := d.CurrentShift(username)
	if err != nil {
		return err
	}
	if shift == nil {
		return ErrNotClockedIn
	}
	onBreak, err := d.OnBreak(username)
	if err != nil {
		return err
	}
	if onBreak {
		return ErrAlreadyOnBreak
	}
	_, err = d.Exec(`INSERT INTO clock_breaks (clock_event_id, start_at, end_at) VALUES (?, ?, '')`,
		shift.ID, time.Now().Format(time.RFC3339))
	return err
}

// EndBreak closes username's open break, if any (ErrNotOnBreak if there isn't one).
func (d *DB) EndBreak(username string) error {
	shift, err := d.CurrentShift(username)
	if err != nil {
		return err
	}
	if shift == nil {
		return ErrNotClockedIn
	}
	res, err := d.Exec(`UPDATE clock_breaks SET end_at = ? WHERE clock_event_id = ? AND end_at = ''`,
		time.Now().Format(time.RFC3339), shift.ID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotOnBreak
	}
	return nil
}

// OnBreak reports whether username's current shift (if any) has an open break.
func (d *DB) OnBreak(username string) (bool, error) {
	var n int
	err := d.QueryRow(`SELECT COUNT(*) FROM clock_breaks
		WHERE end_at = '' AND clock_event_id = (SELECT id FROM clock_events WHERE username = ? AND clock_out_at = '' ORDER BY id DESC LIMIT 1)`,
		username).Scan(&n)
	return n > 0, err
}

// NoShowCandidate is one rota entry whose start time has passed by more than
// the grace window with nobody clocked in for it yet.
type NoShowCandidate struct {
	Username, Date, StartTime string
}

// NoShowCandidates is today's rota entries that look like a no-show: a given
// start time, now at least graceMinutes past it, and still not clocked in.
// An entry with no start time (just "scheduled, whenever") never counts —
// there's nothing to be late against. Checked only while the clock-in gate
// is on (config.RequireClockIn) — see RequireClockedIn's own doc comment for
// why an empty/unused rota must never drive an alert.
func (d *DB) NoShowCandidates(graceMinutes int) ([]NoShowCandidate, error) {
	if config.Get(config.RequireClockIn) != "1" {
		return nil, nil
	}
	date := today()
	rows, err := d.Query(`SELECT username, start_time FROM rota_entries WHERE date = ? AND start_time != ''`, date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NoShowCandidate
	now := time.Now()
	for rows.Next() {
		var username, start string
		if err := rows.Scan(&username, &start); err != nil {
			return nil, err
		}
		startAt, err := time.ParseInLocation("2006-01-02 15:04", date+" "+start, time.Local)
		if err != nil {
			continue // an unparsable start_time (free text) never alerts
		}
		if now.Before(startAt.Add(time.Duration(graceMinutes) * time.Minute)) {
			continue
		}
		in, err := d.IsClockedIn(username)
		if err != nil {
			return nil, err
		}
		if !in {
			out = append(out, NoShowCandidate{Username: username, Date: date, StartTime: start})
		}
	}
	return out, rows.Err()
}

// TeamSummaryLine is one person's day: how long they were clocked in, and
// their accuracy if they did any picker/checker-scored work that day.
type TeamSummaryLine struct {
	Username        string
	HoursWorked     float64
	PickerAccuracy  *float64
	CheckerAccuracy *float64
}

// TeamSummary is every username with clock activity on date, for the
// daily/weekly team summary message — hours from clock_events, accuracy from
// AccuracyToday-equivalent history (nil when they had no scored activity
// that role that day, rather than a misleading 100%).
func (d *DB) TeamSummary(date string) ([]TeamSummaryLine, error) {
	rows, err := d.Query(`SELECT DISTINCT username FROM clock_events WHERE substr(clock_in_at, 1, 10) = ? ORDER BY username`, date)
	if err != nil {
		return nil, err
	}
	var usernames []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			rows.Close()
			return nil, err
		}
		usernames = append(usernames, u)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var out []TeamSummaryLine
	for _, u := range usernames {
		hours, err := d.hoursWorked(u, date)
		if err != nil {
			return nil, err
		}
		line := TeamSummaryLine{Username: u, HoursWorked: hours}
		if pct, hasAny, err := d.accuracyForDayIfAny(u, AccuracyPicker, date); err != nil {
			return nil, err
		} else if hasAny {
			line.PickerAccuracy = &pct
		}
		if pct, hasAny, err := d.accuracyForDayIfAny(u, AccuracyChecker, date); err != nil {
			return nil, err
		} else if hasAny {
			line.CheckerAccuracy = &pct
		}
		out = append(out, line)
	}
	return out, nil
}

func (d *DB) hoursWorked(username, date string) (float64, error) {
	rows, err := d.Query(`SELECT clock_in_at, clock_out_at FROM clock_events WHERE username = ? AND substr(clock_in_at, 1, 10) = ?`, username, date)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var total time.Duration
	now := time.Now()
	for rows.Next() {
		var in, out string
		if err := rows.Scan(&in, &out); err != nil {
			return 0, err
		}
		inAt, err := time.Parse(time.RFC3339, in)
		if err != nil {
			continue
		}
		outAt := now
		if out != "" {
			if t, err := time.Parse(time.RFC3339, out); err == nil {
				outAt = t
			}
		}
		total += outAt.Sub(inAt)
	}
	return total.Hours(), rows.Err()
}

func (d *DB) accuracyForDayIfAny(username, role, date string) (pct float64, hasAny bool, err error) {
	hist, err := d.AccuracyHistory(username, role, date)
	if err != nil || len(hist) == 0 {
		return 0, false, err
	}
	var sum float64
	for _, e := range hist {
		sum += e.Delta
	}
	return max(0, min(100, 100+sum)), true, nil
}
