package lego

import (
	"context"
	"strings"
	"time"
)

// Bookkeeping other API clients (BrickLink) share with the Rebrickable client: one
// response cache, one daily call budget and one request queue in lego.db, so every
// session, each its own process, counts against the same limits.

// APICacheGet returns a stored response younger than ttl.
func (d *DB) APICacheGet(key string, ttl time.Duration) (status int, body []byte, ok bool) {
	return d.cacheGet(key, ttl)
}

// APICachePut stores a response.
func (d *DB) APICachePut(key string, status int, body []byte) { d.cachePut(key, status, body) }

// ReserveAPISlot returns how long to wait before the named API's next request.
func (d *DB) ReserveAPISlot(name string, interval time.Duration) (time.Duration, error) {
	return d.reserveSlotKey("next_"+name, interval)
}

func budgetKey(name string, now time.Time) string {
	return "budget:" + name + ":" + now.UTC().Format("2006-01-02")
}

// SpendAPIBudget counts one call against today's (UTC) budget for name, atomically,
// unless the limit is already reached. Counters for earlier days are dropped.
func (d *DB) SpendAPIBudget(name string, limit int, now time.Time) (used int, allowed bool, err error) {
	ctx := context.Background()
	conn, err := d.Conn(ctx)
	if err != nil {
		return 0, false, err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return 0, false, err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(ctx, "ROLLBACK")
		}
	}()
	key := budgetKey(name, now)
	_, _ = conn.ExecContext(ctx, `DELETE FROM rb_meta WHERE key LIKE ? AND key != ?`, "budget:"+name+":%", key)
	_ = conn.QueryRowContext(ctx, `SELECT value FROM rb_meta WHERE key = ?`, key).Scan(&used)
	if used >= limit {
		_, err := conn.ExecContext(ctx, "COMMIT")
		committed = err == nil
		return used, false, err
	}
	used++
	if _, err := conn.ExecContext(ctx, `INSERT INTO rb_meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, used); err != nil {
		return 0, false, err
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return 0, false, err
	}
	committed = true
	return used, true, nil
}

// APIUsage is how many calls name has used today (UTC).
func (d *DB) APIUsage(name string, now time.Time) int {
	var n int
	_ = d.QueryRow(`SELECT value FROM rb_meta WHERE key = ?`, budgetKey(name, now)).Scan(&n)
	return n
}

// ---- BrickLink colour numbers ----

// BLColorRow maps a Rebrickable colour id to BrickLink's.
type BLColorRow struct {
	RBID int
	BLID int
	Name string
}

// SetBLColors replaces the colour-number map.
func (d *DB) SetBLColors(rows []BLColorRow) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM bl_colors`); err != nil {
		return err
	}
	for _, r := range rows {
		if _, err := tx.Exec(`INSERT OR REPLACE INTO bl_colors (rb_id, bl_id, name) VALUES (?,?,?)`, r.RBID, r.BLID, r.Name); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// BLColorFor returns BrickLink's colour number for a Rebrickable colour id.
func (d *DB) BLColorFor(rbID int) (int, bool) {
	var bl int
	if err := d.QueryRow(`SELECT bl_id FROM bl_colors WHERE rb_id = ?`, rbID).Scan(&bl); err != nil {
		return 0, false
	}
	return bl, true
}

// RBColorFor returns the Rebrickable colour for a BrickLink colour number.
func (d *DB) RBColorFor(blID int) (Color, bool) {
	var id int
	if err := d.QueryRow(`SELECT rb_id FROM bl_colors WHERE bl_id = ? ORDER BY rb_id LIMIT 1`, blID).Scan(&id); err != nil {
		return Color{}, false
	}
	return d.ColorByID(id)
}

// BLColorCount is how many colours are mapped.
func (d *DB) BLColorCount() int { return d.count("bl_colors") }

func normColor(s string) string {
	return strings.NewReplacer(" ", "", "-", "", "_", "", "'", "").Replace(strings.ToLower(s))
}

// MatchBLColorsByName pairs BrickLink colours with the catalog's by normalised name
// ("Light Bluish Gray" = "light-bluish-gray"). It is the fallback when Rebrickable's
// own BrickLink numbers are not available. Returns the pairs and the BrickLink
// names that matched nothing.
func (d *DB) MatchBLColorsByName(bl map[int]string) (rows []BLColorRow, unmatched []string, err error) {
	cols, err := d.CatalogColors()
	if err != nil {
		return nil, nil, err
	}
	byName := map[string]Color{}
	for _, c := range cols {
		byName[normColor(c.Name)] = c
	}
	for id, name := range bl {
		if c, ok := byName[normColor(name)]; ok {
			rows = append(rows, BLColorRow{RBID: c.ID, BLID: id, Name: c.Name})
		} else {
			unmatched = append(unmatched, name)
		}
	}
	return rows, unmatched, nil
}
