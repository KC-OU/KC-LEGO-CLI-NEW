package lego

import (
	"database/sql"
	"errors"
	"time"
)

// HandoverNote is the one free-text note a picker/checker leaves for whoever's
// on next — a single row, always id 1, shown at sign-in.
type HandoverNote struct {
	Body      string
	CreatedBy string
	CreatedAt time.Time
}

// GetHandoverNote returns nil, nil when nobody has left one yet.
func (d *DB) GetHandoverNote() (*HandoverNote, error) {
	var n HandoverNote
	var createdAt string
	err := d.QueryRow(`SELECT body, created_by, created_at FROM handover_note WHERE id = 1`).Scan(&n.Body, &n.CreatedBy, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	n.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return &n, nil
}

func (d *DB) SetHandoverNote(body, by string) error {
	_, err := d.Exec(`INSERT INTO handover_note (id, body, created_by, created_at) VALUES (1, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET body = excluded.body, created_by = excluded.created_by, created_at = excluded.created_at`,
		body, by, time.Now().Format(time.RFC3339))
	return err
}
