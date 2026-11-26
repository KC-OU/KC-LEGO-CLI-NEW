package lego

import (
	"math"
	"sort"
	"time"
)

// Mover is an owned part whose average price changed notably.
type Mover struct {
	PartNum, Label string
	Qty            int
	From, To       float64
	Pct, Value     float64 // Value is the change across the quantity you hold
}

// PriceMovers compares each owned part's latest price with the newest one at
// least sinceDays old, and returns those that moved by at least minPct percent
// AND at least minValue across the quantity held (so a 40% move on a 2p part
// you own one of stays quiet). Biggest value change first.
func (d *DB) PriceMovers(cond string, sinceDays int, minPct, minValue float64) ([]Mover, error) {
	targets, err := d.priceTargets()
	if err != nil {
		return nil, err
	}
	cutoff := time.Now().AddDate(0, 0, -sinceDays).Format("2006-01-02")
	var out []Mover
	for _, k := range targets {
		if k.itemType != "PART" {
			continue
		}
		var to, from float64
		if d.QueryRow(`SELECT avg FROM price_history WHERE item_type=? AND item_no=? AND bl_color=? AND cond=? ORDER BY day DESC LIMIT 1`,
			k.itemType, k.no, k.blColor, cond).Scan(&to) != nil {
			continue
		}
		if d.QueryRow(`SELECT avg FROM price_history WHERE item_type=? AND item_no=? AND bl_color=? AND cond=? AND day <= ? ORDER BY day DESC LIMIT 1`,
			k.itemType, k.no, k.blColor, cond, cutoff).Scan(&from) != nil || from <= 0 {
			continue
		}
		pct := (to - from) / from * 100
		val := (to - from) * float64(k.qty)
		if math.Abs(pct) >= minPct && math.Abs(val) >= minValue {
			out = append(out, Mover{PartNum: k.no, Label: k.label, Qty: k.qty, From: from, To: to, Pct: pct, Value: val})
		}
	}
	sort.Slice(out, func(i, j int) bool { return math.Abs(out[i].Value) > math.Abs(out[j].Value) })
	return out, nil
}

// StoreLotIn is one lot of your BrickLink store as read from its API.
type StoreLotIn struct {
	InventoryID       int
	ItemType, ItemNo  string
	BLColor, Qty      int
	Cond, Description string
	UnitPrice         float64
}

// ReplaceStoreLots swaps in a fresh copy of the store's lots.
func (d *DB) ReplaceStoreLots(lots []StoreLotIn) error {
	if _, err := d.Exec(`DELETE FROM store_lots`); err != nil {
		return err
	}
	now := time.Now().UTC().Format(timeLayout)
	for _, l := range lots {
		if _, err := d.Exec(`INSERT OR REPLACE INTO store_lots (inventory_id, item_type, item_no, bl_color, cond, qty, unit_price, description, synced_at) VALUES (?,?,?,?,?,?,?,?,?)`,
			l.InventoryID, l.ItemType, l.ItemNo, l.BLColor, l.Cond, l.Qty, l.UnitPrice, l.Description, now); err != nil {
			return err
		}
	}
	return nil
}

// Repricing is a suggested new price for one store lot.
type Repricing struct {
	InventoryID     int
	ItemNo          string
	BLColor, Qty    int
	Cond            string
	Current, Market float64
	Suggested       float64
	DiffPct         float64 // how far Current is from Suggested, % of Suggested (negative = underpriced)
}

// RepriceSuggestions proposes marketPct% of the stored average price (100 = at
// market, 110 = 10% above) for every store lot we hold a price for, returning
// only lots whose current price is more than tolerancePct away from that.
// It never changes anything -- it is a list to approve and apply on BrickLink.
func (d *DB) RepriceSuggestions(marketPct, tolerancePct float64) ([]Repricing, error) {
	rows, err := d.Query(`SELECT inventory_id, item_type, item_no, bl_color, cond, qty, unit_price FROM store_lots WHERE qty > 0`)
	if err != nil {
		return nil, err
	}
	type lot struct {
		id, color, qty int
		typ, no, cond  string
		price          float64
	}
	var lots []lot
	for rows.Next() {
		var l lot
		if err := rows.Scan(&l.id, &l.typ, &l.no, &l.color, &l.cond, &l.qty, &l.price); err != nil {
			rows.Close()
			return nil, err
		}
		lots = append(lots, l)
	}
	rows.Close()
	var out []Repricing
	for _, l := range lots {
		p, err := d.getPrice(l.typ, l.no, l.color, l.cond)
		if err != nil || p == nil || p.Missing || p.Avg <= 0 {
			continue
		}
		sug := math.Round(p.Avg*marketPct/100*1000) / 1000
		diff := (l.price - sug) / sug * 100
		if math.Abs(diff) > tolerancePct {
			out = append(out, Repricing{l.id, l.no, l.color, l.qty, l.cond, l.price, p.Avg, sug, diff})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return math.Abs(out[i].DiffPct*float64(out[i].Qty)) > math.Abs(out[j].DiffPct*float64(out[j].Qty))
	})
	return out, nil
}
