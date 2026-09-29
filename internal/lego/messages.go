package lego

import "time"

// A one-way admin-to-user note, shown as a corner toast (see uiapp/messages.go) —
// separate from the full-screen "missing parts" alert. Sessions are separate OS
// processes (see gateway/telnet.go), so delivery is polling: a signed-in session
// checks for undelivered messages addressed to it every so often.
type Message struct {
	ID                  int64
	From, To, Body      string
	CreatedAt           time.Time
	DeliveredAt, ReadAt time.Time
}

func (d *DB) SendMessage(from, to, body string) error {
	_, err := d.Exec(`INSERT INTO user_messages (from_user, to_user, body, created_at) VALUES (?,?,?,?)`,
		from, to, body, time.Now().Format(time.RFC3339))
	return err
}

// UndeliveredMessages is to's messages not yet shown to any of their sessions,
// oldest first — marking them delivered is the caller's job (MarkDelivered) once
// the toast has actually been queued, so a session that dies before showing one
// doesn't lose it.
func (d *DB) UndeliveredMessages(to string) ([]Message, error) {
	rows, err := d.Query(`SELECT id, from_user, to_user, body, created_at FROM user_messages
		WHERE to_user = ? AND delivered_at = '' ORDER BY id`, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		var createdAt string
		if err := rows.Scan(&m.ID, &m.From, &m.To, &m.Body, &createdAt); err != nil {
			return nil, err
		}
		m.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		out = append(out, m)
	}
	return out, rows.Err()
}

func (d *DB) MarkDelivered(id int64) error {
	_, err := d.Exec(`UPDATE user_messages SET delivered_at = ? WHERE id = ?`, time.Now().Format(time.RFC3339), id)
	return err
}
