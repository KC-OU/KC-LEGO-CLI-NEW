package lego

import (
	"errors"
	"strings"
	"time"
)

// A one-way admin-to-user note, shown as a corner toast (see uiapp/messages.go) —
// separate from the full-screen "missing parts" alert. Sessions are separate OS
// processes (see gateway/telnet.go), so delivery is polling: a signed-in session
// checks for undelivered messages addressed to it every so often.
type Message struct {
	ID                  int64
	From, To, Body      string
	CreatedAt           time.Time
	DeliveredAt, ReadAt time.Time
	// ParentID is the message this one replies to (0 for a message that starts a thread).
	ParentID int64
}

func (d *DB) SendMessage(from, to, body string) error {
	_, err := d.SendMessageRef(from, to, body, 0)
	return err
}

// SendMessageRef is SendMessage returning the new message's id, optionally as a reply to parent.
func (d *DB) SendMessageRef(from, to, body string, parent int64) (int64, error) {
	body = strings.TrimSpace(body)
	if strings.TrimSpace(to) == "" || body == "" {
		return 0, errors.New("a message needs someone to send it to and some text")
	}
	if len(body) > 2000 {
		body = body[:2000]
	}
	res, err := d.Exec(`INSERT INTO user_messages (from_user, to_user, body, created_at, parent_id) VALUES (?,?,?,?,?)`,
		from, to, body, time.Now().Format(time.RFC3339), parent)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ReplyMessage answers message parent, addressed to whoever sent it. Only its recipient may reply, so a
// checker can answer an admin but cannot start a conversation with anyone.
func (d *DB) ReplyMessage(from string, parent int64, body string) (int64, error) {
	var sender, recipient string
	if err := d.QueryRow(`SELECT from_user, to_user FROM user_messages WHERE id = ?`, parent).Scan(&sender, &recipient); err != nil {
		return 0, errors.New("that message no longer exists")
	}
	if !strings.EqualFold(recipient, from) {
		return 0, errors.New("you can only reply to a message sent to you")
	}
	return d.SendMessageRef(from, sender, body, parent)
}

// ListMessages is to's inbox, newest first (read and unread); unread ones have a zero ReadAt.
func (d *DB) ListMessages(to string, limit int) ([]Message, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := d.Query(`SELECT id, from_user, to_user, body, created_at, read_at, parent_id FROM user_messages
		WHERE to_user = ? ORDER BY id DESC LIMIT ?`, to, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		var createdAt, readAt string
		if err := rows.Scan(&m.ID, &m.From, &m.To, &m.Body, &createdAt, &readAt, &m.ParentID); err != nil {
			return nil, err
		}
		m.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		m.ReadAt, _ = time.Parse(time.RFC3339, readAt)
		out = append(out, m)
	}
	return out, rows.Err()
}

// UnreadCount is how many of to's messages they have not opened on the web inbox yet.
func (d *DB) UnreadCount(to string) int {
	var n int
	_ = d.QueryRow(`SELECT COUNT(*) FROM user_messages WHERE to_user = ? AND read_at = ''`, to).Scan(&n)
	return n
}

// MarkRead marks one message read, only if it is to's own (id 0 marks everything).
func (d *DB) MarkRead(to string, id int64) error {
	now := time.Now().Format(time.RFC3339)
	if id == 0 {
		_, err := d.Exec(`UPDATE user_messages SET read_at = ? WHERE to_user = ? AND read_at = ''`, now, to)
		return err
	}
	_, err := d.Exec(`UPDATE user_messages SET read_at = ? WHERE id = ? AND to_user = ? AND read_at = ''`, now, id, to)
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
