package lego

import "time"

// AdminEvent kinds shown on the admin "Recent Activity" screen — a deliberately
// narrow, named set (see LogEvent's call sites), not every ticket claim or
// admin quick-switch.
const (
	EventMessage            = "message"
	EventAccuracyDock       = "accuracy_dock"
	EventAccuracyEscalation = "accuracy_escalation"
	EventMissingParts       = "missing_parts"
	EventForcedOff          = "forced_off"
	EventReassigned         = "reassigned"
)

type AdminEvent struct {
	ID                          int64
	Kind, Actor, Target, Detail string
	CreatedAt                   time.Time
}

// LogEvent records one admin-activity-feed entry. Best-effort: called
// alongside an action that already succeeded, so a logging failure here never
// undoes it — callers ignore the error (see e.g. tickets_screens.go).
func (d *DB) LogEvent(kind, actor, target, detail string) error {
	_, err := d.Exec(`INSERT INTO admin_events (kind, actor, target, detail, created_at) VALUES (?,?,?,?,?)`,
		kind, actor, target, detail, time.Now().Format(time.RFC3339))
	return err
}

// AdminEvents is the feed, newest first.
func (d *DB) AdminEvents(limit int) ([]AdminEvent, error) {
	rows, err := d.Query(`SELECT id, kind, actor, target, detail, created_at FROM admin_events ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AdminEvent
	for rows.Next() {
		var e AdminEvent
		var createdAt string
		if err := rows.Scan(&e.ID, &e.Kind, &e.Actor, &e.Target, &e.Detail, &createdAt); err != nil {
			return nil, err
		}
		e.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		out = append(out, e)
	}
	return out, rows.Err()
}
