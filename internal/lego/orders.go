package lego

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Orders for missing parts: where you bought them, what they cost (with shipping),
// the invoice and tracking numbers, and where they are. Receiving a line moves the
// parts into the set it was for (its missing count drops, and at 0 the set is
// complete) or, for a line not tied to a set, into your loose parts.

var OrderStatuses = []string{"wanted", "ordered", "shipped", "received", "cancelled"}

var SupplierKinds = []string{"bricklink", "brickowl", "pab", "rebrickable", "other"}

type Order struct {
	ID                                          int64
	SupplierKind, Supplier                      string
	OrderNo, InvoiceNo, TrackingNo, Carrier     string
	Currency                                    string
	Shipping                                    float64
	Status                                      string
	CreatedAt, OrderedAt, ShippedAt, ReceivedAt string
	Note, CreatedBy                             string
	Lines                                       []OrderLine
}

type OrderLine struct {
	ID                  int64
	SetNum, PartNum     string
	ColorID             int
	ColorName, PartName string
	Qty, ReceivedQty    int
	UnitPrice           float64
}

// Total is the parts plus shipping.
func (o *Order) Total() float64 {
	t := o.Shipping
	for _, l := range o.Lines {
		t += float64(l.Qty) * l.UnitPrice
	}
	return t
}

// Pieces is how many parts the order holds.
func (o *Order) Pieces() int {
	n := 0
	for _, l := range o.Lines {
		n += l.Qty
	}
	return n
}

func today() string { return time.Now().Format("2006-01-02") }

// SaveOrder creates or updates the order header (not its lines).
func (d *DB) SaveOrder(o *Order) error {
	if strings.TrimSpace(o.SupplierKind) == "" {
		o.SupplierKind = "other"
	}
	if o.Status == "" {
		o.Status = "wanted"
	}
	if o.ID == 0 {
		o.CreatedAt = time.Now().UTC().Format(timeLayout)
		res, err := d.Exec(`INSERT INTO orders (supplier_kind, supplier, order_no, invoice_no, tracking_no, carrier, currency, shipping, status, created_at, ordered_at, shipped_at, received_at, note, created_by)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, o.SupplierKind, o.Supplier, o.OrderNo, o.InvoiceNo, o.TrackingNo, o.Carrier, o.Currency, o.Shipping, o.Status,
			o.CreatedAt, o.OrderedAt, o.ShippedAt, o.ReceivedAt, o.Note, o.CreatedBy)
		if err != nil {
			return fmt.Errorf("saving the order: %w", err)
		}
		o.ID, _ = res.LastInsertId()
		return nil
	}
	_, err := d.Exec(`UPDATE orders SET supplier_kind=?, supplier=?, order_no=?, invoice_no=?, tracking_no=?, carrier=?, currency=?, shipping=?, status=?,
		ordered_at=?, shipped_at=?, received_at=?, note=? WHERE id=?`, o.SupplierKind, o.Supplier, o.OrderNo, o.InvoiceNo, o.TrackingNo, o.Carrier, o.Currency,
		o.Shipping, o.Status, o.OrderedAt, o.ShippedAt, o.ReceivedAt, o.Note, o.ID)
	return err
}

// AddOrderLine adds parts to an order.
func (d *DB) AddOrderLine(orderID int64, l OrderLine) error {
	if l.Qty <= 0 {
		return errors.New("quantity must be at least 1")
	}
	_, err := d.Exec(`INSERT INTO order_lines (order_id, set_num, part_num, color_id, color_name, part_name, qty, unit_price) VALUES (?,?,?,?,?,?,?,?)`,
		orderID, l.SetNum, l.PartNum, l.ColorID, l.ColorName, l.PartName, l.Qty, l.UnitPrice)
	return err
}

// SetLinePrice changes a line's unit price.
func (d *DB) SetLinePrice(lineID int64, price float64) error {
	_, err := d.Exec(`UPDATE order_lines SET unit_price = ? WHERE id = ?`, price, lineID)
	return err
}

// GetOrder loads an order with its lines.
func (d *DB) GetOrder(id int64) (*Order, error) {
	o := &Order{ID: id}
	if err := d.QueryRow(`SELECT supplier_kind, supplier, order_no, invoice_no, tracking_no, carrier, currency, shipping, status, created_at, ordered_at, shipped_at, received_at, note, created_by
		FROM orders WHERE id = ?`, id).Scan(&o.SupplierKind, &o.Supplier, &o.OrderNo, &o.InvoiceNo, &o.TrackingNo, &o.Carrier, &o.Currency, &o.Shipping, &o.Status,
		&o.CreatedAt, &o.OrderedAt, &o.ShippedAt, &o.ReceivedAt, &o.Note, &o.CreatedBy); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("no order #%d", id)
		}
		return nil, err
	}
	rows, err := d.Query(`SELECT id, set_num, part_num, color_id, color_name, part_name, qty, unit_price, received_qty FROM order_lines WHERE order_id = ? ORDER BY id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var l OrderLine
		if err := rows.Scan(&l.ID, &l.SetNum, &l.PartNum, &l.ColorID, &l.ColorName, &l.PartName, &l.Qty, &l.UnitPrice, &l.ReceivedQty); err != nil {
			return nil, err
		}
		o.Lines = append(o.Lines, l)
	}
	return o, rows.Err()
}

// ListOrders returns orders newest first; status "" = all, "open" = not received or cancelled.
func (d *DB) ListOrders(status string) ([]Order, error) {
	q := `SELECT id FROM orders`
	var args []any
	switch status {
	case "":
	case "open":
		q += ` WHERE status NOT IN ('received','cancelled')`
	default:
		q += ` WHERE status = ?`
		args = append(args, status)
	}
	rows, err := d.Query(q+` ORDER BY id DESC`, args...)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		_ = rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	out := make([]Order, 0, len(ids))
	for _, id := range ids {
		o, err := d.GetOrder(id)
		if err != nil {
			return nil, err
		}
		out = append(out, *o)
	}
	return out, nil
}

// SetOrderStatus moves an order along; "received" receives every outstanding line.
// It returns the sets that became complete.
func (d *DB) SetOrderStatus(id int64, status string) (completed []string, err error) {
	valid := false
	for _, s := range OrderStatuses {
		valid = valid || s == status
	}
	if !valid {
		return nil, fmt.Errorf("status must be one of %s", strings.Join(OrderStatuses, ", "))
	}
	if status == "received" {
		o, err := d.GetOrder(id)
		if err != nil {
			return nil, err
		}
		for _, l := range o.Lines {
			if left := l.Qty - l.ReceivedQty; left > 0 {
				done, err := d.ReceiveLine(l.ID, left)
				if err != nil {
					return completed, err
				}
				if done != "" {
					completed = append(completed, done)
				}
			}
		}
	}
	col := map[string]string{"ordered": "ordered_at", "shipped": "shipped_at", "received": "received_at"}[status]
	q := `UPDATE orders SET status = ? WHERE id = ?`
	if col != "" {
		q = `UPDATE orders SET status = ?, ` + col + ` = CASE WHEN ` + col + ` = '' THEN '` + today() + `' ELSE ` + col + ` END WHERE id = ?`
	}
	_, err = d.Exec(q, status, id)
	return completed, err
}

// ReceiveLine books qty of an order line as arrived: into its set (the set's last
// check gains them) or, for a line with no set, into your loose parts. It returns
// the set number when that set just became complete.
func (d *DB) ReceiveLine(lineID int64, qty int) (completedSet string, err error) {
	d.ensureDailySnapshot()
	tx, err := d.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var l OrderLine
	if err := tx.QueryRow(`SELECT set_num, part_num, color_id, color_name, part_name, qty, received_qty FROM order_lines WHERE id = ?`, lineID).
		Scan(&l.SetNum, &l.PartNum, &l.ColorID, &l.ColorName, &l.PartName, &l.Qty, &l.ReceivedQty); err != nil {
		return "", fmt.Errorf("no order line %d", lineID)
	}
	qty = min(qty, l.Qty-l.ReceivedQty)
	if qty <= 0 {
		return "", nil
	}
	if _, err := tx.Exec(`UPDATE order_lines SET received_qty = received_qty + ? WHERE id = ?`, qty, lineID); err != nil {
		return "", err
	}
	var checkID int64
	if l.SetNum != "" {
		_ = tx.QueryRow(`SELECT last_check_id FROM set_state WHERE set_num = ?`, l.SetNum).Scan(&checkID)
	}
	if checkID > 0 {
		var before int
		_ = tx.QueryRow(`SELECT COALESCE(SUM(MAX(need - have, 0)), 0) FROM set_check_lines WHERE check_id = ?`, checkID).Scan(&before)
		if _, err := tx.Exec(`UPDATE set_check_lines SET have = MIN(need, have + ?) WHERE check_id = ? AND part_num = ? AND color_id = ?`, qty, checkID, l.PartNum, l.ColorID); err != nil {
			return "", err
		}
		if err := refreshMissing(tx, l.SetNum, checkID); err != nil {
			return "", err
		}
		var after int
		_ = tx.QueryRow(`SELECT missing_qty FROM set_state WHERE set_num = ?`, l.SetNum).Scan(&after)
		if before > 0 && after == 0 {
			completedSet = l.SetNum
		}
		d.journal(tx, "receive", "set", l.SetNum, l.ColorID, l.ColorName, before, after, fmt.Sprintf("received %d x %s", qty, l.PartNum))
	} else {
		cur, err := getOwnedTx(tx, l.PartNum, l.ColorID, l.ColorName)
		if err != nil {
			return "", err
		}
		p := OwnedPart{PartNum: l.PartNum, Name: l.PartName, ColorID: l.ColorID, ColorName: l.ColorName, Qty: qty}
		before := 0
		if cur != nil {
			before, p.Qty, p.Category = cur.Qty, cur.Qty+qty, cur.Category
		}
		if err := addOwned(tx, p); err != nil {
			return "", err
		}
		d.journal(tx, "receive", "part", l.PartNum, l.ColorID, l.ColorName, before, p.Qty, "from an order")
	}
	return completedSet, tx.Commit()
}

// OrderFromMissing starts an order (status wanted) with a line for every part the
// set is short, at the given unit prices (0 = unknown).
func (d *DB) OrderFromMissing(setNum string, o *Order, price func(CheckLine) float64) error {
	c, err := d.LastCheck(setNum)
	if err != nil || c == nil {
		return fmt.Errorf("set %s has not been checked yet", setNum)
	}
	if err := d.SaveOrder(o); err != nil {
		return err
	}
	for _, l := range c.Lines {
		if m := l.Missing() - d.onOrder(setNum, l.PartNum, l.ColorID); m > 0 {
			p := 0.0
			if price != nil {
				p = price(l)
			}
			if err := d.AddOrderLine(o.ID, OrderLine{SetNum: setNum, PartNum: l.PartNum, ColorID: l.ColorID, ColorName: l.ColorName, PartName: l.PartName, Qty: m, UnitPrice: p}); err != nil {
				return err
			}
		}
	}
	return nil
}

// onOrder is how many of a set's part are already on an open order.
func (d *DB) onOrder(setNum, partNum string, colorID int) int {
	var n int
	_ = d.QueryRow(`SELECT COALESCE(SUM(l.qty - l.received_qty), 0) FROM order_lines l JOIN orders o ON o.id = l.order_id
		WHERE l.set_num = ? AND l.part_num = ? AND l.color_id = ? AND o.status NOT IN ('received','cancelled')`, setNum, partNum, colorID).Scan(&n)
	return n
}

// SpendRow is one line of the spend report.
type SpendRow struct {
	Key            string
	Orders, Pieces int
	Parts          float64
	Shipping       float64
	Currency       string
}

// Spend totals what orders (not cancelled) cost, grouped by "set", "supplier" or "month".
// Shipping is split across an order's sets by their share of its pieces.
func (d *DB) Spend(by string) ([]SpendRow, error) {
	orders, err := d.ListOrders("")
	if err != nil {
		return nil, err
	}
	agg := map[string]*SpendRow{}
	var keys []string
	add := func(k string, orderID int64, pieces int, parts, ship float64, cur string, seen map[string]bool) {
		r := agg[k]
		if r == nil {
			r = &SpendRow{Key: k, Currency: cur}
			agg[k] = r
			keys = append(keys, k)
		}
		if !seen[k] {
			r.Orders++
			seen[k] = true
		}
		r.Pieces += pieces
		r.Parts += parts
		r.Shipping += ship
	}
	for _, o := range orders {
		if o.Status == "cancelled" {
			continue
		}
		seen := map[string]bool{}
		total := o.Pieces()
		for _, l := range o.Lines {
			k := ""
			switch by {
			case "supplier":
				k = strings.TrimSpace(o.SupplierKind + " " + o.Supplier)
			case "month":
				day := o.OrderedAt
				if day == "" {
					day = o.CreatedAt
				}
				k = day[:min(7, len(day))]
			default:
				k = l.SetNum
				if k == "" {
					k = "(loose)"
				}
			}
			ship := 0.0
			if total > 0 {
				ship = o.Shipping * float64(l.Qty) / float64(total)
			}
			add(k, o.ID, l.Qty, float64(l.Qty)*l.UnitPrice, ship, o.Currency, seen)
		}
	}
	out := make([]SpendRow, 0, len(keys))
	for _, k := range keys {
		out = append(out, *agg[k])
	}
	return out, nil
}
