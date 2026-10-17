package lego

// usernameColumns is every column in this database that stores a plain
// username string as a foreign key by convention (SQLite has no FK
// constraint on any of them). RenameUser updates all of them in one
// transaction so a renamed account's ticket, accuracy, kanban, attendance
// and messaging history stays attached to it instead of being silently
// orphaned under the old name. admin_events is here too even though it's
// just the cosmetic Recent Activity feed — cheap to keep in step. The
// audit log is deliberately NOT in this list: it is a tamper-evident,
// hash-chained append-only record (see internal/audit), so a rename must
// never rewrite it — old entries stay under the old username forever.
var usernameColumns = []struct{ table, col string }{
	{"job_tickets", "assigned_to"},
	{"job_tickets", "created_by"},
	{"kanban_notes", "created_by"},
	{"accuracy_log", "username"},
	{"accuracy_log", "created_by"},
	{"accuracy_reports", "username"},
	{"accuracy_reports", "reviewed_by"},
	{"set_checks", "checked_by"},
	{"set_check_proofs", "uploaded_by"},
	{"set_photos", "uploaded_by"},
	{"set_state", "backlog_released_by"},
	{"check_bags", "created_by"},
	{"bag_pool", "created_by"},
	{"bag_kinds", "created_by"},
	{"rota_entries", "username"},
	{"rota_entries", "created_by"},
	{"clock_events", "username"},
	{"user_messages", "from_user"},
	{"user_messages", "to_user"},
	{"admin_events", "actor"},
	{"admin_events", "target"},
}

// RenameUser moves a numeric Picker/Checker login ID (e.g. "7354" -> "7421")
// across every table that references it by username, in one transaction, and
// drops any live heartbeat row for the old name so an open session is forced
// to reconnect under the new one rather than silently losing its policy
// lookup mid-session. It does not touch internal/access's Policy.Users map,
// internal/twofa or internal/loginlock — those are separate on-disk stores
// the caller (wms access user rename) moves alongside this.
func (d *DB) RenameUser(oldUsername, newUsername string) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, c := range usernameColumns {
		if _, err := tx.Exec(`UPDATE `+c.table+` SET `+c.col+` = ? WHERE `+c.col+` = ?`, newUsername, oldUsername); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`DELETE FROM live_sessions WHERE username = ?`, oldUsername); err != nil {
		return err
	}
	return tx.Commit()
}
