package lego

import (
	"database/sql"
	"errors"
	"time"
)

// LastClosedShift is the most recently closed clock event, any user —
// "whoever was last on shift," for the auto handover summary below. nil,
// nil if nobody has ever clocked out.
func (d *DB) LastClosedShift() (*ClockEvent, error) {
	var e ClockEvent
	var in, out string
	err := d.QueryRow(`SELECT id, username, clock_in_at, clock_out_at FROM clock_events WHERE clock_out_at != '' ORDER BY id DESC LIMIT 1`).
		Scan(&e.ID, &e.Username, &in, &out)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	e.ClockInAt, _ = time.Parse(time.RFC3339, in)
	e.ClockOutAt, _ = time.Parse(time.RFC3339, out)
	return &e, nil
}

// ShiftHandoverSummary is "what got done / what's still open" for one
// finished shift — the automatic counterpart to the manual handover note
// (see HandoverNote), sitting alongside it rather than replacing it.
// Computed fresh whenever it's shown (at the next sign-in, see
// showHandoverNoteOnce), from the shift's own already-persisted clock-in/
// out times, rather than generated and stored the instant ClockOut runs —
// the same "stateless, computed fresh" shape as loadSortedCheck and
// friends, so no new table is needed. Like the manual note, this is never
// gated behind config.RequireClockIn — see RequireClockedIn's own doc
// comment ("viewing things ... is never gated by this").
type ShiftHandoverSummary struct {
	Username             string
	ShiftStart, ShiftEnd time.Time
	Done                 []Ticket // this user's own tickets finished during the shift
	StillOpen            []Ticket // every queued/claimed ticket, system-wide, as of now
}

func (d *DB) ShiftHandoverSummary(e ClockEvent) (*ShiftHandoverSummary, error) {
	rows, err := d.Query(`SELECT id, kind, target, label, assigned_to, status, priority, note, token, created_by, created_at, claimed_at, done_at
		FROM job_tickets WHERE assigned_to = ? AND status = ? AND done_at >= ? AND done_at <= ? ORDER BY done_at`,
		e.Username, TicketDone, e.ClockInAt.Format(time.RFC3339), e.ClockOutAt.Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	done, err := scanTickets(rows)
	if err != nil {
		return nil, err
	}
	open, err := d.AllOpenTickets()
	if err != nil {
		return nil, err
	}
	return &ShiftHandoverSummary{Username: e.Username, ShiftStart: e.ClockInAt, ShiftEnd: e.ClockOutAt, Done: done, StillOpen: open}, nil
}
