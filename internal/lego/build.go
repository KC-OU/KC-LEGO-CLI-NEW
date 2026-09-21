package lego

import (
	"fmt"
)

// BuildOptions filters "what can I build".
type BuildOptions struct {
	MinPercent int // only sets where you hold at least this % of the pieces (default 60)
	MinPieces  int // ignore tiny sets (default 20): a polybag you "can build" says nothing
	MaxMissing int // at most this many pieces missing (0 = no limit)
	Limit      int // how many to return (default 30)
}

// BuildResult is one set and how much of it your loose parts cover.
type BuildResult struct {
	SetNum  string
	Name    string
	Theme   string
	Year    int
	Total   int // pieces the set needs (spares excluded)
	Have    int // of those, how many you hold in the right part and colour
	Missing int
	Percent int
}

// CanBuild ranks the catalog's sets by how much of each your loose parts cover,
// entirely offline. It matches exact part number and colour (equivalents apply
// when you look at one set with `missing`, which is where they are named and
// can be checked). Only your parts with a real colour can match.
func (d *DB) CanBuild(o BuildOptions) ([]BuildResult, error) {
	if o.MinPercent <= 0 {
		o.MinPercent = 60
	}
	if o.MinPieces <= 0 {
		o.MinPieces = 20
	}
	if o.Limit <= 0 {
		o.Limit = 30
	}
	var exists int
	// An existence check: COUNT(*) over 1.4 million rows would cost a second.
	if err := d.QueryRow(`SELECT 1 FROM cat_inventory_parts LIMIT 1`).Scan(&exists); err != nil {
		return nil, fmt.Errorf("the offline catalog has no set contents yet: run `wms lego catalog refresh`")
	}
	rows, err := d.Query(`
		WITH mine AS (
			SELECT part_num, color_id, SUM(qty) AS qty FROM owned_parts WHERE color_id >= 0 AND qty > 0 GROUP BY part_num, color_id),
		latest AS (
			SELECT i.id, i.set_num FROM cat_inventories i
			WHERE i.version = (SELECT MAX(version) FROM cat_inventories WHERE set_num = i.set_num)),
		per_row AS (
			SELECT ip.inventory_id, ip.part_num, ip.color_id, SUM(ip.quantity) AS q, mine.qty AS held
			FROM mine JOIN cat_inventory_parts ip ON ip.part_num = mine.part_num AND ip.color_id = mine.color_id
			GROUP BY ip.inventory_id, ip.part_num, ip.color_id),
		have AS (
			SELECT inventory_id, SUM(MIN(q, held)) AS have FROM per_row GROUP BY inventory_id),
		total AS (
			SELECT inventory_id, SUM(quantity) AS total FROM cat_inventory_parts
			WHERE inventory_id IN (SELECT inventory_id FROM have) GROUP BY inventory_id)
		SELECT s.set_num, s.name, s.year, s.theme_id, t.total, h.have
		FROM latest l
		JOIN have h ON h.inventory_id = l.id
		JOIN total t ON t.inventory_id = l.id
		JOIN cat_sets s ON s.set_num = l.set_num
		WHERE t.total >= ?1 AND h.have * 100 >= ?2 * t.total AND (?3 = 0 OR t.total - h.have <= ?3)
		ORDER BY (h.have * 1000 / t.total) DESC, t.total DESC, s.year DESC
		LIMIT ?4`, o.MinPieces, o.MinPercent, o.MaxMissing, o.Limit)
	if err != nil {
		return nil, fmt.Errorf("working out what you can build: %w", err)
	}
	defer rows.Close()
	var out []BuildResult
	var themeIDs []int
	for rows.Next() {
		var r BuildResult
		var tid int
		if err := rows.Scan(&r.SetNum, &r.Name, &r.Year, &tid, &r.Total, &r.Have); err != nil {
			return nil, err
		}
		r.Missing = r.Total - r.Have
		r.Percent = r.Have * 100 / r.Total
		out = append(out, r)
		themeIDs = append(themeIDs, tid)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Theme = d.ThemePath(themeIDs[i])
	}
	return out, nil
}
