package wmsdb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// generateASNNo builds a receipt ticket number. The original used just
// "ASN"+YYYYMMDDHHMMSS with no suffix, which collides if two receipts land
// in the same second; appending a short random suffix is a deliberate fix.
func generateASNNo(now time.Time) string {
	suffix := make([]byte, 2)
	if _, err := rand.Read(suffix); err != nil {
		panic(err)
	}
	return fmt.Sprintf("ASN%s%s", now.Format("20060102150405"), hex.EncodeToString(suffix))
}

type ReceiptResult struct {
	Found       bool   `json:"found"`
	AsnNo       string `json:"asn_no"`
	PartID      int    `json:"part_id"`
	SpuCode     string `json:"spu_code"`
	SpuName     string `json:"spu_name"`
	AddedQty    int    `json:"added_qty"`
	PreviousQty int    `json:"previous_qty"`
	NewQty      int    `json:"new_qty"`
}

// ReceiveStock ports receive_stock.py: find the SPU by name/code/id/
// description, ensure a stock row at location 1, insert asn+asnsort rows,
// update the stock quantity. creatorUserNum is threaded through from the
// logged-in session instead of the original's hardcoded "7354".
func (c *Client) ReceiveStock(ctx context.Context, identifier string, qty int, creatorUserNum string) (*ReceiptResult, error) {
	now := time.Now()
	asnNo := generateASNNo(now)
	nowStr := now.Format("2006-01-02 15:04:05.000000")

	script := c.connectPrelude() + fmt.Sprintf(`
identifier = %s
qty_to_add = %s
asn_no = %s
now_str = %s
creator = %s

like = "%%" + identifier + "%%"
cur.execute("SELECT id, spu_code, spu_name, spu_description FROM spu WHERE spu_name = ? OR spu_code = ? OR id = ? OR spu_description LIKE ? LIMIT 1", (identifier, identifier, identifier, like))
part = cur.fetchone()
if part is None:
    print(json.dumps({"found": False}))
else:
    p_id = part["id"]
    cur.execute("SELECT qty FROM stock WHERE sku_id=? AND goods_location_id=1", (p_id,))
    stock_row = cur.fetchone()
    if stock_row is None:
        cur.execute("INSERT INTO stock (sku_id, goods_location_id, qty, goods_owner_id, is_freeze, last_update_time, tenant_id) VALUES (?, 1, 0, 1, 0, ?, 1)", (p_id, now_str))
        prev_qty = 0
    else:
        prev_qty = stock_row["qty"]
    new_qty = prev_qty + qty_to_add

    cur.execute("""INSERT INTO asn (asn_no, asn_status, spu_id, sku_id, asn_qty, actual_qty, sorted_qty, shortage_qty,
                    more_qty, damage_qty, weight, volume, supplier_id, supplier_name, goods_owner_id,
                    goods_owner_name, creator, create_time, last_update_time, is_valid, tenant_id)
                    VALUES (?, 4, ?, ?, ?, ?, ?, 0, 0, 0, '0.0', '0.0', 1, 'KCLEGO', 1, 'Default Owner', ?, ?, ?, 1, 1)""",
                (asn_no, p_id, p_id, qty_to_add, qty_to_add, qty_to_add, creator, now_str, now_str))
    asn_id = cur.lastrowid
    cur.execute("INSERT INTO asnsort (asn_id, sorted_qty, creator, create_time, last_update_time, is_valid, tenant_id) VALUES (?, ?, ?, ?, ?, 1, 1)",
                (asn_id, qty_to_add, creator, now_str, now_str))
    cur.execute("UPDATE stock SET qty=?, last_update_time=? WHERE sku_id=? AND goods_location_id=1", (new_qty, now_str, p_id))
    con.commit()
    print(json.dumps({"found": True, "asn_no": asn_no, "part_id": p_id, "spu_code": part["spu_code"], "spu_name": part["spu_name"], "added_qty": qty_to_add, "previous_qty": prev_qty, "new_qty": new_qty}))
`, pyStringLit(identifier), pyIntLit(qty), pyStringLit(asnNo), pyStringLit(nowStr), pyStringLit(creatorUserNum))

	var result ReceiptResult
	if err := c.RunScriptJSON(ctx, script, &result); err != nil {
		return nil, err
	}
	if !result.Found {
		return nil, fmt.Errorf("part not found: %s", identifier)
	}
	return &result, nil
}
