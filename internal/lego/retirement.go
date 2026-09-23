package lego

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Retirement dates aren't in Rebrickable — this reads the community-maintained
// "Brick Tap" LEGO retirement-tracking spreadsheet (or any CSV sharing its
// column shape: Theme, Subtheme, Set #, Set Name, Age, Piece Count, Retirement
// Date, Notes, LEGO.com Link), matched by header name so a reordered or
// hand-edited copy still imports. It's someone's personal public document, not
// an official API, so every use here is fail-soft: a bad or unreachable fetch
// never blocks anything else, and `wms lego retirement import <csv>` is the
// fallback if the live sheet is ever down, moved or restructured.

type RetirementInfo struct {
	SetNum, Theme, Subtheme, Name, Notes string
	RetiresAt                            time.Time // zero = unknown/not yet dated
}

var retirementDateLayouts = []string{"Jan 2 2006", "Jan 2, 2006", "2006-01-02"}

func parseRetirementDate(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" || s == "—" || s == "-" {
		return time.Time{}
	}
	for _, layout := range retirementDateLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// ParseRetirementCSV reads rows by header name (case-insensitive), tolerating
// extra or reordered columns. Only "Set #" is required; everything else is
// best-effort.
func ParseRetirementCSV(r io.Reader) ([]RetirementInfo, error) {
	cr := csv.NewReader(bufio.NewReader(r))
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true

	// The sheet's real header isn't necessarily row 1 — a title, a last-updated note or a
	// blank row often comes first (the live Brick Tap export does exactly this) — so the
	// first 10 rows are searched for one that actually names a "Set #" column, rather than
	// assuming row 1 is it.
	idx := func(header []string, names ...string) int {
		col := map[string]int{}
		for i, h := range header {
			col[strings.ToLower(strings.TrimSpace(h))] = i
		}
		for _, n := range names {
			if i, ok := col[n]; ok {
				return i
			}
		}
		return -1
	}
	var header []string
	setCol, sawARow := -1, false
	for tries := 0; setCol < 0 && tries < 10; tries++ {
		row, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading header: %w", err)
		}
		sawARow = true
		if c := idx(row, "set #", "set num", "set number", "set"); c >= 0 {
			header, setCol = row, c
		}
	}
	if !sawARow {
		return nil, nil // a genuinely empty file, not a malformed one
	}
	if setCol < 0 {
		return nil, fmt.Errorf(`no "Set #" column found in the first 10 rows`)
	}
	nameCol := idx(header, "set name", "name")
	themeCol := idx(header, "theme")
	subCol := idx(header, "subtheme")
	dateCol := idx(header, "retirement date", "retires", "retirement")
	notesCol := idx(header, "notes:", "notes: (exclusivity, release, etc.)", "notes")

	get := func(row []string, i int) string {
		if i < 0 || i >= len(row) {
			return ""
		}
		s := strings.TrimSpace(row[i])
		if s == "—" {
			return ""
		}
		return s
	}
	var out []RetirementInfo
	for {
		row, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			continue // one malformed row is skipped, not a reason to abort the whole sheet
		}
		num := get(row, setCol)
		if num == "" {
			continue
		}
		out = append(out, RetirementInfo{
			SetNum: num, Theme: get(row, themeCol), Subtheme: get(row, subCol),
			Name: get(row, nameCol), Notes: get(row, notesCol),
			RetiresAt: parseRetirementDate(get(row, dateCol)),
		})
	}
	return out, nil
}

// FetchRetirementCSV downloads and parses url. Treat a non-nil error as
// soft — log or warn, and keep whatever was imported last; nothing else in
// this package depends on it succeeding.
func FetchRetirementCSV(ctx context.Context, url string) ([]RetirementInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching %s: HTTP %d", url, resp.StatusCode)
	}
	return ParseRetirementCSV(resp.Body)
}

// ImportRetirements replaces every stored retirement record with rows — the
// sheet (or a manually imported CSV) is the source of truth, so there is
// nothing to merge line by line.
func (d *DB) ImportRetirements(rows []RetirementInfo) (int, error) {
	tx, err := d.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM retirements`); err != nil {
		return 0, err
	}
	now := time.Now().Format(timeLayout)
	// OR REPLACE: the real sheet lists the same set number more than once (e.g. one row per
	// region/exclusivity note) — the last row for a given set wins rather than the whole
	// import failing on a duplicate.
	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO retirements (set_num, theme, subtheme, set_name, retirement_date, notes, updated_at) VALUES (?,?,?,?,?,?,?)`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()
	for _, r := range rows {
		date := ""
		if !r.RetiresAt.IsZero() {
			date = r.RetiresAt.Format("2006-01-02")
		}
		if _, err := stmt.Exec(r.SetNum, r.Theme, r.Subtheme, r.Name, date, r.Notes, now); err != nil {
			return 0, err
		}
	}
	// A distinct count, not len(rows): the sheet repeats a set number across several rows
	// (one per region/exclusivity note — see the OR REPLACE above), so counting input rows
	// would overstate how many sets are actually stored.
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM retirements`).Scan(&n); err != nil {
		return 0, err
	}
	return n, tx.Commit()
}

// RetirementFor is one set's retirement record, or nil when it isn't in the
// imported data (never imported, or the set just isn't in the sheet).
func (d *DB) RetirementFor(setNum string) (*RetirementInfo, error) {
	var r RetirementInfo
	var date string
	err := d.QueryRow(`SELECT set_num, theme, subtheme, set_name, retirement_date, notes FROM retirements WHERE set_num IN (?, ?)`,
		setNum, strings.TrimSuffix(setNum, "-1")).
		Scan(&r.SetNum, &r.Theme, &r.Subtheme, &r.Name, &date, &r.Notes)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.RetiresAt = parseRetirementDate(date)
	return &r, nil
}

// RetiringSet is one retirement row plus how you're tracking that set today.
type RetiringSet struct {
	RetirementInfo
	Owned    bool // a copy is in your `sets` table (qty > 0)
	Watching bool // you have a price watch on it (wms lego watch)
}

// RetiringSoon lists dated sets retiring within `within` from now, soonest
// first, each flagged with whether you own or are watching it — the single
// query behind "flag it on what I own", "flag it on my watches" and "let me
// just browse what's retiring", since all three are this one list read three
// different ways.
func (d *DB) RetiringSoon(within time.Duration) ([]RetiringSet, error) {
	cutoff := time.Now().Add(within).Format("2006-01-02")
	// Set numbers aren't stored consistently across tables (`sets` keeps whatever was given;
	// `watchlist` always appends "-1"; the retirement sheet never has a suffix at all) — matched
	// the same way reference.go's set lookup does, by checking both the bare and "-1" forms.
	rows, err := d.Query(`
		SELECT r.set_num, r.theme, r.subtheme, r.set_name, r.retirement_date, r.notes,
			EXISTS(SELECT 1 FROM sets s WHERE s.qty > 0 AND s.set_num IN (r.set_num, r.set_num || '-1')),
			EXISTS(SELECT 1 FROM watchlist w WHERE w.item_type = 'SET' AND w.item_no IN (r.set_num, r.set_num || '-1'))
		FROM retirements r
		WHERE r.retirement_date != '' AND r.retirement_date <= ?
		ORDER BY r.retirement_date`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RetiringSet
	for rows.Next() {
		var rs RetiringSet
		var date string
		if err := rows.Scan(&rs.SetNum, &rs.Theme, &rs.Subtheme, &rs.Name, &date, &rs.Notes, &rs.Owned, &rs.Watching); err != nil {
			return nil, err
		}
		rs.RetiresAt = parseRetirementDate(date)
		out = append(out, rs)
	}
	return out, rows.Err()
}

// RetirementDataAge is when the retirement table was last (re)imported, or
// the zero time if it has never been imported.
func (d *DB) RetirementDataAge() time.Time {
	var s string
	if err := d.QueryRow(`SELECT updated_at FROM retirements ORDER BY updated_at DESC LIMIT 1`).Scan(&s); err != nil {
		return time.Time{}
	}
	t, _ := time.Parse(timeLayout, s)
	return t
}
