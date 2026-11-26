package lego

import (
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/partdb"
)

// Cycle counting: instead of one big stocktake, count a few lots at a time.
// CountsDue says which lots to count next (never counted first, then the
// longest ago; lots holding more pieces win ties); RecordCount saves what was
// counted, corrects the stock if it differs, and keeps the variance.

type CountDue struct {
	PartNum, ColorName string
	ColorID, Qty       int
	LastCounted        string // "" = never
	Location           string
}

// CountsDue returns up to limit owned lots not counted in the last olderThanDays days.
func (d *DB) CountsDue(pdb *partdb.DB, limit, olderThanDays int) ([]CountDue, error) {
	owned, err := d.ListOwnedParts()
	if err != nil {
		return nil, err
	}
	last := map[string]string{}
	rows, err := d.Query(`SELECT part_num, color_id, color_name, MAX(counted_at) FROM cycle_counts GROUP BY part_num, color_id, color_name`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var p, cn, at string
		var c int
		if rows.Scan(&p, &c, &cn, &at) == nil {
			last[countKey(p, c, cn)] = at
		}
	}
	rows.Close()
	cutoff := time.Now().AddDate(0, 0, -olderThanDays).UTC().Format(timeLayout)
	var due []CountDue
	for _, o := range owned {
		if o.Qty <= 0 {
			continue
		}
		at := last[countKey(o.PartNum, o.ColorID, o.ColorName)]
		if at != "" && at > cutoff {
			continue
		}
		due = append(due, CountDue{PartNum: o.PartNum, ColorName: o.ColorName, ColorID: o.ColorID, Qty: o.Qty, LastCounted: at})
	}
	sort.SliceStable(due, func(i, j int) bool {
		if due[i].LastCounted != due[j].LastCounted {
			return due[i].LastCounted < due[j].LastCounted // "" (never) sorts first
		}
		return due[i].Qty > due[j].Qty
	})
	if limit > 0 && len(due) > limit {
		due = due[:limit]
	}
	part, color, name := make([]string, len(due)), make([]int, len(due)), make([]string, len(due))
	for i, c := range due {
		part[i], color[i], name[i] = c.PartNum, c.ColorID, c.ColorName
	}
	for i, loc := range d.LocationNames(pdb, part, color, name) {
		due[i].Location = loc
	}
	return due, nil
}

func countKey(part string, color int, name string) string {
	if color >= 0 {
		name = ""
	}
	return part + "|" + strings.ToLower(name) + "|" + strconv.Itoa(color)
}

// RecordCount saves a count and returns the variance (counted - expected). If it
// differs from the stock held, the stock is corrected to what was counted (the
// change lands in the normal journal under the current actor).
func (d *DB) RecordCount(partNum string, colorID int, colorName string, counted int, by string) (variance int, err error) {
	if counted < 0 {
		return 0, errors.New("a count cannot be negative")
	}
	cur, err := d.GetOwnedPart(partNum, colorID, colorName)
	if err != nil || cur == nil {
		return 0, errors.New("that part and colour is not in your stock")
	}
	variance = counted - cur.Qty
	if _, err := d.Exec(`INSERT INTO cycle_counts (part_num, color_id, color_name, expected, counted, counted_by, counted_at) VALUES (?,?,?,?,?,?,?)`,
		partNum, colorID, colorName, cur.Qty, counted, by, time.Now().UTC().Format(timeLayout)); err != nil {
		return 0, err
	}
	if variance != 0 {
		cur.Qty = counted
		if err := d.AddOwnedPart(*cur); err != nil {
			return variance, err
		}
	}
	return variance, nil
}
