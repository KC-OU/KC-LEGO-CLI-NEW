package lego

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// BorrowedStages follow one borrowed part: taken from another set, replacement ordered from LEGO's
// replacement-parts service, replacement back in the set it was borrowed from.
var BorrowedStages = []string{"to order", "ordered", "replaced"}

// BorrowedPart: Build* / Need* is the set being built and the part it is short of; From* is the set the
// part was borrowed from (and the part that was taken, which can carry a different element id).
type BorrowedPart struct {
	ID                                           int64
	BuildSetNum, BuildSetName                    string
	NeedPartNum, NeedElementID                   string
	FromSetNum, FromSetName                      string
	FromPartNum, FromElementID                   string
	Qty                                          int
	Status, StatusAt, Note, CreatedBy, CreatedAt string
	// ColorID/ColorName say which colour of the part (ColorID -1: not recorded). CheckID is the check of
	// the set being built that the part was borrowed on (0: none yet), so its line can carry the note.
	ColorID   int
	ColorName string
	CheckID   int64
}

// partFrom is the part that was actually taken from the donor set (the needed part unless the donor's was
// a different element).
func (b BorrowedPart) partFrom() string {
	if b.FromPartNum != "" {
		return b.FromPartNum
	}
	return b.NeedPartNum
}

func cleanBorrowed(b *BorrowedPart) error {
	b.BuildSetNum, b.NeedPartNum = strings.TrimSpace(b.BuildSetNum), strings.TrimSpace(b.NeedPartNum)
	b.FromSetNum, b.FromPartNum = strings.TrimSpace(b.FromSetNum), strings.TrimSpace(b.FromPartNum)
	if b.BuildSetNum == "" || b.NeedPartNum == "" {
		return errors.New("the set being built and the part it needs are required")
	}
	if b.Qty < 1 {
		b.Qty = 1
	}
	if b.ColorID == 0 && b.ColorName == "" {
		b.ColorID = -1 // an unset colour is "not recorded", not colour 0 (black)
	}
	return nil
}

func insertBorrowed(tx *sql.Tx, b BorrowedPart) (int64, error) {
	now := time.Now().UTC().Format(timeLayout)
	res, err := tx.Exec(`INSERT INTO borrowed_parts (build_set_num, build_set_name, need_part_num, need_element_id, from_set_num, from_set_name,
		from_part_num, from_element_id, qty, status, status_at, note, created_by, created_at, color_id, color_name, check_id) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		b.BuildSetNum, strings.TrimSpace(b.BuildSetName), b.NeedPartNum, strings.TrimSpace(b.NeedElementID),
		b.FromSetNum, strings.TrimSpace(b.FromSetName), b.FromPartNum, strings.TrimSpace(b.FromElementID),
		b.Qty, BorrowedStages[0], now, b.Note, b.CreatedBy, now, b.ColorID, strings.TrimSpace(b.ColorName), b.CheckID)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// adjustDonorLent changes how many of the donor set's part are out on loan, on that set's last finished
// check, and refreshes its missing count. A donor that was never checked has nothing to adjust.
func adjustDonorLent(tx *sql.Tx, b BorrowedPart, delta int) error {
	if b.FromSetNum == "" {
		return nil
	}
	var checkID int64
	if err := tx.QueryRow(`SELECT last_check_id FROM set_state WHERE set_num = ?`, b.FromSetNum).Scan(&checkID); err != nil || checkID == 0 {
		return nil
	}
	if _, err := tx.Exec(`UPDATE set_check_lines SET lent_qty = MAX(0, lent_qty + ?) WHERE check_id = ? AND part_num = ? AND (? < 0 OR color_id = ?)`,
		delta, checkID, b.partFrom(), b.ColorID, b.ColorID); err != nil {
		return err
	}
	return refreshMissing(tx, b.FromSetNum, checkID)
}

// adjustBorrowerLine puts the borrowed quantity on the line of the set being built, on the given check
// (else its last finished one), and refreshes the missing count when that is the set's current result.
func adjustBorrowerLine(tx *sql.Tx, b BorrowedPart) error {
	var last int64
	_ = tx.QueryRow(`SELECT last_check_id FROM set_state WHERE set_num = ?`, b.BuildSetNum).Scan(&last)
	checkID := b.CheckID
	if checkID == 0 {
		checkID = last
	}
	if checkID == 0 {
		return nil
	}
	if _, err := tx.Exec(`UPDATE set_check_lines SET borrowed_qty = borrowed_qty + ?,
		borrowed_from = CASE WHEN ? = '' THEN borrowed_from WHEN borrowed_from = '' THEN ? WHEN instr(borrowed_from, ?) > 0 THEN borrowed_from ELSE borrowed_from || ', ' || ? END
		WHERE check_id = ? AND part_num = ? AND (? < 0 OR color_id = ?)`,
		b.Qty, b.FromSetNum, b.FromSetNum, b.FromSetNum, b.FromSetNum, checkID, b.NeedPartNum, b.ColorID, b.ColorID); err != nil {
		return err
	}
	if checkID == last {
		return refreshMissing(tx, b.BuildSetNum, checkID)
	}
	return nil
}

// AddBorrowedPart records a part borrowed from another set, on a check that is already saved: the line of
// the set being built counts it as present, and the donor set's line counts it as lent until it is replaced.
func (d *DB) AddBorrowedPart(b BorrowedPart) (int64, error) {
	if err := cleanBorrowed(&b); err != nil {
		return 0, err
	}
	tx, err := d.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	id, err := insertBorrowed(tx, b)
	if err != nil {
		return 0, err
	}
	if err := adjustBorrowerLine(tx, b); err != nil {
		return 0, err
	}
	if err := adjustDonorLent(tx, b, b.Qty); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// AddBorrowedForCheck is AddBorrowedPart for a check still being filled in (the TUI and the CLI): the line
// in memory takes the borrowed quantity, and the check saves it with everything else.
func (d *DB) AddBorrowedForCheck(c *SetCheck, b BorrowedPart) (int64, error) {
	b.BuildSetNum, b.CheckID = c.SetNum, c.ID
	if err := cleanBorrowed(&b); err != nil {
		return 0, err
	}
	line := -1
	for i, l := range c.Lines {
		if l.PartNum == b.NeedPartNum && (b.ColorID < 0 || l.ColorID == b.ColorID) {
			line = i
			break
		}
	}
	if line < 0 {
		return 0, fmt.Errorf("part %s is not on this check", b.NeedPartNum)
	}
	tx, err := d.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if b.ColorID < 0 {
		b.ColorID, b.ColorName = c.Lines[line].ColorID, c.Lines[line].ColorName
	}
	id, err := insertBorrowed(tx, b)
	if err != nil {
		return 0, err
	}
	if err := adjustDonorLent(tx, b, b.Qty); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	l := &c.Lines[line]
	l.Borrowed += b.Qty
	if b.FromSetNum != "" && !strings.Contains(l.BorrowedFrom, b.FromSetNum) {
		if l.BorrowedFrom != "" {
			l.BorrowedFrom += ", "
		}
		l.BorrowedFrom += b.FromSetNum
	}
	return id, nil
}

// refreshLent sets each line's Lent from the parts of this set still out on loan.
func (d *DB) refreshLent(setNum string, lines []CheckLine) {
	rows, err := d.Query(`SELECT from_part_num, need_part_num, color_id, qty FROM borrowed_parts WHERE from_set_num = ? AND status <> ?`, setNum, BorrowedStages[len(BorrowedStages)-1])
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var fromPart, needPart string
		var colour, qty int
		if rows.Scan(&fromPart, &needPart, &colour, &qty) != nil {
			continue
		}
		if fromPart == "" {
			fromPart = needPart
		}
		for i := range lines {
			if lines[i].PartNum == fromPart && (colour < 0 || lines[i].ColorID == colour) {
				lines[i].Lent += qty
				break
			}
		}
	}
}

// unrecordedBorrowed names the lines of a check that claim borrowed parts nobody recorded, so a check
// cannot be finished with parts marked borrowed but missing from the Borrowed list.
func unrecordedBorrowed(tx *sql.Tx, c *SetCheck) []string {
	var out []string
	for _, l := range c.Lines {
		if l.Borrowed <= 0 {
			continue
		}
		var recorded int
		_ = tx.QueryRow(`SELECT COALESCE(SUM(qty), 0) FROM borrowed_parts WHERE build_set_num = ? AND need_part_num = ? AND (color_id < 0 OR color_id = ?)`,
			c.SetNum, l.PartNum, l.ColorID).Scan(&recorded)
		if recorded < l.Borrowed {
			out = append(out, fmt.Sprintf("%s %s", l.PartNum, l.ColorName))
		}
	}
	return out
}

// BorrowedForSet lists what was borrowed to build setNum, oldest first (the order it was written down).
func (d *DB) BorrowedForSet(setNum string) ([]BorrowedPart, error) {
	all, err := d.ListBorrowedParts()
	if err != nil {
		return nil, err
	}
	var out []BorrowedPart
	for i := len(all) - 1; i >= 0; i-- {
		if all[i].BuildSetNum == setNum {
			out = append(out, all[i])
		}
	}
	return out, nil
}

// AdvanceBorrowedPart moves it to the next stage and returns the new one.
func (d *DB) AdvanceBorrowedPart(id int64) (string, error) {
	var stage string
	var b BorrowedPart
	if err := d.QueryRow(`SELECT status, need_part_num, from_set_num, from_part_num, qty, color_id FROM borrowed_parts WHERE id = ?`, id).
		Scan(&stage, &b.NeedPartNum, &b.FromSetNum, &b.FromPartNum, &b.Qty, &b.ColorID); err != nil {
		return "", fmt.Errorf("no borrowed part %d", id)
	}
	for i, s := range BorrowedStages {
		if s == stage {
			if i == len(BorrowedStages)-1 {
				return stage, errors.New("already replaced: nothing further to do")
			}
			next := BorrowedStages[i+1]
			tx, err := d.Begin()
			if err != nil {
				return "", err
			}
			defer tx.Rollback()
			if _, err := tx.Exec(`UPDATE borrowed_parts SET status = ?, status_at = ? WHERE id = ?`, next, time.Now().UTC().Format(timeLayout), id); err != nil {
				return "", err
			}
			if next == BorrowedStages[len(BorrowedStages)-1] { // replaced: the donor has it back
				if err := adjustDonorLent(tx, b, -b.Qty); err != nil {
					return "", err
				}
			}
			return next, tx.Commit()
		}
	}
	return "", fmt.Errorf("borrowed part %d is in unknown stage %q", id, stage)
}

// ListBorrowedParts returns the notes newest first.
func (d *DB) ListBorrowedParts() ([]BorrowedPart, error) {
	rows, err := d.Query(`SELECT id, build_set_num, build_set_name, need_part_num, need_element_id, from_set_num, from_set_name, from_part_num,
		from_element_id, qty, status, status_at, note, created_by, created_at, color_id, color_name, check_id FROM borrowed_parts ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BorrowedPart
	for rows.Next() {
		var b BorrowedPart
		if err := rows.Scan(&b.ID, &b.BuildSetNum, &b.BuildSetName, &b.NeedPartNum, &b.NeedElementID, &b.FromSetNum, &b.FromSetName, &b.FromPartNum,
			&b.FromElementID, &b.Qty, &b.Status, &b.StatusAt, &b.Note, &b.CreatedBy, &b.CreatedAt, &b.ColorID, &b.ColorName, &b.CheckID); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
