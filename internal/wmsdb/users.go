package wmsdb

import (
	"context"
	"fmt"
	"strings"
)

// protectedUsers mirrors the WMS-side half of the original's combined
// protected-account guard ("admin", "1", "kcollins", "2") — kcollins/2 are
// Part-DB-side identities and belong in internal/partdb's guard instead.
var protectedWMSUsers = map[string]bool{"admin": true, "1": true}

func (c *Client) EnsureSecurityTable(ctx context.Context) error {
	script := c.connectPrelude() + `
cur.execute("CREATE TABLE IF NOT EXISTS user_security (user_id INTEGER PRIMARY KEY, must_change_pw INTEGER DEFAULT 0, temp_pw_created_at TEXT)")
con.commit()
print(json.dumps({"ok": True}))
`
	var out struct {
		OK bool `json:"ok"`
	}
	return c.RunScriptJSON(ctx, script, &out)
}

type WMSUser struct {
	ID           int    `json:"id"`
	UserNum      string `json:"user_num"`
	UserName     string `json:"user_name"`
	Role         string `json:"role"`
	IsValid      bool   `json:"is_valid"`
	Email        string `json:"email"`
	CreateTime   string `json:"create_time"`
	MustChangePW bool   `json:"must_change_pw"`
}

func (c *Client) ListUsers(ctx context.Context) ([]WMSUser, error) {
	script := c.connectPrelude() + `
cur.execute("""SELECT u.id, u.user_num, u.user_name, u.user_role, u.is_valid, u.email, u.create_time, COALESCE(s.must_change_pw, 0) AS must_change_pw
               FROM user u LEFT JOIN user_security s ON u.id = s.user_id ORDER BY u.id ASC""")
rows = [{"id": r["id"], "user_num": r["user_num"], "user_name": r["user_name"], "role": r["user_role"], "is_valid": bool(r["is_valid"]), "email": r["email"] or "", "create_time": r["create_time"] or "", "must_change_pw": bool(r["must_change_pw"])} for r in cur.fetchall()]
print(json.dumps(rows))
`
	var rows []WMSUser
	if err := c.RunScriptJSON(ctx, script, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// CreateUser mirrors the original's manual id = MAX(id)+1 allocation
// (kept for compatibility with existing ModernWMS data, which relies on
// numeric id conventions elsewhere) and its hardcoded sex/creator/tenant
// defaults.
func (c *Client) CreateUser(ctx context.Context, username, role, email, authString string, mustChangePW bool) (int, error) {
	script := c.connectPrelude() + fmt.Sprintf(`
username = %s
role = %s
email = %s
auth_string = %s
must_change = %s

cur.execute("SELECT id FROM user WHERE LOWER(user_name)=LOWER(?) OR LOWER(user_num)=LOWER(?)", (username, username))
if cur.fetchone():
    print(json.dumps({"ok": False, "error": "User already exists"}))
else:
    cur.execute("SELECT COALESCE(MAX(id), 0) AS m FROM user")
    next_id = cur.fetchone()["m"] + 1
    cur.execute("""INSERT INTO user (id, user_num, user_name, contact_tel, user_role, sex, is_valid, auth_string, email, creator, create_time, last_update_time, tenant_id)
                   VALUES (?, ?, ?, '', ?, 'Unknown', 1, ?, ?, 'admin', datetime('now'), datetime('now'), 1)""",
                (next_id, username, username, role, auth_string, email))
    cur.execute("INSERT OR REPLACE INTO user_security (user_id, must_change_pw, temp_pw_created_at) VALUES (?, ?, datetime('now'))", (next_id, 1 if must_change else 0))
    con.commit()
    print(json.dumps({"ok": True, "id": next_id}))
`, pyStringLit(username), pyStringLit(role), pyStringLit(email), pyStringLit(authString), pyBoolLit(mustChangePW))

	var out struct {
		OK    bool   `json:"ok"`
		ID    int    `json:"id"`
		Error string `json:"error"`
	}
	if err := c.RunScriptJSON(ctx, script, &out); err != nil {
		return 0, err
	}
	if !out.OK {
		return 0, fmt.Errorf("%s", out.Error)
	}
	return out.ID, nil
}

// ResetPassword also reactivates the account (is_valid=1), matching the
// original's behavior.
func (c *Client) ResetPassword(ctx context.Context, usernameOrID, authString string, mustChangePW bool) error {
	script := c.connectPrelude() + fmt.Sprintf(`
ident = %s
auth_string = %s
must_change = %s
try:
    uid = int(ident)
except ValueError:
    uid = -1
cur.execute("SELECT id FROM user WHERE LOWER(user_name)=LOWER(?) OR LOWER(user_num)=LOWER(?) OR id=?", (ident, ident, uid))
row = cur.fetchone()
if row is None:
    print(json.dumps({"ok": False, "error": "User not found"}))
else:
    user_id = row["id"]
    cur.execute("UPDATE user SET auth_string=?, is_valid=1, last_update_time=datetime('now') WHERE id=?", (auth_string, user_id))
    cur.execute("INSERT OR REPLACE INTO user_security (user_id, must_change_pw, temp_pw_created_at) VALUES (?, ?, datetime('now'))", (user_id, 1 if must_change else 0))
    con.commit()
    print(json.dumps({"ok": True}))
`, pyStringLit(usernameOrID), pyStringLit(authString), pyBoolLit(mustChangePW))

	return c.runOKScript(ctx, script)
}

// ModifyUser applies a partial update: nil pointers keep the current value.
func (c *Client) ModifyUser(ctx context.Context, usernameOrID string, role, email *string, isValid *bool) error {
	script := c.connectPrelude() + fmt.Sprintf(`
ident = %s
try:
    uid = int(ident)
except ValueError:
    uid = -1
cur.execute("SELECT id, user_role, email, is_valid FROM user WHERE LOWER(user_name)=LOWER(?) OR LOWER(user_num)=LOWER(?) OR id=?", (ident, ident, uid))
row = cur.fetchone()
if row is None:
    print(json.dumps({"ok": False, "error": "User not found"}))
else:
    new_role = %s if %s else row["user_role"]
    new_email = %s if %s else row["email"]
    new_valid = %s if %s else row["is_valid"]
    cur.execute("UPDATE user SET user_role=?, email=?, is_valid=?, last_update_time=datetime('now') WHERE id=?", (new_role, new_email, new_valid, row["id"]))
    con.commit()
    print(json.dumps({"ok": True}))
`, pyStringLit(usernameOrID),
		pyOptionalStringLit(role), pyBoolLit(role != nil),
		pyOptionalStringLit(email), pyBoolLit(email != nil),
		pyOptionalIntBoolLit(isValid), pyBoolLit(isValid != nil))

	return c.runOKScript(ctx, script)
}

func (c *Client) ToggleActive(ctx context.Context, usernameOrID string, isValid bool) error {
	return c.ModifyUser(ctx, usernameOrID, nil, nil, &isValid)
}

// DeleteUser guards the WMS-side protected identities and never deletes a
// row whose user_name is literally "admin", matching the original's extra
// belt-and-suspenders check in the SQL itself.
func (c *Client) DeleteUser(ctx context.Context, usernameOrID string) error {
	if protectedWMSUsers[strings.ToLower(usernameOrID)] {
		return fmt.Errorf("protected administrator account %q cannot be deleted", usernameOrID)
	}
	script := c.connectPrelude() + fmt.Sprintf(`
ident = %s
try:
    uid = int(ident)
except ValueError:
    uid = -1
cur.execute("SELECT id FROM user WHERE (LOWER(user_name)=LOWER(?) OR LOWER(user_num)=LOWER(?) OR id=?) AND LOWER(user_name) != 'admin'", (ident, ident, uid))
row = cur.fetchone()
if row is None:
    print(json.dumps({"ok": False, "error": "User not found or protected"}))
else:
    cur.execute("DELETE FROM user WHERE id=?", (row["id"],))
    cur.execute("DELETE FROM user_security WHERE user_id=?", (row["id"],))
    con.commit()
    print(json.dumps({"ok": True}))
`, pyStringLit(usernameOrID))

	return c.runOKScript(ctx, script)
}

func (c *Client) CheckMustChangePassword(ctx context.Context, usernameOrID string) (bool, error) {
	script := c.connectPrelude() + fmt.Sprintf(`
ident = %s
try:
    uid = int(ident)
except ValueError:
    uid = -1
cur.execute("""SELECT COALESCE(s.must_change_pw, 0) AS m FROM user u LEFT JOIN user_security s ON u.id = s.user_id
               WHERE LOWER(u.user_name)=LOWER(?) OR LOWER(u.user_num)=LOWER(?) OR u.id=?""", (ident, ident, uid))
row = cur.fetchone()
print(json.dumps({"must_change": bool(row["m"]) if row else False}))
`, pyStringLit(usernameOrID))

	var out struct {
		MustChange bool `json:"must_change"`
	}
	if err := c.RunScriptJSON(ctx, script, &out); err != nil {
		return false, err
	}
	return out.MustChange, nil
}

func (c *Client) CompleteForcedPasswordChange(ctx context.Context, usernameOrID, newAuthString string) error {
	return c.ResetPassword(ctx, usernameOrID, newAuthString, false)
}

func (c *Client) runOKScript(ctx context.Context, script string) error {
	var out struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := c.RunScriptJSON(ctx, script, &out); err != nil {
		return err
	}
	if !out.OK {
		return fmt.Errorf("%s", out.Error)
	}
	return nil
}

func pyBoolLit(b bool) string {
	if b {
		return "True"
	}
	return "False"
}

func pyOptionalStringLit(s *string) string {
	if s == nil {
		return `""`
	}
	return pyStringLit(*s)
}

func pyOptionalIntBoolLit(b *bool) string {
	if b != nil && *b {
		return "1"
	}
	return "0"
}
