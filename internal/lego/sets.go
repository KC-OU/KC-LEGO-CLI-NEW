package lego

import (
	"database/sql"
	"fmt"
	"time"
)

const timeLayout = "2006-01-02T15:04:05.999999"

// UpsertSet inserts or updates a set keyed on SetNum — safe to call
// repeatedly (e.g. re-running `wms lego import` against newer export files),
// last write wins.
func (d *DB) UpsertSet(s Set) error {
	d.ensureDailySnapshot()
	before, existed := 0, false
	if cur, err := d.GetSetByNum(s.SetNum); err == nil && cur != nil {
		before, existed = cur.Qty, true
	}
	if err := d.upsertSet(s); err != nil {
		return err
	}
	switch {
	case !existed:
		d.journal(d.DB, "add", "set", s.SetNum, -1, "", 0, s.Qty, s.Name)
	case before != s.Qty:
		d.journal(d.DB, "change", "set", s.SetNum, -1, "", before, s.Qty, s.Name)
	}
	return nil
}

func (d *DB) upsertSet(s Set) error {
	partedOut := 0
	if s.PartedOut {
		partedOut = 1
	}
	if s.PartedOutInfo == "" {
		s.PartedOutInfo = "{}"
	}
	now := time.Now().UTC().Format(timeLayout)
	createdAt := s.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	_, err := d.Exec(`
		INSERT INTO sets (set_num, name, theme, year, instruction_book_number, instruction_book_count,
			qty, parts_qty, parted_out, parted_out_info, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(set_num) DO UPDATE SET
			name=excluded.name, theme=excluded.theme, year=excluded.year,
			instruction_book_number=excluded.instruction_book_number,
			instruction_book_count=excluded.instruction_book_count,
			qty=excluded.qty, parts_qty=excluded.parts_qty,
			parted_out=excluded.parted_out, parted_out_info=excluded.parted_out_info,
			updated_at=excluded.updated_at`,
		s.SetNum, s.Name, s.Theme, s.Year, s.InstructionBookNumber, s.InstructionBookCount,
		s.Qty, s.PartsQty, partedOut, s.PartedOutInfo,
		createdAt.Format(timeLayout), now)
	if err != nil {
		return fmt.Errorf("upserting set %s: %w", s.SetNum, err)
	}
	return nil
}

func scanSet(row interface{ Scan(...any) error }) (Set, error) {
	var s Set
	var partedOut int
	var createdAt, updatedAt string
	err := row.Scan(&s.ID, &s.SetNum, &s.Name, &s.Theme, &s.Year, &s.InstructionBookNumber,
		&s.InstructionBookCount, &s.Qty, &s.PartsQty, &partedOut, &s.PartedOutInfo, &createdAt, &updatedAt)
	if err != nil {
		return Set{}, err
	}
	s.PartedOut = partedOut != 0
	s.CreatedAt, _ = time.Parse(timeLayout, createdAt)
	s.UpdatedAt, _ = time.Parse(timeLayout, updatedAt)
	return s, nil
}

const setColumns = `id, set_num, name, theme, year, instruction_book_number, instruction_book_count,
	qty, parts_qty, parted_out, parted_out_info, created_at, updated_at`

// SearchSets does a name/theme/set_num LIKE match, newest-updated first. An
// empty term returns everything (bounded to 200 rows — a personal
// collection, not a paginated catalog).
func (d *DB) SearchSets(term string) ([]Set, error) {
	like := "%" + term + "%"
	rows, err := d.Query(`SELECT `+setColumns+` FROM sets
		WHERE (? = '' OR name LIKE ? OR theme LIKE ? OR set_num LIKE ?)
		ORDER BY updated_at DESC LIMIT 200`, term, like, like, like)
	if err != nil {
		return nil, fmt.Errorf("searching sets: %w", err)
	}
	defer rows.Close()

	var out []Set
	for rows.Next() {
		s, err := scanSet(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (d *DB) GetSetByNum(setNum string) (*Set, error) {
	row := d.QueryRow(`SELECT `+setColumns+` FROM sets WHERE set_num = ?`, setNum)
	s, err := scanSet(row)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("set %s not found in collection", setNum)
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// MarkPartedOut flips the parted-out flag on an owned set.
func (d *DB) MarkPartedOut(setNum string, partedOut bool) error {
	v := 0
	if partedOut {
		v = 1
	}
	res, err := d.Exec(`UPDATE sets SET parted_out = ?, updated_at = ? WHERE set_num = ?`,
		v, time.Now().UTC().Format(timeLayout), setNum)
	if err != nil {
		return fmt.Errorf("updating parted-out status for %s: %w", setNum, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("set %s not found in collection", setNum)
	}
	return nil
}
