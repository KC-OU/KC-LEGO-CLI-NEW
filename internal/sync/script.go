package sync

import (
	"encoding/json"
	"fmt"
)

// spuRow is a fully-derived SPU/SKU record ready to write — all business
// logic (code/name/description/gtin derivation) already ran in Go via the
// Derive* functions in derive.go, so the embedded Python below is a dumb
// executor, not a second copy of the mapping rules.
type spuRow struct {
	ID          int    `json:"id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
	BarCode     string `json:"bar_code"`
	CategoryID  int    `json:"category_id"`
	Mass        string `json:"mass"`
}

type categoryRow struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	ParentID int    `json:"parent_id"`
}

type syncPayload struct {
	Now                 string        `json:"now"`
	Categories          []categoryRow `json:"categories"`
	CategorySoftDeletes []int         `json:"category_soft_delete"`
	SPUs                []spuRow      `json:"spus"`
	SPUSoftDeletes      []int         `json:"spu_soft_delete"`
	Stock               map[int]int   `json:"stock"`
	// ViewOnlyAuth is the ModernWMS password hash of the read-only "viewonly" account the sync keeps
	// in place (SYNC_VIEWONLY_AUTH). Empty means the account is left alone, never created with a default.
	ViewOnlyAuth  string `json:"viewonly_auth"`
	ViewOnlyEmail string `json:"viewonly_email"`
}

func pyStringLit(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// buildApplyScript embeds the already-computed payload as a JSON string
// literal (matching the original's "double-JSON-encoded" embedding
// technique) so the in-container script is a plain executor over it. The
// fixed bootstrap rows (warehouse/location/owner/supplier/category-0,
// the viewonly seed account+role+rolemenu) are re-applied idempotently via
// INSERT OR REPLACE every run, exactly as the original does.
func buildApplyScript(dbPath string, payload syncPayload) (string, error) {
	// A nil slice/map here would json.Marshal to `null`, and the generated
	// script below does `for x in data["..."]` / `.items()` unconditionally
	// on every one of these fields — `null` crashes the script instead of
	// iterating zero times. Normalize once at the marshal boundary rather
	// than relying on every producer to remember to return `[]`/`{}`.
	if payload.Categories == nil {
		payload.Categories = []categoryRow{}
	}
	if payload.CategorySoftDeletes == nil {
		payload.CategorySoftDeletes = []int{}
	}
	if payload.SPUs == nil {
		payload.SPUs = []spuRow{}
	}
	if payload.SPUSoftDeletes == nil {
		payload.SPUSoftDeletes = []int{}
	}
	if payload.Stock == nil {
		payload.Stock = map[int]int{}
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	script := fmt.Sprintf(`import sqlite3, json
con = sqlite3.connect(%s)
con.row_factory = sqlite3.Row
cur = con.cursor()
data = json.loads(%s)
now = data["now"]

cur.execute("UPDATE user SET is_valid = 0 WHERE user_name = 'admin' OR user_num = 'admin'")
cur.execute("UPDATE user SET is_valid = 1, tenant_id = 1 WHERE user_num IN ('7354','10932')")

if data.get("viewonly_auth"):
    cur.execute("""INSERT OR REPLACE INTO user (id, user_num, user_name, contact_tel, user_role, sex, is_valid, auth_string, email, creator, create_time, last_update_time, tenant_id)
                   VALUES (4, 'viewonly', 'viewonly', '', 'ViewOnly', 'Unknown', 1, ?, ?, '7354', ?, ?, 1)""", (data["viewonly_auth"], data.get("viewonly_email", ""), now, now))
cur.execute("""INSERT OR REPLACE INTO userrole (id, role_name, is_valid, create_time, last_update_time, tenant_id)
               VALUES (6, 'ViewOnly', 1, ?, ?, 1)""", (now, now))
cur.execute("DELETE FROM rolemenu WHERE userrole_id = 6")
for menu_id in [24, 25, 26, 27, 28, 29, 30, 31, 37, 38]:
    cur.execute("""INSERT INTO rolemenu (userrole_id, menu_id, authority, create_time, last_update_time, tenant_id)
                   VALUES (6, ?, 1, ?, ?, 1)""", (menu_id, now, now))

cur.execute("""INSERT OR REPLACE INTO warehouse (id, warehouse_name, city, address, email, manager, contact_tel, creator, create_time, last_update_time, is_valid, tenant_id)
               VALUES (1, 'Main Warehouse', 'Default City', 'Main Street', '', '7354', '', '7354', ?, ?, 1, 1)""", (now, now))
cur.execute("""INSERT OR REPLACE INTO warehousearea (id, warehouse_id, area_name, parent_id, create_time, last_update_time, is_valid, tenant_id, area_property)
               VALUES (1, 1, 'General Storage', 0, ?, ?, 1, 1, 1)""", (now, now))
cur.execute("""INSERT OR REPLACE INTO goodslocation (id, warehouse_id, warehouse_name, warehouse_area_name, warehouse_area_property, location_name, location_length, location_width, location_heigth, location_volume, location_load, roadway_number, shelf_number, layer_number, tag_number, create_time, last_update_time, is_valid, tenant_id, warehouse_area_id)
               VALUES (1, 1, 'Main Warehouse', 'General Storage', 1, 'KCLEGO', '0', '0', '0', '0', '0', '1', '1', '1', 'KCLEGO', ?, ?, 1, 1, 1)""", (now, now))
cur.execute("UPDATE goodslocation SET is_valid = 0 WHERE id != 1")
cur.execute("""INSERT OR REPLACE INTO goodsowner (id, goods_owner_name, city, address, manager, contact_tel, creator, create_time, last_update_time, is_valid, tenant_id)
               VALUES (1, 'Default Owner', '', '', '7354', '', '7354', ?, ?, 1, 1)""", (now, now))
cur.execute("""INSERT OR REPLACE INTO supplier (id, supplier_name, city, address, email, manager, contact_tel, creator, create_time, last_update_time, is_valid, tenant_id)
               VALUES (1, 'KCLEGO', '', '', '', '7354', '', '7354', ?, ?, 1, 1)""", (now, now))
cur.execute("""INSERT OR REPLACE INTO category (id, category_name, parent_id, creator, create_time, last_update_time, is_valid, tenant_id)
               VALUES (0, 'Uncategorized', 0, '7354', ?, ?, 1, 1)""", (now, now))

for c in data["categories"]:
    cur.execute("""INSERT OR REPLACE INTO category (id, category_name, parent_id, creator, create_time, last_update_time, is_valid, tenant_id)
                   VALUES (?, ?, ?, '7354', ?, ?, 1, 1)""", (c["id"], c["name"], c["parent_id"], now, now))
for cat_id in data["category_soft_delete"]:
    cur.execute("UPDATE category SET is_valid = 0 WHERE id = ?", (cat_id,))

for s in data["spus"]:
    cur.execute("""INSERT OR REPLACE INTO spu (id, spu_code, spu_name, category_id, spu_description, bar_code, supplier_id, supplier_name,
                    brand, origin, length_unit, volume_unit, weight_unit, creator, create_time, last_update_time, is_valid, tenant_id)
                    VALUES (?, ?, ?, ?, ?, ?, 1, 'KCLEGO', '', '', 0, 0, 0, '7354', ?, ?, 1, 1)""",
                (s["id"], s["code"], s["name"], s["category_id"], s["description"], s["bar_code"], now, now))
    cur.execute("""INSERT OR REPLACE INTO sku (id, spu_id, sku_code, sku_name, weight, lenght, width, height, volume, unit, cost, price, create_time, last_update_time)
                   VALUES (?, ?, ?, ?, ?, '0', '0', '0', '0', 'pcs', '0', '0', ?, ?)""",
                (s["id"], s["id"], s["code"], s["name"], s["mass"], now, now))
for spu_id in data["spu_soft_delete"]:
    cur.execute("UPDATE spu SET is_valid = 0 WHERE id = ?", (spu_id,))
    cur.execute("DELETE FROM stock WHERE sku_id = ?", (spu_id,))

for part_id_str, qty in data["stock"].items():
    part_id = int(part_id_str)
    cur.execute("SELECT id FROM stock WHERE sku_id = ?", (part_id,))
    if cur.fetchone() is None:
        cur.execute("""INSERT INTO stock (sku_id, goods_location_id, qty, goods_owner_id, is_freeze, last_update_time, tenant_id)
                       VALUES (?, 1, ?, 1, 0, ?, 1)""", (part_id, qty, now))

cur.execute("SELECT COALESCE(SUM(qty), 0) AS t FROM stock WHERE goods_location_id = 1")
total_qty = cur.fetchone()["t"]
con.commit()
print(json.dumps({
    "synced_categories": len(data["categories"]),
    "synced_locations": 1,
    "synced_parts": len(data["spus"]),
    "synced_stock_records": len(data["stock"]),
    "total_stock_qty": total_qty,
}))
`, pyStringLit(dbPath), pyStringLit(string(payloadJSON)))

	return script, nil
}

// existingIDsScript queries which category/spu ids already exist in
// ModernWMS, so the Go-side soft-delete diff (SoftDeleteTargets) can decide
// what to retire without a second round trip per id.
func existingIDsScript(dbPath string) string {
	return fmt.Sprintf(`import sqlite3, json
con = sqlite3.connect(%s)
cur = con.cursor()
cur.execute("SELECT id FROM category")
cat_ids = [r[0] for r in cur.fetchall()]
cur.execute("SELECT id FROM spu")
spu_ids = [r[0] for r in cur.fetchall()]
print(json.dumps({"category_ids": cat_ids, "spu_ids": spu_ids}))
`, pyStringLit(dbPath))
}
