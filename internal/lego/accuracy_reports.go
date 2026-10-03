package lego

import "time"

// AccuracyEscalation is one 21+-missing check/order flagged for manual review
// (see RecordCheckOutcome) — a row of accuracy_log with kind "escalation".
type AccuracyEscalation struct {
	ID             int64
	Username, Role string
	Target         string // the set number or order ref that triggered it
	Missing        int
	CreatedAt      time.Time
}

// OpenAccuracyEscalations is every escalation that has no accuracy_reports row
// filed against it yet, newest first — the Alerts screen's whole feed. Filing
// a report (FileAccuracyReport) is what removes one from this list.
func (d *DB) OpenAccuracyEscalations() ([]AccuracyEscalation, error) {
	rows, err := d.Query(`SELECT a.id, a.username, a.role, a.target, a.missing, a.created_at
		FROM accuracy_log a
		WHERE a.kind = ? AND NOT EXISTS (SELECT 1 FROM accuracy_reports r WHERE r.escalation_id = a.id)
		ORDER BY a.id DESC`, accKindEscalate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AccuracyEscalation
	for rows.Next() {
		var e AccuracyEscalation
		var createdAt string
		if err := rows.Scan(&e.ID, &e.Username, &e.Role, &e.Target, &e.Missing, &createdAt); err != nil {
			return nil, err
		}
		e.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		out = append(out, e)
	}
	return out, rows.Err()
}

// AccuracyEscalationByID fetches one escalation row — used to pre-fill the
// report form with who/what it was about.
func (d *DB) AccuracyEscalationByID(id int64) (AccuracyEscalation, error) {
	var e AccuracyEscalation
	var createdAt string
	err := d.QueryRow(`SELECT id, username, role, target, missing, created_at FROM accuracy_log WHERE id = ? AND kind = ?`,
		id, accKindEscalate).Scan(&e.ID, &e.Username, &e.Role, &e.Target, &e.Missing, &createdAt)
	e.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return e, err
}

// AccuracyReport is one manager's filed write-up — permanently kept, found
// through AccuracyReportsFor ("any manager can see it" later).
type AccuracyReport struct {
	ID                         int64
	EscalationID               int64
	Username, Role, ReviewedBy string
	Summary, ActionTaken       string
	TalkRequested              bool
	CreatedAt                  time.Time
}

// FileAccuracyReport records a manager's review of one escalation — the
// write-up itself (summary + action taken), whether it warrants flagging the
// employee for a conversation, and who reviewed it. Resolves the escalation
// as a side effect (see OpenAccuracyEscalations).
func (d *DB) FileAccuracyReport(escalationID int64, username, role, reviewedBy, summary, actionTaken string, talkRequested bool) (int64, error) {
	talk := 0
	if talkRequested {
		talk = 1
	}
	res, err := d.Exec(`INSERT INTO accuracy_reports (escalation_id, username, role, reviewed_by, summary, action_taken, talk_requested, created_at)
		VALUES (?,?,?,?,?,?,?,?)`,
		escalationID, username, role, reviewedBy, summary, actionTaken, talk, time.Now().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// AccuracyReportsFor is username/role's filed reports, newest first — the
// permanent history any admin can pull up for that person.
func (d *DB) AccuracyReportsFor(username, role string) ([]AccuracyReport, error) {
	rows, err := d.Query(`SELECT id, escalation_id, username, role, reviewed_by, summary, action_taken, talk_requested, created_at
		FROM accuracy_reports WHERE username = ? AND role = ? ORDER BY id DESC`, username, role)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AccuracyReport
	for rows.Next() {
		var r AccuracyReport
		var createdAt string
		var talk int
		if err := rows.Scan(&r.ID, &r.EscalationID, &r.Username, &r.Role, &r.ReviewedBy, &r.Summary, &r.ActionTaken, &talk, &createdAt); err != nil {
			return nil, err
		}
		r.TalkRequested = talk != 0
		r.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		out = append(out, r)
	}
	return out, rows.Err()
}
