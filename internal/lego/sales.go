package lego

import (
	"context"
	"database/sql"
	"sort"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/partdb"
)

// Sales orders are orders your customers placed in your BrickLink store (the
// existing Order type is the opposite direction: what you bought). They are
// imported here so they can be picked in shelf order and ticked off.

type SalesOrder struct {
	OrderID, Buyer, Status, OrderedAt, Currency string
	Total                                       float64
	Pieces                                      int
	ImportedAt, PickedAt                        string
	Lines                                       []SalesLine
}

type SalesLine struct {
	LineNo                                  int
	ItemType, ItemNo, Name, ColorName, Cond string
	BLColor, Qty, Picked                    int
	UnitPrice                               float64
	Location                                string // filled by SalesPickList, "" when unknown
}

// SalesFeed is where orders come from (BrickLink in production, a fake in tests).
type SalesFeed interface {
	ListOrders(ctx context.Context) ([]SalesOrder, error)
	OrderLines(ctx context.Context, orderID string) ([]SalesLine, error)
}

// SyncSalesOrders imports orders it hasn't seen (fetching their lines), refreshes
// the status of known ones, and marks local orders that BrickLink no longer lists
// as open as "closed". Lines are only fetched for new orders -- one API call
// each -- and a re-sync never resets what has already been picked.
func (d *DB) SyncSalesOrders(ctx context.Context, feed SalesFeed) (added int, err error) {
	orders, err := feed.ListOrders(ctx)
	if err != nil {
		return 0, err
	}
	seen := map[string]bool{}
	now := time.Now().UTC().Format(timeLayout)
	for _, o := range orders {
		seen[o.OrderID] = true
		res, err := d.Exec(`UPDATE sales_orders SET status = ?, total = ?, currency = ? WHERE order_id = ?`, o.Status, o.Total, o.Currency, o.OrderID)
		if err != nil {
			return added, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			continue
		}
		lines, err := feed.OrderLines(ctx, o.OrderID)
		if err != nil {
			return added, err // keep what was imported so far; the next sync retries this one
		}
		pieces := 0
		for _, l := range lines {
			pieces += l.Qty
		}
		if _, err := d.Exec(`INSERT INTO sales_orders (order_id, buyer, status, ordered_at, total, currency, pieces, imported_at) VALUES (?,?,?,?,?,?,?,?)`,
			o.OrderID, o.Buyer, o.Status, o.OrderedAt, o.Total, o.Currency, pieces, now); err != nil {
			return added, err
		}
		for i, l := range lines {
			if l.ItemType == "" {
				l.ItemType = "PART"
			}
			if _, err := d.Exec(`INSERT INTO sales_order_lines (order_id, line_no, item_type, item_no, name, bl_color, color_name, cond, qty, unit_price) VALUES (?,?,?,?,?,?,?,?,?,?)`,
				o.OrderID, i+1, l.ItemType, l.ItemNo, l.Name, l.BLColor, l.ColorName, l.Cond, l.Qty, l.UnitPrice); err != nil {
				return added, err
			}
		}
		added++
	}
	rows, err := d.Query(`SELECT order_id FROM sales_orders WHERE status NOT IN ('closed')`)
	if err != nil {
		return added, err
	}
	var gone []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil && !seen[id] {
			gone = append(gone, id)
		}
	}
	rows.Close()
	for _, id := range gone {
		if _, err := d.Exec(`UPDATE sales_orders SET status = 'closed' WHERE order_id = ?`, id); err != nil {
			return added, err
		}
	}
	return added, nil
}

// ListSalesOrders returns orders newest first; open=true hides closed ones.
func (d *DB) ListSalesOrders(open bool) ([]SalesOrder, error) {
	q := `SELECT order_id, buyer, status, ordered_at, total, currency, pieces, imported_at, picked_at FROM sales_orders`
	if open {
		q += ` WHERE status <> 'closed'`
	}
	rows, err := d.Query(q + ` ORDER BY ordered_at DESC, order_id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SalesOrder
	for rows.Next() {
		var o SalesOrder
		if err := rows.Scan(&o.OrderID, &o.Buyer, &o.Status, &o.OrderedAt, &o.Total, &o.Currency, &o.Pieces, &o.ImportedAt, &o.PickedAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// GetSalesOrder loads one order with its lines (nil, nil when unknown).
func (d *DB) GetSalesOrder(id string) (*SalesOrder, error) {
	var o SalesOrder
	err := d.QueryRow(`SELECT order_id, buyer, status, ordered_at, total, currency, pieces, imported_at, picked_at FROM sales_orders WHERE order_id = ?`, id).
		Scan(&o.OrderID, &o.Buyer, &o.Status, &o.OrderedAt, &o.Total, &o.Currency, &o.Pieces, &o.ImportedAt, &o.PickedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rows, err := d.Query(`SELECT line_no, item_type, item_no, name, bl_color, color_name, cond, qty, unit_price, picked FROM sales_order_lines WHERE order_id = ? ORDER BY line_no`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var l SalesLine
		if err := rows.Scan(&l.LineNo, &l.ItemType, &l.ItemNo, &l.Name, &l.BLColor, &l.ColorName, &l.Cond, &l.Qty, &l.UnitPrice, &l.Picked); err != nil {
			return nil, err
		}
		o.Lines = append(o.Lines, l)
	}
	return &o, rows.Err()
}

// SalesPickList returns the order with each line's shelf location filled in and
// the lines in shelf order (unlocated ones last, in their original order). BrickLink
// numbers are mapped to Rebrickable ones for the lookup: colours through bl_colors,
// part numbers as-is (they match for the large majority of parts).
func (d *DB) SalesPickList(pdb *partdb.DB, id string) (*SalesOrder, error) {
	o, err := d.GetSalesOrder(id)
	if err != nil || o == nil {
		return o, err
	}
	part, color, name := make([]string, len(o.Lines)), make([]int, len(o.Lines)), make([]string, len(o.Lines))
	for i, l := range o.Lines {
		part[i], color[i] = l.ItemNo, -1
		if c, ok := d.RBColorFor(l.BLColor); ok {
			color[i], name[i] = c.ID, c.Name
		}
	}
	loc := d.LocationNames(pdb, part, color, name)
	for i := range o.Lines {
		o.Lines[i].Location = loc[i]
	}
	sort.SliceStable(o.Lines, func(i, j int) bool { return locLess(o.Lines[i].Location, o.Lines[j].Location) })
	return o, nil
}

// SetSalesLinePicked sets how many of a line have been picked (clamped to 0..qty)
// and stamps the order picked_at once every line is fully picked.
func (d *DB) SetSalesLinePicked(orderID string, lineNo, picked int) error {
	if _, err := d.Exec(`UPDATE sales_order_lines SET picked = MAX(0, MIN(?, qty)) WHERE order_id = ? AND line_no = ?`, picked, orderID, lineNo); err != nil {
		return err
	}
	var open int
	if err := d.QueryRow(`SELECT COUNT(*) FROM sales_order_lines WHERE order_id = ? AND picked < qty`, orderID).Scan(&open); err != nil {
		return err
	}
	stamp := ""
	if open == 0 {
		stamp = time.Now().UTC().Format(timeLayout)
	}
	_, err := d.Exec(`UPDATE sales_orders SET picked_at = ? WHERE order_id = ?`, stamp, orderID)
	return err
}
