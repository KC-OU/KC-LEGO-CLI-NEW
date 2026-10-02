package lego

import (
	"database/sql"
	"errors"
	"time"
)

// A live session heartbeat, upserted by the session itself on a slow timer and
// deleted on a clean exit (see uiapp App.heartbeat) — purely diagnostic (Admin →
// live sessions), never used for anything access-related. A crashed session's row
// just goes stale; StaleSessions cleans those up.
type LiveSession struct {
	SessionID, Username, Role, Transport, RemoteAddr, Screen string
	StartedAt, LastSeen                                      time.Time
}

func (d *DB) Heartbeat(sessionID, username, role, transport, remoteAddr, screen string) error {
	now := time.Now().Format(time.RFC3339)
	_, err := d.Exec(`INSERT INTO live_sessions (session_id, username, role, transport, remote_addr, screen, started_at, last_seen)
		VALUES (?,?,?,?,?,?,?,?)
		ON CONFLICT(session_id) DO UPDATE SET screen = excluded.screen, last_seen = excluded.last_seen`,
		sessionID, username, role, transport, remoteAddr, screen, now, now)
	return err
}

func (d *DB) EndSession(sessionID string) error {
	_, err := d.Exec(`DELETE FROM live_sessions WHERE session_id = ?`, sessionID)
	return err
}

// ForceLogoff flags sessionID to be signed off at its next idle tick (at most
// 15s away — see app.go's checkForceLogoff), carrying the message that
// session should show once it happens.
func (d *DB) ForceLogoff(sessionID, message string) error {
	_, err := d.Exec(`INSERT INTO force_logoffs (session_id, message, created_at) VALUES (?,?,?)
		ON CONFLICT(session_id) DO UPDATE SET message = excluded.message, created_at = excluded.created_at`,
		sessionID, message, time.Now().Format(time.RFC3339))
	return err
}

// ConsumeForceLogoff reports and clears a pending ForceLogoff for sessionID —
// a session calls this on its own idle tick, same poll-and-clear shape as
// pollMessages/MarkDelivered.
func (d *DB) ConsumeForceLogoff(sessionID string) (message string, found bool, err error) {
	err = d.QueryRow(`SELECT message FROM force_logoffs WHERE session_id = ?`, sessionID).Scan(&message)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	_, err = d.Exec(`DELETE FROM force_logoffs WHERE session_id = ?`, sessionID)
	return message, true, err
}

// LiveSessions lists every session seen within maxAge — older rows are treated as
// crashed/abandoned, not shown.
func (d *DB) LiveSessions(maxAge time.Duration) ([]LiveSession, error) {
	cutoff := time.Now().Add(-maxAge).Format(time.RFC3339)
	rows, err := d.Query(`SELECT session_id, username, role, transport, remote_addr, screen, started_at, last_seen
		FROM live_sessions WHERE last_seen >= ? ORDER BY started_at`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LiveSession
	for rows.Next() {
		var s LiveSession
		var started, seen string
		if err := rows.Scan(&s.SessionID, &s.Username, &s.Role, &s.Transport, &s.RemoteAddr, &s.Screen, &started, &seen); err != nil {
			return nil, err
		}
		s.StartedAt, _ = time.Parse(time.RFC3339, started)
		s.LastSeen, _ = time.Parse(time.RFC3339, seen)
		out = append(out, s)
	}
	return out, rows.Err()
}
