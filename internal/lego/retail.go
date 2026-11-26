package lego

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
)

// SetRetail is what LEGO.com/Brickset says about a set's shelf life.
type SetRetail struct {
	SetNum, Name, Theme, LaunchDate, ExitDate, Availability string
	Year, Pieces                                            int
	RetailUK, RetailUS                                      float64
	LastUK, LastUS                                          string // LEGO.com's last-available date per market
	FetchedAt                                               string
}

// RetailChange describes something worth telling the owner about.
type RetailChange struct {
	SetNum, Name, What string
}

// SaveSetRetail upserts rows and reports meaningful changes against what was
// stored: a first-ever exit date, a moved exit date, or availability turning
// into a retiring/retired state. A set seen for the first time with no exit
// date yet is silent (nothing to say).
func (d *DB) SaveSetRetail(rows []SetRetail) ([]RetailChange, error) {
	var changes []RetailChange
	now := time.Now().UTC().Format(timeLayout)
	for _, r := range rows {
		var oldExit, oldAvail string
		err := d.QueryRow(`SELECT exit_date, availability FROM set_retail WHERE set_num = ?`, r.SetNum).Scan(&oldExit, &oldAvail)
		known := err == nil
		if err != nil && err != sql.ErrNoRows {
			return changes, err
		}
		switch {
		case r.ExitDate != "" && r.ExitDate != oldExit && oldExit == "":
			changes = append(changes, RetailChange{r.SetNum, r.Name, "now has an exit date: " + r.ExitDate})
		case r.ExitDate != "" && oldExit != "" && r.ExitDate != oldExit:
			changes = append(changes, RetailChange{r.SetNum, r.Name, fmt.Sprintf("exit date moved %s -> %s", oldExit, r.ExitDate)})
		case known && r.Availability != oldAvail && isRetiringText(r.Availability):
			changes = append(changes, RetailChange{r.SetNum, r.Name, "availability is now: " + r.Availability})
		}
		if _, err := d.Exec(`INSERT INTO set_retail (set_num, name, year, theme, pieces, launch_date, exit_date, availability, retail_uk, retail_us, last_uk, last_us, fetched_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(set_num) DO UPDATE SET name=excluded.name, year=excluded.year, theme=excluded.theme, pieces=excluded.pieces,
			launch_date=excluded.launch_date, exit_date=excluded.exit_date, availability=excluded.availability, retail_uk=excluded.retail_uk,
			retail_us=excluded.retail_us, last_uk=excluded.last_uk, last_us=excluded.last_us, fetched_at=excluded.fetched_at`,
			r.SetNum, r.Name, r.Year, r.Theme, r.Pieces, r.LaunchDate, r.ExitDate, r.Availability, r.RetailUK, r.RetailUS, r.LastUK, r.LastUS, now); err != nil {
			return changes, err
		}
	}
	return changes, nil
}

func isRetiringText(s string) bool {
	s = strings.ToLower(s)
	return strings.Contains(s, "retir") || strings.Contains(s, "last chance")
}

// TrackedSetNums is what the daily Brickset refresh looks up: sets you own, sets
// on a price watch, and everything the retirement sheet dates (all as "{n}-{v}").
func (d *DB) TrackedSetNums() ([]string, error) {
	rows, err := d.Query(`SELECT set_num FROM sets WHERE qty > 0
		UNION SELECT item_no FROM watchlist WHERE item_type = 'SET'
		UNION SELECT set_num FROM retirements WHERE retirement_date != ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	var out []string
	for rows.Next() {
		var n string
		if rows.Scan(&n) != nil || strings.TrimSpace(n) == "" {
			continue
		}
		if !strings.Contains(n, "-") {
			n += "-1"
		}
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out, rows.Err()
}

// RetiredRow is one line of the Retired Sets page: the sheet's date, Brickset's
// date and LEGO.com availability, retail price, and whether you own the set.
type RetiredRow struct {
	SetNum, Name, Theme string
	SheetDate           string // from the community sheet / an uploaded file
	ExitDate            string // from Brickset (LEGO.com)
	Availability        string
	RetailUK, RetailUS  float64
	Owned               bool
}

// RetiredView merges the retirement sheet with Brickset's data for sets that
// retire (by either source) on or before `within` from now, soonest first.
// Sets whose date has already passed stay in, so "recently retired" is visible.
func (d *DB) RetiredView(within time.Duration) ([]RetiredRow, error) {
	cutoff := time.Now().Add(within).Format("2006-01-02")
	rows, err := d.Query(`
		SELECT COALESCE(r.set_num, s.set_num), COALESCE(NULLIF(r.set_name,''), s.name, ''), COALESCE(NULLIF(r.theme,''), s.theme, ''),
			COALESCE(r.retirement_date,''), COALESCE(s.exit_date,''), COALESCE(s.availability,''), COALESCE(s.retail_uk,0), COALESCE(s.retail_us,0),
			EXISTS(SELECT 1 FROM sets o WHERE o.qty > 0 AND o.set_num IN (COALESCE(r.set_num, s.set_num), COALESCE(r.set_num, s.set_num) || '-1'))
		FROM retirements r LEFT JOIN set_retail s ON s.set_num IN (r.set_num, r.set_num || '-1')
		UNION
		SELECT s.set_num, s.name, s.theme, '', s.exit_date, s.availability, s.retail_uk, s.retail_us,
			EXISTS(SELECT 1 FROM sets o WHERE o.qty > 0 AND o.set_num IN (s.set_num, REPLACE(s.set_num, '-1', '')))
		FROM set_retail s WHERE s.exit_date != '' AND NOT EXISTS (SELECT 1 FROM retirements r WHERE s.set_num IN (r.set_num, r.set_num || '-1'))`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RetiredRow
	for rows.Next() {
		var r RetiredRow
		if err := rows.Scan(&r.SetNum, &r.Name, &r.Theme, &r.SheetDate, &r.ExitDate, &r.Availability, &r.RetailUK, &r.RetailUS, &r.Owned); err != nil {
			return nil, err
		}
		if when := firstNonEmptyStr(r.ExitDate, r.SheetDate); when != "" && when <= cutoff {
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return firstNonEmptyStr(out[i].ExitDate, out[i].SheetDate) < firstNonEmptyStr(out[j].ExitDate, out[j].SheetDate)
	})
	return out, rows.Err()
}

func firstNonEmptyStr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
