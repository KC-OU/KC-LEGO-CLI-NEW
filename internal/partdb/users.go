package partdb

import (
	"database/sql"
	"fmt"
	"strings"
)

// PartDB's own protected-account guard: the primary admin account
// (kcollins) and the first two seeded ids can't be deleted.
func isProtectedPartDBUser(usernameOrID string, id int) bool {
	return strings.EqualFold(usernameOrID, "kcollins") || id <= 2
}

type PartDBUser struct {
	ID            int
	Name          string
	Email         string
	Disabled      bool
	NeedPWChange  bool
	GroupID       int
	GroupName     string
	DatetimeAdded string
}

func (d *DB) ListUsers() ([]PartDBUser, error) {
	rows, err := d.Query(`
		SELECT u.id, u.name, COALESCE(u.email,''), u.disabled, u.need_pw_change, u.group_id, COALESCE(g.name,''), COALESCE(u.datetime_added,'')
		FROM users u LEFT JOIN groups g ON u.group_id = g.id
		ORDER BY u.id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []PartDBUser
	for rows.Next() {
		var u PartDBUser
		if err := rows.Scan(&u.ID, &u.Name, &u.Email, &u.Disabled, &u.NeedPWChange, &u.GroupID, &u.GroupName, &u.DatetimeAdded); err != nil {
			return nil, err
		}
		if u.GroupName == "" {
			if u.GroupID == 1 {
				u.GroupName = "Admin"
			} else {
				u.GroupName = "User"
			}
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (d *DB) GetUserByName(name string) (*PartDBUser, error) {
	var u PartDBUser
	var passwordHash string
	err := d.QueryRow(`SELECT id, name, COALESCE(email,''), disabled, need_pw_change, group_id, password FROM users WHERE LOWER(name) = LOWER(?)`, name).
		Scan(&u.ID, &u.Name, &u.Email, &u.Disabled, &u.NeedPWChange, &u.GroupID, &passwordHash)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// PasswordHash returns the stored bcrypt hash for a username, used by the
// login flow (see internal/auth.VerifyPartDB).
func (d *DB) PasswordHash(name string) (string, bool, error) {
	var hash string
	err := d.QueryRow("SELECT password FROM users WHERE LOWER(name) = LOWER(?)", name).Scan(&hash)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return hash, true, nil
}

// CreateUser mirrors user_manager.py's create_partdb_user column defaults.
func (d *DB) CreateUser(username, role, email, bcryptHash string, needPWChange bool) (int64, error) {
	var existing int
	err := d.QueryRow("SELECT id FROM users WHERE LOWER(name) = LOWER(?)", username).Scan(&existing)
	if err == nil {
		return 0, fmt.Errorf("user already exists")
	}
	if err != sql.ErrNoRows {
		return 0, err
	}

	groupID := 2
	if strings.Contains(strings.ToLower(role), "admin") {
		groupID = 1
	}

	// config_instock_comment_a/_w and about_me are NOT NULL with no default in
	// current Part-DB schemas; omitting them made every user creation fail.
	res, err := d.Exec(`INSERT INTO users (name, email, password, group_id, disabled, need_pw_change,
		currency_id, trusted_device_cookie_version, backup_codes, settings, saml_user, show_email_on_profile,
		permissions_data, datetime_added, config_instock_comment_a, config_instock_comment_w, about_me)
		VALUES (?, ?, ?, ?, 0, ?, NULL, 1, '[]', '[]', 0, 0, '[]', datetime('now'), '', '', '')`,
		username, email, bcryptHash, groupID, boolToInt(needPWChange))
	if err != nil {
		return 0, fmt.Errorf("creating partdb user: %w", err)
	}
	return res.LastInsertId()
}

func (d *DB) ResetPassword(usernameOrID, bcryptHash string, needPWChange bool) error {
	id, err := d.resolveUserID(usernameOrID)
	if err != nil {
		return err
	}
	_, err = d.Exec("UPDATE users SET password = ?, need_pw_change = ?, disabled = 0 WHERE id = ?", bcryptHash, boolToInt(needPWChange), id)
	return err
}

// ModifyUser applies a partial update: nil pointers keep the current value.
func (d *DB) ModifyUser(usernameOrID string, role, email *string, isValid *bool) error {
	id, err := d.resolveUserID(usernameOrID)
	if err != nil {
		return err
	}

	if role != nil {
		groupID := 2
		if strings.Contains(strings.ToLower(*role), "admin") {
			groupID = 1
		}
		if _, err := d.Exec("UPDATE users SET group_id = ? WHERE id = ?", groupID, id); err != nil {
			return err
		}
	}
	if email != nil {
		if _, err := d.Exec("UPDATE users SET email = ? WHERE id = ?", *email, id); err != nil {
			return err
		}
	}
	if isValid != nil {
		disabled := boolToInt(!*isValid)
		if _, err := d.Exec("UPDATE users SET disabled = ? WHERE id = ?", disabled, id); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) ToggleActive(usernameOrID string, isValid bool) error {
	return d.ModifyUser(usernameOrID, nil, nil, &isValid)
}

func (d *DB) DeleteUser(usernameOrID string) error {
	id, err := d.resolveUserID(usernameOrID)
	if err != nil {
		return err
	}
	if isProtectedPartDBUser(usernameOrID, id) {
		return fmt.Errorf("protected administrator account %q cannot be deleted", usernameOrID)
	}
	_, err = d.Exec("DELETE FROM users WHERE id = ? AND LOWER(name) != 'kcollins' AND id > 2", id)
	return err
}

func (d *DB) resolveUserID(usernameOrID string) (int, error) {
	var id int
	err := d.QueryRow("SELECT id FROM users WHERE LOWER(name) = LOWER(?) OR CAST(id AS TEXT) = ?", usernameOrID, usernameOrID).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, fmt.Errorf("user %q not found", usernameOrID)
	}
	return id, err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
