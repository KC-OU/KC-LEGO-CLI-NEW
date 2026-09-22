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

// A set check goes through a set's parts list: every line starts as "have all",
// and you mark what is missing and what is extra. The intake check runs when a set
// is added; a recount is a stock check later. Finishing a check records who did it,
// updates what the set is short, and turns extras into loose parts that remember
// the set they came from — so another set missing that part can find them.

const (
	CheckIntake  = "intake"
	CheckRecount = "recount"
	StatusDraft  = "draft"
	StatusDone   = "done"
)

type CheckLine struct {
	PartNum, PartName, Category string
	ColorID                     int
	ColorName, BLID             string
	BLColor                     int
	Need, Have, Extra           int
}

// Missing is how many of the line the set is short.
func (l CheckLine) Missing() int { return max(0, l.Need-l.Have) }

type SetCheck struct {
	ID                   int64
	SetNum, Kind, Status string
	CheckedBy            string
	StartedAt            time.Time
	FinishedAt           time.Time
	Lines                []CheckLine
}

// Totals are the check's piece counts.
func (c *SetCheck) Totals() (pieces, have, missing, extra, missingLines int) {
	for _, l := range c.Lines {
		pieces += l.Need
		have += min(l.Have, l.Need)
		missing += l.Missing()
		extra += l.Extra
		if l.Missing() > 0 {
			missingLines++
		}
	}
	return
}

// NewCheck starts a check of setNum. An unfinished draft is resumed. An intake starts
// every line at "have all"; a recount starts from what the last finished check found.
func (d *DB) NewCheck(ctx context.Context, rb *Client, setNum, kind, by string) (*SetCheck, error) {
	if c, err := d.draftCheck(setNum, kind); err != nil || c != nil {
		return c, err
	}
	c := &SetCheck{SetNum: setNum, Kind: kind, Status: StatusDraft, CheckedBy: by, StartedAt: time.Now()}
	if kind == CheckRecount {
		if last, err := d.LastCheck(setNum); err == nil && last != nil {
			c.Lines = last.Lines
			for i := range c.Lines {
				c.Lines[i].Extra = 0
			}
			return c, nil
		}
	}
	inv := d.LookupSetInventory(ctx, rb, setNum)
	if len(inv.Items) == 0 {
		return nil, fmt.Errorf("no parts list for set %s: %s", setNum, strings.Join(inv.Notes, " "))
	}
	copies := 1
	if s, err := d.GetSetByNum(strings.TrimSuffix(setNum, "-1")); err == nil && s != nil && s.Qty > 1 {
		copies = s.Qty
	}
	cats := map[string]string{}
	seen := map[[2]any]int{}
	for _, it := range inv.Items {
		if it.Qty <= 0 {
			continue
		}
		cat, ok := cats[it.PartNum]
		if !ok {
			if cp, _ := d.CatalogPart(it.PartNum); cp != nil {
				cat = cp.Category
			}
			cats[it.PartNum] = cat
		}
		n := it.Qty * copies
		if i, ok := seen[[2]any{it.PartNum, it.ColorID}]; ok { // the same part+colour listed twice: one line
			c.Lines[i].Need += n
			c.Lines[i].Have += n
			continue
		}
		seen[[2]any{it.PartNum, it.ColorID}] = len(c.Lines)
		c.Lines = append(c.Lines, CheckLine{PartNum: it.PartNum, PartName: it.PartName, Category: cat, ColorID: it.ColorID,
			ColorName: it.ColorName, BLID: it.BrickLinkID, BLColor: it.BLColor, Need: n, Have: n})
	}
	sort.SliceStable(c.Lines, func(i, j int) bool {
		if c.Lines[i].ColorName != c.Lines[j].ColorName {
			return c.Lines[i].ColorName < c.Lines[j].ColorName
		}
		return c.Lines[i].PartNum < c.Lines[j].PartNum
	})
	return c, nil
}

func (d *DB) draftCheck(setNum, kind string) (*SetCheck, error) {
	var id int64
	err := d.QueryRow(`SELECT id FROM set_checks WHERE set_num = ? AND kind = ? AND status = ? ORDER BY id DESC LIMIT 1`, setNum, kind, StatusDraft).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return d.GetCheck(id)
}

// GetCheck loads a check with its lines.
func (d *DB) GetCheck(id int64) (*SetCheck, error) {
	c := &SetCheck{ID: id}
	var started, finished string
	if err := d.QueryRow(`SELECT set_num, kind, status, checked_by, started_at, finished_at FROM set_checks WHERE id = ?`, id).
		Scan(&c.SetNum, &c.Kind, &c.Status, &c.CheckedBy, &started, &finished); err != nil {
		return nil, err
	}
	c.StartedAt, _ = time.Parse(timeLayout, started)
	c.FinishedAt, _ = time.Parse(timeLayout, finished)
	rows, err := d.Query(`SELECT part_num, part_name, category, color_id, color_name, bl_id, bl_color, need, have, extra
		FROM set_check_lines WHERE check_id = ? ORDER BY color_name, part_num`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var l CheckLine
		if err := rows.Scan(&l.PartNum, &l.PartName, &l.Category, &l.ColorID, &l.ColorName, &l.BLID, &l.BLColor, &l.Need, &l.Have, &l.Extra); err != nil {
			return nil, err
		}
		c.Lines = append(c.Lines, l)
	}
	return c, rows.Err()
}

// LastCheck is the set's most recent finished check (nil if never checked).
func (d *DB) LastCheck(setNum string) (*SetCheck, error) {
	var id int64
	err := d.QueryRow(`SELECT id FROM set_checks WHERE set_num = ? AND status = ? ORDER BY id DESC LIMIT 1`, setNum, StatusDone).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return d.GetCheck(id)
}

// SaveCheck stores the check (a draft to resume, or as it stands).
func (d *DB) SaveCheck(c *SetCheck) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := saveCheck(tx, c); err != nil {
		return err
	}
	return tx.Commit()
}

func saveCheck(tx *sql.Tx, c *SetCheck) error {
	pieces, have, missing, extra, _ := c.Totals()
	finished := ""
	if !c.FinishedAt.IsZero() {
		finished = c.FinishedAt.UTC().Format(timeLayout)
	}
	if c.ID == 0 {
		res, err := tx.Exec(`INSERT INTO set_checks (set_num, kind, status, checked_by, started_at, finished_at, lines, pieces, have, missing, extra)
			VALUES (?,?,?,?,?,?,?,?,?,?,?)`, c.SetNum, c.Kind, c.Status, c.CheckedBy, c.StartedAt.UTC().Format(timeLayout), finished, len(c.Lines), pieces, have, missing, extra)
		if err != nil {
			return fmt.Errorf("saving the check: %w", err)
		}
		c.ID, _ = res.LastInsertId()
	} else if _, err := tx.Exec(`UPDATE set_checks SET status = ?, checked_by = ?, finished_at = ?, lines = ?, pieces = ?, have = ?, missing = ?, extra = ? WHERE id = ?`,
		c.Status, c.CheckedBy, finished, len(c.Lines), pieces, have, missing, extra, c.ID); err != nil {
		return fmt.Errorf("saving the check: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM set_check_lines WHERE check_id = ?`, c.ID); err != nil {
		return err
	}
	for _, l := range c.Lines {
		if _, err := tx.Exec(`INSERT OR REPLACE INTO set_check_lines (check_id, part_num, part_name, category, color_id, color_name, bl_id, bl_color, need, have, extra)
			VALUES (?,?,?,?,?,?,?,?,?,?,?)`, c.ID, l.PartNum, l.PartName, l.Category, l.ColorID, l.ColorName, l.BLID, l.BLColor, l.Need, l.Have, l.Extra); err != nil {
			return fmt.Errorf("saving line %s: %w", l.PartNum, err)
		}
	}
	return nil
}

// FinishCheck completes the check in one transaction: it is recorded as done, the
// set's missing count is updated, and extras are added to your loose parts with
// the set they came from. It returns the extras added, for the Part-DB push.
func (d *DB) FinishCheck(c *SetCheck) (extras []OwnedPart, err error) {
	d.ensureDailySnapshot()
	tx, err := d.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	c.Status, c.FinishedAt = StatusDone, time.Now()
	if err := saveCheck(tx, c); err != nil {
		return nil, err
	}
	_, _, missing, _, _ := c.Totals()
	if _, err := tx.Exec(`INSERT INTO set_state (set_num, missing_qty, last_check_id) VALUES (?,?,?)
		ON CONFLICT(set_num) DO UPDATE SET missing_qty = excluded.missing_qty, last_check_id = excluded.last_check_id`, c.SetNum, missing, c.ID); err != nil {
		return nil, err
	}
	for _, l := range c.Lines {
		if l.Extra <= 0 {
			continue
		}
		cur, err := getOwnedTx(tx, l.PartNum, l.ColorID, l.ColorName)
		if err != nil {
			return nil, err
		}
		p := OwnedPart{PartNum: l.PartNum, Name: l.PartName, Category: l.Category, ColorID: l.ColorID, ColorName: l.ColorName, Qty: l.Extra}
		before := 0
		if cur != nil {
			before = cur.Qty
			p.Qty += cur.Qty
			p.MinQty, p.ID, p.SyncedPartID = cur.MinQty, cur.ID, cur.SyncedPartID
		}
		if err := addOwned(tx, p); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(`INSERT INTO part_origins (part_num, color_id, origin_set, qty) VALUES (?,?,?,?)
			ON CONFLICT(part_num, color_id, origin_set) DO UPDATE SET qty = qty + excluded.qty`, l.PartNum, l.ColorID, c.SetNum, l.Extra); err != nil {
			return nil, err
		}
		d.journal(tx, "change", "part", l.PartNum, l.ColorID, l.ColorName, before, p.Qty, "extras from set "+c.SetNum)
		if got, _ := getOwnedTx(tx, l.PartNum, l.ColorID, l.ColorName); got != nil {
			p = *got
		}
		extras = append(extras, p)
	}
	pieces, have, _, extra, _ := c.Totals()
	d.journal(tx, "check", "set", c.SetNum, -1, "", pieces, have, fmt.Sprintf("%s check by %s: %d missing, %d extra", c.Kind, c.CheckedBy, missing, extra))
	return extras, tx.Commit()
}

func getOwnedTx(tx *sql.Tx, partNum string, colorID int, colorName string) (*OwnedPart, error) {
	p, err := scanOwned(tx.QueryRow(`SELECT `+ownedCols+` FROM owned_parts WHERE `+ownedMatch, partNum, colorID, colorName))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return p, err
}

// Spare is loose stock of a part that could fill a set's gap.
type Spare struct {
	Qty       int
	OriginSet string // "" = plain loose parts
}

// SparesFor lists loose parts of part+colour, split by the set they came from.
func (d *DB) SparesFor(partNum string, colorID int) ([]Spare, error) {
	p, err := d.GetOwnedPart(partNum, colorID, "")
	if err != nil || p == nil || p.Qty <= 0 {
		return nil, err
	}
	rows, err := d.Query(`SELECT origin_set, qty FROM part_origins WHERE part_num = ? AND color_id = ? AND qty > 0 ORDER BY qty DESC`, partNum, colorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Spare
	tagged := 0
	for rows.Next() {
		var s Spare
		if err := rows.Scan(&s.OriginSet, &s.Qty); err != nil {
			return nil, err
		}
		s.Qty = min(s.Qty, p.Qty-tagged)
		if s.Qty > 0 {
			out = append(out, s)
			tagged += s.Qty
		}
	}
	if rest := p.Qty - tagged; rest > 0 {
		out = append(out, Spare{Qty: rest})
	}
	return out, rows.Err()
}

// TakeSpareLoose takes qty loose parts of part+colour for set toSet (preferring
// ones that came from other sets' extras): the loose count and origins drop. The
// caller credits the set (a check in progress does it on its own lines).
func (d *DB) TakeSpareLoose(partNum string, colorID, qty int, toSet string) (*OwnedPart, error) {
	tx, err := d.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	p, err := takeLoose(d, tx, partNum, colorID, qty, toSet)
	if err != nil {
		return nil, err
	}
	return p, tx.Commit()
}

func takeLoose(d *DB, tx *sql.Tx, partNum string, colorID, qty int, toSet string) (*OwnedPart, error) {
	p, err := getOwnedTx(tx, partNum, colorID, "")
	if err != nil {
		return nil, err
	}
	if p == nil || p.Qty < qty {
		have := 0
		if p != nil {
			have = p.Qty
		}
		return nil, fmt.Errorf("only %d loose %s on hand", have, partNum)
	}
	before := p.Qty
	p.Qty -= qty
	if err := addOwned(tx, *p); err != nil {
		return nil, err
	}
	rows, err := tx.Query(`SELECT origin_set, qty FROM part_origins WHERE part_num = ? AND color_id = ? AND qty > 0 AND origin_set != ? ORDER BY qty DESC`, partNum, colorID, toSet)
	if err != nil {
		return nil, err
	}
	var origins []Spare
	for rows.Next() {
		var s Spare
		_ = rows.Scan(&s.OriginSet, &s.Qty)
		origins = append(origins, s)
	}
	rows.Close()
	left := qty
	for _, s := range origins {
		take := min(left, s.Qty)
		if take <= 0 {
			break
		}
		if _, err := tx.Exec(`UPDATE part_origins SET qty = qty - ? WHERE part_num = ? AND color_id = ? AND origin_set = ?`, take, partNum, colorID, s.OriginSet); err != nil {
			return nil, err
		}
		left -= take
	}
	d.journal(tx, "change", "part", partNum, colorID, p.ColorName, before, p.Qty, "moved into set "+toSet)
	return p, nil
}

// TakeSpare moves qty loose parts into a checked set: the loose count drops and the
// set's last check gains them (its missing count drops).
func (d *DB) TakeSpare(toSet, partNum string, colorID, qty int) (*OwnedPart, error) {
	tx, err := d.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var checkID int64
	if err := tx.QueryRow(`SELECT last_check_id FROM set_state WHERE set_num = ?`, toSet).Scan(&checkID); err != nil || checkID == 0 {
		return nil, fmt.Errorf("set %s has not been checked yet", toSet)
	}
	res, err := tx.Exec(`UPDATE set_check_lines SET have = MIN(need, have + ?) WHERE check_id = ? AND part_num = ? AND color_id = ?`, qty, checkID, partNum, colorID)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, fmt.Errorf("set %s does not use %s in that colour", toSet, partNum)
	}
	p, err := takeLoose(d, tx, partNum, colorID, qty, toSet)
	if err != nil {
		return nil, err
	}
	if err := refreshMissing(tx, toSet, checkID); err != nil {
		return nil, err
	}
	return p, tx.Commit()
}

// refreshMissing recomputes a set's missing count from its last check.
func refreshMissing(tx *sql.Tx, setNum string, checkID int64) error {
	var missing int
	if err := tx.QueryRow(`SELECT COALESCE(SUM(MAX(need - have, 0)), 0) FROM set_check_lines WHERE check_id = ?`, checkID).Scan(&missing); err != nil {
		return err
	}
	_, err := tx.Exec(`UPDATE set_state SET missing_qty = ? WHERE set_num = ?`, missing, setNum)
	return err
}

// ---- set state ----

type SetState struct {
	SetNum, Location, Condition, ConditionNote string
	MissingQty, OnOrderQty                     int
	LastCheck                                  *SetCheck // without lines
	PDBLocationID                              int
}

// Incomplete reports whether the last check found parts missing.
func (s *SetState) Incomplete() bool { return s.MissingQty > 0 }

// Checked reports whether the set was ever checked.
func (s *SetState) Checked() bool { return s.LastCheck != nil }

// GetSetState returns what is known about a set's contents (an empty state, never nil).
func (d *DB) GetSetState(setNum string) *SetState {
	s := &SetState{SetNum: setNum}
	var checkID int64
	_ = d.QueryRow(`SELECT location, condition, condition_note, missing_qty, last_check_id, pdb_location_id FROM set_state WHERE set_num = ?`, setNum).
		Scan(&s.Location, &s.Condition, &s.ConditionNote, &s.MissingQty, &checkID, &s.PDBLocationID)
	if checkID > 0 {
		c := &SetCheck{ID: checkID}
		var started, finished string
		if d.QueryRow(`SELECT set_num, kind, status, checked_by, started_at, finished_at FROM set_checks WHERE id = ?`, checkID).
			Scan(&c.SetNum, &c.Kind, &c.Status, &c.CheckedBy, &started, &finished) == nil {
			c.StartedAt, _ = time.Parse(timeLayout, started)
			c.FinishedAt, _ = time.Parse(timeLayout, finished)
			s.LastCheck = c
		}
	}
	_ = d.QueryRow(`SELECT COALESCE(SUM(l.qty - l.received_qty), 0) FROM order_lines l JOIN orders o ON o.id = l.order_id
		WHERE l.set_num = ? AND o.status IN ('ordered','shipped')`, setNum).Scan(&s.OnOrderQty)
	return s
}

// SetInfo updates a set's location and condition.
func (d *DB) SetInfo(setNum, location, condition, note string) error {
	_, err := d.Exec(`INSERT INTO set_state (set_num, location, condition, condition_note) VALUES (?,?,?,?)
		ON CONFLICT(set_num) DO UPDATE SET location = excluded.location, condition = excluded.condition, condition_note = excluded.condition_note`,
		setNum, strings.TrimSpace(location), strings.TrimSpace(condition), strings.TrimSpace(note))
	return err
}

// SetPDBLocation remembers the Part-DB storage location made for the set.
func (d *DB) SetPDBLocation(setNum string, id int) error {
	_, err := d.Exec(`INSERT INTO set_state (set_num, pdb_location_id) VALUES (?,?)
		ON CONFLICT(set_num) DO UPDATE SET pdb_location_id = excluded.pdb_location_id`, setNum, id)
	return err
}

// IncompleteSets lists sets whose last check found parts missing, most missing first.
func (d *DB) IncompleteSets() ([]SetState, error) {
	rows, err := d.Query(`SELECT set_num FROM set_state WHERE missing_qty > 0 ORDER BY missing_qty DESC, set_num`)
	if err != nil {
		return nil, err
	}
	var nums []string
	for rows.Next() {
		var n string
		_ = rows.Scan(&n)
		nums = append(nums, n)
	}
	rows.Close()
	out := make([]SetState, 0, len(nums))
	for _, n := range nums {
		out = append(out, *d.GetSetState(n))
	}
	return out, nil
}

// CheckedSets lists every set with a finished check.
func (d *DB) CheckedSets() ([]SetState, error) {
	rows, err := d.Query(`SELECT set_num FROM set_state WHERE last_check_id > 0 ORDER BY set_num`)
	if err != nil {
		return nil, err
	}
	var nums []string
	for rows.Next() {
		var n string
		_ = rows.Scan(&n)
		nums = append(nums, n)
	}
	rows.Close()
	out := make([]SetState, 0, len(nums))
	for _, n := range nums {
		out = append(out, *d.GetSetState(n))
	}
	return out, nil
}
