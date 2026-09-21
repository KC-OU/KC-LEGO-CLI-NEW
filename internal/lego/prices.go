package lego

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Prices, collection value and price alerts. Prices come from a PriceFetcher (the
// BrickLink client, wrapped by the caller), are stored per item so a value can be
// worked out any day without new calls, and are refreshed a few at a time within
// the daily API budget. A value is always reported as "N of M priced": missing
// prices are never silently counted as zero.

// PriceFetcher returns the average price of one item. found=false means the
// service does not know the item (that is remembered and not asked again for a
// while); any error stops the refresh, since the likely causes (budget spent,
// bad token, rate limit) affect every later call too.
type PriceFetcher interface {
	AvgPrice(ctx context.Context, itemType, itemNo string, blColor int, cond string) (avg float64, currency string, found bool, err error)
}

const (
	priceMaxAge     = 7 * 24 * time.Hour
	priceMissingAge = 14 * 24 * time.Hour // an item BrickLink does not know is not asked about again for two weeks
)

// PriceRow is one stored price.
type PriceRow struct {
	ItemType  string // "PART" or "SET"
	ItemNo    string
	BLColor   int
	Cond      string // "N" or "U"
	Avg       float64
	Currency  string
	Missing   bool
	FetchedAt time.Time
}

func (d *DB) getPrice(itemType, no string, blColor int, cond string) (*PriceRow, error) {
	var p PriceRow
	var missing int
	var at string
	err := d.QueryRow(`SELECT item_type, item_no, bl_color, cond, avg, currency, missing, fetched_at FROM prices
		WHERE item_type = ? AND item_no = ? AND bl_color = ? AND cond = ?`, itemType, no, blColor, cond).
		Scan(&p.ItemType, &p.ItemNo, &p.BLColor, &p.Cond, &p.Avg, &p.Currency, &missing, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.Missing = missing != 0
	p.FetchedAt, _ = time.Parse(timeLayout, at)
	return &p, nil
}

func (d *DB) putPrice(p PriceRow) error {
	_, err := d.Exec(`INSERT INTO prices (item_type, item_no, bl_color, cond, avg, currency, missing, fetched_at) VALUES (?,?,?,?,?,?,?,?)
		ON CONFLICT(item_type, item_no, bl_color, cond) DO UPDATE SET avg = excluded.avg, currency = excluded.currency, missing = excluded.missing, fetched_at = excluded.fetched_at`,
		p.ItemType, p.ItemNo, p.BLColor, p.Cond, p.Avg, p.Currency, b2i(p.Missing), time.Now().UTC().Format(timeLayout))
	return err
}

// priceKey identifies what to price for an owned part or set.
type priceKey struct {
	itemType string
	no       string
	blColor  int
	qty      int
	label    string
}

func (d *DB) priceTargets() ([]priceKey, error) {
	var keys []priceKey
	owned, err := d.ListOwnedParts()
	if err != nil {
		return nil, err
	}
	for _, p := range owned {
		if p.Qty <= 0 {
			continue
		}
		bl := 0
		if p.ColorID >= 0 {
			if id, ok := d.BLColorFor(p.ColorID); ok {
				bl = id
			}
		}
		keys = append(keys, priceKey{"PART", p.PartNum, bl, p.Qty, strings.TrimSpace(p.PartNum + " " + p.ColorName)})
	}
	rows, err := d.Query(`SELECT set_num, name, qty FROM sets WHERE qty > 0 AND parted_out = 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var num, name string
		var qty int
		if err := rows.Scan(&num, &name, &qty); err != nil {
			return nil, err
		}
		n := num
		if !strings.Contains(n, "-") {
			n += "-1"
		}
		keys = append(keys, priceKey{"SET", n, 0, qty, num + " " + name})
	}
	return keys, rows.Err()
}

// RefreshPrices fetches up to limit prices that are missing or stale, oldest first.
// It returns how many it fetched and how many items the service did not know, and
// stops at the first error (with what it got so far kept).
func (d *DB) RefreshPrices(ctx context.Context, f PriceFetcher, cond string, limit int) (fetched, unknown int, err error) {
	targets, err := d.priceTargets()
	if err != nil {
		return 0, 0, err
	}
	type due struct {
		k   priceKey
		age time.Time
	}
	var todo []due
	now := time.Now()
	for _, k := range targets {
		p, err := d.getPrice(k.itemType, k.no, k.blColor, cond)
		if err != nil {
			return 0, 0, err
		}
		switch {
		case p == nil:
			todo = append(todo, due{k, time.Time{}})
		case p.Missing && now.Sub(p.FetchedAt) > priceMissingAge, !p.Missing && now.Sub(p.FetchedAt) > priceMaxAge:
			todo = append(todo, due{k, p.FetchedAt})
		}
	}
	sort.SliceStable(todo, func(i, j int) bool { return todo[i].age.Before(todo[j].age) })
	for _, t := range todo {
		if fetched+unknown >= limit {
			break
		}
		avg, cur, found, err := f.AvgPrice(ctx, t.k.itemType, t.k.no, t.k.blColor, cond)
		if err != nil {
			return fetched, unknown, err
		}
		if err := d.putPrice(PriceRow{ItemType: t.k.itemType, ItemNo: t.k.no, BLColor: t.k.blColor, Cond: cond, Avg: avg, Currency: cur, Missing: !found}); err != nil {
			return fetched, unknown, err
		}
		if found {
			fetched++
		} else {
			unknown++
		}
	}
	return fetched, unknown, nil
}

// ValueLine is one priced line of the collection.
type ValueLine struct {
	Label string
	Qty   int
	Each  float64
	Total float64
}

// Value is what the collection is worth by the stored prices.
type Value struct {
	Currency string
	Total    float64
	Parts    float64
	Sets     float64
	Priced   int // lines with a price
	Lines    int // all lines
	Top      []ValueLine
}

// CollectionValue sums quantity x stored average price for every owned part and
// set. Lines without a price are counted in Lines but not Priced, never as zero.
func (d *DB) CollectionValue(cond string) (*Value, error) {
	targets, err := d.priceTargets()
	if err != nil {
		return nil, err
	}
	v := &Value{Lines: len(targets)}
	var lines []ValueLine
	for _, k := range targets {
		p, err := d.getPrice(k.itemType, k.no, k.blColor, cond)
		if err != nil {
			return nil, err
		}
		if p == nil || p.Missing {
			continue
		}
		v.Priced++
		if v.Currency == "" {
			v.Currency = p.Currency
		}
		total := p.Avg * float64(k.qty)
		v.Total += total
		if k.itemType == "SET" {
			v.Sets += total
		} else {
			v.Parts += total
		}
		lines = append(lines, ValueLine{k.label, k.qty, p.Avg, total})
	}
	sort.Slice(lines, func(i, j int) bool { return lines[i].Total > lines[j].Total })
	v.Top = lines[:min(len(lines), 10)]
	return v, nil
}

// ---- watchlist ----

// Watch is something you want to hear about when its price drops to your limit.
type Watch struct {
	ID          int64
	ItemType    string // "PART" or "SET"
	ItemNo      string
	ColorID     int // Rebrickable colour id, -1 for none
	ColorName   string
	Cond        string
	MaxPrice    float64
	LastPrice   float64
	LastChecked time.Time
	LastAlert   time.Time
	AlertPrice  float64 // the price it last alerted at
}

// AddWatch adds or updates a watch (one per item, colour and condition).
func (d *DB) AddWatch(w Watch) error {
	if w.MaxPrice <= 0 {
		return errors.New("the price limit must be more than zero")
	}
	if w.ItemType != "PART" && w.ItemType != "SET" {
		return fmt.Errorf("a watch is for a PART or a SET, not %q", w.ItemType)
	}
	if w.Cond != "N" && w.Cond != "U" {
		w.Cond = "U"
	}
	if w.ItemType == "SET" && !strings.Contains(w.ItemNo, "-") {
		w.ItemNo += "-1"
	}
	_, err := d.Exec(`INSERT INTO watchlist (item_type, item_no, color_id, color_name, cond, max_price) VALUES (?,?,?,?,?,?)
		ON CONFLICT(item_type, item_no, color_id, cond) DO UPDATE SET max_price = excluded.max_price, color_name = excluded.color_name`,
		w.ItemType, w.ItemNo, w.ColorID, w.ColorName, w.Cond, w.MaxPrice)
	return err
}

func scanWatch(row interface{ Scan(...any) error }) (Watch, error) {
	var w Watch
	var checked, alert string
	if err := row.Scan(&w.ID, &w.ItemType, &w.ItemNo, &w.ColorID, &w.ColorName, &w.Cond, &w.MaxPrice, &w.LastPrice, &checked, &alert, &w.AlertPrice); err != nil {
		return Watch{}, err
	}
	w.LastChecked, _ = time.Parse(timeLayout, checked)
	w.LastAlert, _ = time.Parse(timeLayout, alert)
	return w, nil
}

const watchCols = `id, item_type, item_no, color_id, color_name, cond, max_price, last_price, last_checked, last_alert, alert_price`

// ListWatches returns every watch, newest first.
func (d *DB) ListWatches() ([]Watch, error) {
	rows, err := d.Query(`SELECT ` + watchCols + ` FROM watchlist ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Watch
	for rows.Next() {
		w, err := scanWatch(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// RemoveWatch deletes a watch by id.
func (d *DB) RemoveWatch(id int64) error {
	res, err := d.Exec(`DELETE FROM watchlist WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("no watch with id %d", id)
	}
	return nil
}

// WatchHit is a watch whose price is at or under its limit.
type WatchHit struct {
	Watch
	Price    float64
	Currency string
}

// alertCooldown stops the same watch alerting again for a week unless the price falls further.
const alertCooldown = 7 * 24 * time.Hour

// CheckWatches prices every watch (through the fetcher, so the daily budget still
// applies), records the price, and returns the ones at or under their limit that
// have not already alerted recently. A price lower than the one it last alerted
// at alerts again at once. Any fetch error stops the check with hits so far kept.
func (d *DB) CheckWatches(ctx context.Context, f PriceFetcher) ([]WatchHit, error) {
	ws, err := d.ListWatches()
	if err != nil {
		return nil, err
	}
	var hits []WatchHit
	for _, w := range ws {
		bl := 0
		if w.ColorID >= 0 {
			if id, ok := d.BLColorFor(w.ColorID); ok {
				bl = id
			}
		}
		avg, cur, found, err := f.AvgPrice(ctx, w.ItemType, w.ItemNo, bl, w.Cond)
		if err != nil {
			return hits, err
		}
		now := time.Now().UTC().Format(timeLayout)
		if !found {
			_, _ = d.Exec(`UPDATE watchlist SET last_checked = ? WHERE id = ?`, now, w.ID)
			continue
		}
		_ = d.putPrice(PriceRow{ItemType: w.ItemType, ItemNo: w.ItemNo, BLColor: bl, Cond: w.Cond, Avg: avg, Currency: cur})
		_, _ = d.Exec(`UPDATE watchlist SET last_price = ?, last_checked = ? WHERE id = ?`, avg, now, w.ID)
		if avg > w.MaxPrice {
			continue
		}
		recent := !w.LastAlert.IsZero() && time.Since(w.LastAlert) < alertCooldown
		if recent && !(avg < w.AlertPrice) { // bouncing around the limit must not repeat the alert
			continue
		}
		_, _ = d.Exec(`UPDATE watchlist SET last_alert = ?, alert_price = ? WHERE id = ?`, now, avg, w.ID)
		hits = append(hits, WatchHit{Watch: w, Price: avg, Currency: cur})
	}
	return hits, nil
}

// LatestPrice returns the stored price for an item, or nil when none was ever fetched.
func (d *DB) LatestPrice(itemType, no string, blColor int, cond string) (*PriceRow, error) {
	return d.getPrice(itemType, no, blColor, cond)
}

// StorePrice records a price fetched elsewhere (the detail page's P key).
func (d *DB) StorePrice(itemType, no string, blColor int, cond string, avg float64, currency string, missing bool) error {
	return d.putPrice(PriceRow{ItemType: itemType, ItemNo: no, BLColor: blColor, Cond: cond, Avg: avg, Currency: currency, Missing: missing})
}
