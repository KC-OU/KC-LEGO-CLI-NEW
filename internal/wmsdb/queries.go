package wmsdb

import (
	"context"
	"fmt"
	"strings"
)

type UserAuthResult struct {
	Found    bool   `json:"found"`
	ID       int    `json:"id"`
	UserNum  string `json:"user_num"`
	UserName string `json:"user_name"`
	Role     string `json:"role"`
	IsValid  bool   `json:"is_valid"`
	Email    string `json:"email"`
}

// AuthenticateUser mirrors the original's permissive login lookup: match by
// username, user_num, or numeric id, and accept auth_string equal to either
// the raw password or its MD5 hex (callers pass both, see internal/auth).
func (c *Client) AuthenticateUser(ctx context.Context, usernameOrID, password, passwordMD5 string) (*UserAuthResult, error) {
	script := c.connectPrelude() + fmt.Sprintf(`
uname = %s
pwd_raw = %s
pwd_md5 = %s
try:
    uid = int(uname)
except ValueError:
    uid = -1
cur.execute("SELECT id, user_num, user_name, user_role, is_valid, email FROM user WHERE (LOWER(user_name)=LOWER(?) OR LOWER(user_num)=LOWER(?) OR id=?) AND (auth_string=? OR auth_string=?)", (uname, uname, uid, pwd_raw, pwd_md5))
row = cur.fetchone()
if row:
    print(json.dumps({"found": True, "id": row["id"], "user_num": row["user_num"], "user_name": row["user_name"], "role": row["user_role"], "is_valid": bool(row["is_valid"]), "email": row["email"] or ""}))
else:
    print(json.dumps({"found": False}))
`, pyStringLit(usernameOrID), pyStringLit(password), pyStringLit(passwordMD5))

	var result UserAuthResult
	if err := c.RunScriptJSON(ctx, script, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

type Permissions struct {
	RoleID   int      `json:"role_id"`
	RoleName string   `json:"role_name"`
	Menus    []string `json:"menus"`
	CanWrite bool     `json:"can_write"`
	IsAdmin  bool     `json:"is_admin"`
}

var viewOnlyMenus = []string{
	"stockManagement", "baseModule", "stockAsn", "deliveryManagement",
	"commodityManagement", "supplier", "customer", "warehouseSetting",
	"ownerOfCargo", "freightSetting", "commodityCategorySetting",
}

// FetchUserPermissions replicates the original's role-name heuristic: a
// role not found in userrole falls back to a fixed view-only menu set (if
// its name suggests "view"/"read"/"viewonly") or unrestricted admin-like
// access otherwise; a found role's menus come from rolemenu/menu.
func (c *Client) FetchUserPermissions(ctx context.Context, role string) (*Permissions, error) {
	viewMenusJSON := "[" + strings.Join(quoteAll(viewOnlyMenus), ", ") + "]"
	script := c.connectPrelude() + fmt.Sprintf(`
role = %s
role_lower = role.lower()
is_view_name = ("view" in role_lower) or ("read" in role_lower) or role_lower == "viewonly"
cur.execute("SELECT id, role_name FROM userrole WHERE LOWER(role_name)=LOWER(?)", (role,))
row = cur.fetchone()
if row is None:
    if is_view_name:
        result = {"role_id": 0, "role_name": role, "menus": %s, "can_write": False, "is_admin": False}
    else:
        result = {"role_id": 0, "role_name": role, "menus": ["*"], "can_write": True, "is_admin": role_lower == "admin"}
else:
    role_id = row["id"]
    cur.execute("SELECT DISTINCT m.menu_name FROM rolemenu rm JOIN menu m ON rm.menu_id=m.id WHERE rm.userrole_id=?", (role_id,))
    menus = [r["menu_name"] for r in cur.fetchall()]
    result = {"role_id": role_id, "role_name": row["role_name"], "menus": menus, "can_write": not is_view_name, "is_admin": role_lower == "admin"}
print(json.dumps(result))
`, pyStringLit(role), viewMenusJSON)

	var result Permissions
	if err := c.RunScriptJSON(ctx, script, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func quoteAll(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = pyStringLit(s)
	}
	return out
}

type Metrics struct {
	SPUCount      int `json:"spu_count"`
	StockSum      int `json:"stock_sum"`
	FrozenCount   int `json:"frozen_count"`
	ASNCount      int `json:"asn_count"`
	DispatchCount int `json:"dispatch_count"`
	UserCount     int `json:"user_count"`
	SupplierCount int `json:"supplier_count"`
	CustomerCount int `json:"customer_count"`
	LowStockCount int `json:"low_stock_count"`
}

func (c *Client) DashboardMetrics(ctx context.Context) (*Metrics, error) {
	script := c.connectPrelude() + `
result = {}
cur.execute("SELECT COUNT(*) c FROM spu WHERE is_valid=1"); result["spu_count"] = cur.fetchone()["c"]
cur.execute("SELECT COALESCE(SUM(qty),0) c FROM stock"); result["stock_sum"] = cur.fetchone()["c"]
cur.execute("SELECT COUNT(*) c FROM stock WHERE is_freeze=1 AND qty>0"); result["frozen_count"] = cur.fetchone()["c"]
cur.execute("SELECT COUNT(*) c FROM asn WHERE is_valid=1"); result["asn_count"] = cur.fetchone()["c"]
# dispatchlist has no is_valid column (unlike spu/asn), so count every row.
cur.execute("SELECT COUNT(*) c FROM dispatchlist"); result["dispatch_count"] = cur.fetchone()["c"]
cur.execute("SELECT COUNT(*) c FROM user"); result["user_count"] = cur.fetchone()["c"]
cur.execute("SELECT COUNT(*) c FROM supplier"); result["supplier_count"] = cur.fetchone()["c"]
cur.execute("SELECT COUNT(*) c FROM customer"); result["customer_count"] = cur.fetchone()["c"]
cur.execute("SELECT COUNT(*) c FROM stock WHERE qty<=5"); result["low_stock_count"] = cur.fetchone()["c"]
print(json.dumps(result))
`
	var m Metrics
	if err := c.RunScriptJSON(ctx, script, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

type StockRow struct {
	ID         int    `json:"id"`
	SPUCode    string `json:"spu_code"`
	SPUName    string `json:"spu_name"`
	Qty        int    `json:"qty"`
	Frozen     bool   `json:"frozen"`
	LastUpdate string `json:"last_update"`
}

func (c *Client) StockLookup(ctx context.Context, search string) ([]StockRow, error) {
	like := "%" + search + "%"
	script := c.connectPrelude() + fmt.Sprintf(`
like = %s
cur.execute("SELECT s.id, sp.spu_code, sp.spu_name, s.qty, s.is_freeze, s.last_update_time FROM stock s JOIN spu sp ON s.sku_id = sp.id WHERE sp.spu_name LIKE ? OR sp.spu_code LIKE ? OR sp.id LIKE ? LIMIT 40", (like, like, like))
rows = [{"id": r["id"], "spu_code": r["spu_code"], "spu_name": r["spu_name"], "qty": r["qty"], "frozen": bool(r["is_freeze"]), "last_update": r["last_update_time"]} for r in cur.fetchall()]
print(json.dumps(rows))
`, pyStringLit(like))

	var rows []StockRow
	if err := c.RunScriptJSON(ctx, script, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

type SPURow struct {
	ID   int    `json:"id"`
	Code string `json:"code"`
	Name string `json:"name"`
}

func (c *Client) MasterDataSPUs(ctx context.Context) ([]SPURow, error) {
	script := c.connectPrelude() + `
cur.execute("SELECT id, spu_code, spu_name FROM spu LIMIT 30")
rows = [{"id": r["id"], "code": r["spu_code"], "name": r["spu_name"]} for r in cur.fetchall()]
print(json.dumps(rows))
`
	var rows []SPURow
	if err := c.RunScriptJSON(ctx, script, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

type NamedRow struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func (c *Client) MasterDataSuppliers(ctx context.Context) ([]NamedRow, error) {
	return c.namedRows(ctx, "SELECT id, supplier_name AS name FROM supplier LIMIT 30")
}

func (c *Client) MasterDataCustomers(ctx context.Context) ([]NamedRow, error) {
	return c.namedRows(ctx, "SELECT id, customer_name AS name FROM customer LIMIT 30")
}

func (c *Client) namedRows(ctx context.Context, sql string) ([]NamedRow, error) {
	script := c.connectPrelude() + fmt.Sprintf(`
cur.execute(%s)
rows = [{"id": r["id"], "name": r["name"]} for r in cur.fetchall()]
print(json.dumps(rows))
`, pyStringLit(sql))
	var rows []NamedRow
	if err := c.RunScriptJSON(ctx, script, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

type DispatchRow struct {
	ID     int    `json:"id"`
	No     string `json:"no"`
	Status int    `json:"status"`
}

func (c *Client) DispatchList(ctx context.Context) ([]DispatchRow, error) {
	script := c.connectPrelude() + `
cur.execute("SELECT id, dispatch_no, dispatch_status FROM dispatchlist LIMIT 20")
rows = [{"id": r["id"], "no": r["dispatch_no"], "status": r["dispatch_status"]} for r in cur.fetchall()]
print(json.dumps(rows))
`
	var rows []DispatchRow
	if err := c.RunScriptJSON(ctx, script, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}
