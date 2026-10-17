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
	// Damaged is a condition flag, not a quantity: how many of Have are
	// present but not in good condition. Purely informational -- it does
	// not reduce Have/Missing()/completion, LowStock's auto-tracking, or
	// ordering; a damaged piece still counts as held, the same as before
	// this existed. Set via the TUI's D key or the mobile app's DAMAGED
	// button.
	Damaged  int
	Optional bool // doesn't count toward missing/completion totals — see DB.IsOptional
	SmallBag bool // goes in the small-parts bag, not the main one — see DB.PartBagSize
	// Deprioritized is the kanban board's "moved to the back" flag: set
	// automatically in saveCheck when Missing() is at or above
	// kanbanQtyThreshold() (default 50, see internal/config.KanbanBacklogQtyThreshold).
	// Purely a display/sort-order hint, like Damaged — the line stays part of
	// the same check, nothing is pulled out of it.
	Deprioritized bool
}

// Missing is how many of the line the set is short — always 0 for an optional line.
func (l CheckLine) Missing() int {
	if l.Optional {
		return 0
	}
	return max(0, l.Need-l.Have)
}

type SetCheck struct {
	ID                   int64
	SetNum, Kind, Status string
	CheckedBy            string
	StartedAt            time.Time
	FinishedAt           time.Time
	// LastPosition is the furthest line a mobile client's confirm flow has
	// reached (see AdvanceCheckPosition) -- so opening this check on a
	// different device, or just reopening the app, resumes there instead of
	// always restarting at line 0. Not touched by SaveCheck/the TUI.
	LastPosition int
	// PaperCheckedAt is set by MarkPaperDone: the paper copy of this check is
	// finished, but the CLI entry (the rest of this struct) may still be
	// whatever NewCheck built it as -- see DB.PendingPaperChecks. Zero means
	// no paper-done step happened (most checks: CLI-only, as before).
	PaperCheckedAt time.Time
	// Disputed and DisputeNote are set by FlagDispute, never automatically:
	// this check's own numbers are in question, and a proof on file (see
	// ProofsFor) is the evidence of what was actually true. Existing solely
	// for an admin to see before deciding whether a deduction stands --
	// never changes Totals(), RecordCheckOutcome, or anything else.
	Disputed    bool
	DisputeNote string
	Lines       []CheckLine
}

// Totals are the check's piece counts.
func (c *SetCheck) Totals() (pieces, have, missing, extra, missingLines int) {
	for _, l := range c.Lines {
		extra += l.Extra
		if l.Optional { // stickers etc. don't count toward completion — see DB.IsOptional
			continue
		}
		pieces += l.Need
		have += min(l.Have, l.Need)
		missing += l.Missing()
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
				c.Lines[i].Damaged = 0 // a recount re-verifies condition too, not just quantity
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
		optional, _ := d.IsOptional(it.PartNum, cat)
		bagSize, _ := d.PartBagSize(it.PartNum)
		c.Lines = append(c.Lines, CheckLine{PartNum: it.PartNum, PartName: it.PartName, Category: cat, ColorID: it.ColorID,
			ColorName: it.ColorName, BLID: it.BrickLinkID, BLColor: it.BLColor, Need: n, Have: n, Optional: optional, SmallBag: bagSize == BagSmall})
	}
	sortLinesByColorThenCategory(c.Lines)
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

// DeleteDraftCheck discards a draft's progress entirely (the "abandon without
// saving" quit option — see uiapp's quit workflow) — refuses anything already
// finished, since that would erase a real result, not a redo.
func (d *DB) DeleteDraftCheck(id int64) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var status string
	if err := tx.QueryRow(`SELECT status FROM set_checks WHERE id = ?`, id).Scan(&status); err != nil {
		return err
	}
	if status != StatusDraft {
		return errors.New("only a draft (not yet finished) check can be abandoned this way")
	}
	if _, err := tx.Exec(`DELETE FROM set_check_lines WHERE check_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM set_checks WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// GetCheck loads a check with its lines.
func (d *DB) GetCheck(id int64) (*SetCheck, error) {
	c := &SetCheck{ID: id}
	var started, finished, paperChecked string
	var disputed int
	if err := d.QueryRow(`SELECT set_num, kind, status, checked_by, started_at, finished_at, last_position, paper_checked_at, disputed, dispute_note
		FROM set_checks WHERE id = ?`, id).
		Scan(&c.SetNum, &c.Kind, &c.Status, &c.CheckedBy, &started, &finished, &c.LastPosition, &paperChecked, &disputed, &c.DisputeNote); err != nil {
		return nil, err
	}
	c.StartedAt, _ = time.Parse(timeLayout, started)
	c.FinishedAt, _ = time.Parse(timeLayout, finished)
	c.PaperCheckedAt, _ = time.Parse(timeLayout, paperChecked)
	c.Disputed = disputed != 0
	rows, err := d.Query(`SELECT part_num, part_name, category, color_id, color_name, bl_id, bl_color, need, have, extra, damaged, deprioritized
		FROM set_check_lines WHERE check_id = ?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var l CheckLine
		if err := rows.Scan(&l.PartNum, &l.PartName, &l.Category, &l.ColorID, &l.ColorName, &l.BLID, &l.BLColor, &l.Need, &l.Have, &l.Extra, &l.Damaged, &l.Deprioritized); err != nil {
			return nil, err
		}
		l.Optional, _ = d.IsOptional(l.PartNum, l.Category) // recomputed fresh, in case it changed since this line was saved
		if bagSize, err := d.PartBagSize(l.PartNum); err == nil {
			l.SmallBag = bagSize == BagSmall
		}
		c.Lines = append(c.Lines, l)
	}
	sortLinesByColorThenCategory(c.Lines)
	return c, rows.Err()
}

// sortLinesByColorThenCategory is CheckLine's colour-then-category order (see
// lego.LessColorThenCategory), with the part number as a final, fully deterministic
// tiebreak — used by both NewCheck and GetCheck so a check's lines read the same
// order whether freshly built or reloaded.
func sortLinesByColorThenCategory(lines []CheckLine) {
	sort.SliceStable(lines, func(i, j int) bool {
		a, b := lines[i], lines[j]
		if less := LessColorThenCategory(a.ColorName, a.Category, a.PartName, b.ColorName, b.Category, b.PartName); less {
			return true
		}
		if LessColorThenCategory(b.ColorName, b.Category, b.PartName, a.ColorName, a.Category, a.PartName) {
			return false
		}
		return a.PartNum < b.PartNum
	})
}

// MarkPaperDone opens (or resumes) setNum's current check and stamps it
// paper-done at `at` -- the paper copy is finished, the CLI entry is still to
// come. This is the same draft `wms lego check`/the TUI/mobile app resume
// into later (see NewCheck's draftCheck lookup), so paper-done and the
// eventual CLI entry stay one check session throughout.
func (d *DB) MarkPaperDone(ctx context.Context, rb *Client, setNum, kind, by string, at time.Time) (*SetCheck, error) {
	c, err := d.NewCheck(ctx, rb, setNum, kind, by)
	if err != nil {
		return nil, err
	}
	c.PaperCheckedAt = at
	if err := d.SaveCheck(c); err != nil {
		return nil, err
	}
	return c, nil
}

// PendingPaperChecks lists open drafts whose paper copy is done but the CLI
// entry isn't, oldest paper-done first -- `wms lego checks pending` and the
// login banner's count.
func (d *DB) PendingPaperChecks() ([]SetCheck, error) {
	rows, err := d.Query(`SELECT id, set_num, kind, checked_by, paper_checked_at FROM set_checks
		WHERE status = ? AND paper_checked_at != '' ORDER BY paper_checked_at`, StatusDraft)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SetCheck
	for rows.Next() {
		var c SetCheck
		var at string
		if err := rows.Scan(&c.ID, &c.SetNum, &c.Kind, &c.CheckedBy, &at); err != nil {
			return nil, err
		}
		c.PaperCheckedAt, _ = time.Parse(timeLayout, at)
		out = append(out, c)
	}
	return out, rows.Err()
}

// CurrentOrLastCheck is setNum's open draft if it has one, else its most
// recent check of any status (nil if it has never been checked at all) --
// resolves "this set" into a specific check row for `wms lego attach`,
// without the caller needing to know intake vs recount.
func (d *DB) CurrentOrLastCheck(setNum string) (*SetCheck, error) {
	var id int64
	err := d.QueryRow(`SELECT id FROM set_checks WHERE set_num = ? AND status = ? ORDER BY id DESC LIMIT 1`, setNum, StatusDraft).Scan(&id)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if id == 0 {
		err = d.QueryRow(`SELECT id FROM set_checks WHERE set_num = ? ORDER BY id DESC LIMIT 1`, setNum).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
	}
	return d.GetCheck(id)
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

// AdvanceCheckPosition records that a mobile client's confirm flow has
// reached pos — only forward (MAX), so jumping ahead to handle a missing
// line out of order and then going back doesn't un-advance the resume point
// a different device would pick up. Deliberately separate from
// SaveCheck/saveCheck: the TUI's own save paths (S key, FinishCheck) never
// touch this column, so nothing about the TUI's behavior changes.
func (d *DB) AdvanceCheckPosition(checkID int64, pos int) error {
	_, err := d.Exec(`UPDATE set_checks SET last_position = MAX(last_position, ?) WHERE id = ?`, pos, checkID)
	return err
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
	paperChecked := ""
	if !c.PaperCheckedAt.IsZero() {
		paperChecked = c.PaperCheckedAt.UTC().Format(timeLayout)
	}
	disputed := 0
	if c.Disputed {
		disputed = 1
	}
	if c.ID == 0 {
		res, err := tx.Exec(`INSERT INTO set_checks (set_num, kind, status, checked_by, started_at, finished_at, lines, pieces, have, missing, extra, paper_checked_at, disputed, dispute_note)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, c.SetNum, c.Kind, c.Status, c.CheckedBy, c.StartedAt.UTC().Format(timeLayout), finished, len(c.Lines), pieces, have, missing, extra, paperChecked, disputed, c.DisputeNote)
		if err != nil {
			return fmt.Errorf("saving the check: %w", err)
		}
		c.ID, _ = res.LastInsertId()
	} else if _, err := tx.Exec(`UPDATE set_checks SET status = ?, checked_by = ?, finished_at = ?, lines = ?, pieces = ?, have = ?, missing = ?, extra = ?, paper_checked_at = ?, disputed = ?, dispute_note = ? WHERE id = ?`,
		c.Status, c.CheckedBy, finished, len(c.Lines), pieces, have, missing, extra, paperChecked, disputed, c.DisputeNote, c.ID); err != nil {
		return fmt.Errorf("saving the check: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM set_check_lines WHERE check_id = ?`, c.ID); err != nil {
		return err
	}
	threshold := kanbanQtyThreshold()
	for _, l := range c.Lines {
		deprioritized := l.Missing() >= threshold
		if _, err := tx.Exec(`INSERT OR REPLACE INTO set_check_lines (check_id, part_num, part_name, category, color_id, color_name, bl_id, bl_color, need, have, extra, damaged, deprioritized)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`, c.ID, l.PartNum, l.PartName, l.Category, l.ColorID, l.ColorName, l.BLID, l.BLColor, l.Need, l.Have, l.Extra, l.Damaged, deprioritized); err != nil {
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
	if err := autoTrackMissing(tx, d, c); err != nil {
		return nil, err
	}
	if err := releasePoolBagsForCheck(tx, c.ID); err != nil {
		return nil, err
	}
	pieces, have, _, extra, _ := c.Totals()
	d.journal(tx, "check", "set", c.SetNum, -1, "", pieces, have, fmt.Sprintf("%s check by %s: %d missing, %d extra", c.Kind, c.CheckedBy, missing, extra))
	return extras, tx.Commit()
}

// autoTrackMissing is "a check's missing findings feed the low-stock alert
// directly, no separate manual flag step": the first time a non-optional
// line comes back genuinely short, its part gets a min_qty equal to the
// shortfall (creating an owned_parts row at qty 0 if there wasn't one
// already), so it shows up in LowStock()/`wms lego low` and the Discord
// low-stock alert from then on without an admin having to notice and run
// `wms lego set-min` themselves. Never touches a part that already has a
// min_qty set -- that's an admin's own chosen threshold, not something a
// check result should silently override.
func autoTrackMissing(tx *sql.Tx, d *DB, c *SetCheck) error {
	now := time.Now().UTC().Format(timeLayout)
	for _, l := range c.Lines {
		missing := l.Missing()
		if missing <= 0 {
			continue
		}
		cur, err := getOwnedTx(tx, l.PartNum, l.ColorID, l.ColorName)
		if err != nil {
			return err
		}
		if cur != nil && cur.MinQty > 0 {
			continue
		}
		qty := 0
		if cur == nil {
			if _, err := tx.Exec(`INSERT INTO owned_parts (part_num, name, category, color_id, color_name, qty, min_qty, synced_part_id, updated_at)
				VALUES (?,?,?,?,?,0,?,0,?)`, l.PartNum, l.PartName, l.Category, l.ColorID, l.ColorName, missing, now); err != nil {
				return fmt.Errorf("auto-tracking %s as low stock: %w", l.PartNum, err)
			}
		} else {
			qty = cur.Qty
			if _, err := tx.Exec(`UPDATE owned_parts SET min_qty = ?, updated_at = ? WHERE `+ownedMatch, missing, now, l.PartNum, l.ColorID, l.ColorName); err != nil {
				return fmt.Errorf("auto-tracking %s as low stock: %w", l.PartNum, err)
			}
		}
		d.journal(tx, "min", "part", l.PartNum, l.ColorID, l.ColorName, qty, qty, fmt.Sprintf("auto-tracked: %d short on set %s check", missing, c.SetNum))
	}
	return nil
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
	// ImageURL is an admin's override of this set's picture (blank = use the
	// catalog's own cat_sets.img_url). Set via SetImageOverride, applied by
	// DB.CatalogSet so every reader — TUI, CLI, the mobile API's lineView —
	// sees it the same way, with no separate push to keep them in sync.
	ImageURL string
	// MaxBags is how many bag instances (see bags.go) a check of this set may
	// register at once -- 2 (small + main) by default; an admin raises it for
	// a set that genuinely needs more. BagGating "" (the default) means the
	// live bag-scan gate is on; "off" is an admin's per-set exemption.
	MaxBags   int
	BagGating string
	// BoxBarcode is the retail box's own UPC/EAN, a second way to resolve this
	// set besides its set_num -- see SetBoxBarcode/DB.GetSetByNum. Blank means
	// none registered yet.
	BoxBarcode string
	// BacklogStatus is the kanban board's gate on a newly-added set: "backlog"
	// (not yet checkable, an admin hasn't released it) or "released" (the
	// default, and the only state any set added before this existed has ever
	// been in). See DB.SetBacklogStatus/BacklogSets.
	BacklogStatus                        string
	BacklogReleasedBy, BacklogReleasedAt string
}

// BagGatingOn reports whether this set's live bag-scan gate is active --
// true unless an admin has explicitly turned it off for this set.
func (s *SetState) BagGatingOn() bool { return s.BagGating != "off" }

// Incomplete reports whether the last check found parts missing.
func (s *SetState) Incomplete() bool { return s.MissingQty > 0 }

// Checked reports whether the set was ever checked.
func (s *SetState) Checked() bool { return s.LastCheck != nil }

// GetSetState returns what is known about a set's contents (an empty state, never nil).
func (d *DB) GetSetState(setNum string) *SetState {
	s := &SetState{SetNum: setNum, MaxBags: 2, BacklogStatus: BacklogReleased}
	var checkID int64
	_ = d.QueryRow(`SELECT location, condition, condition_note, missing_qty, last_check_id, pdb_location_id, image_url, max_bags, bag_gating, box_barcode, backlog_status, backlog_released_by, backlog_released_at FROM set_state WHERE set_num = ?`, setNum).
		Scan(&s.Location, &s.Condition, &s.ConditionNote, &s.MissingQty, &checkID, &s.PDBLocationID, &s.ImageURL, &s.MaxBags, &s.BagGating, &s.BoxBarcode, &s.BacklogStatus, &s.BacklogReleasedBy, &s.BacklogReleasedAt)
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

// SetBagPolicy updates a set's bag-gating knobs -- maxBags how many bag
// instances a check of it may register (admin raises it for a set that
// genuinely needs more than the small+main default), gating "off" to exempt
// the set from the live scan gate entirely, "" (or any other value) to leave
// it on. Caller enforces admin-only, same as every other set-info flag.
func (d *DB) SetBagPolicy(setNum string, maxBags int, gating string) error {
	if maxBags < 1 {
		return errors.New("max bags must be at least 1")
	}
	_, err := d.Exec(`INSERT INTO set_state (set_num, max_bags, bag_gating) VALUES (?,?,?)
		ON CONFLICT(set_num) DO UPDATE SET max_bags = excluded.max_bags, bag_gating = excluded.bag_gating`,
		setNum, maxBags, strings.TrimSpace(gating))
	return err
}

// SetImageOverride records an admin's replacement picture for a set (blank
// clears it, back to the catalog's own cat_sets.img_url). See DB.CatalogSet.
func (d *DB) SetImageOverride(setNum, url string) error {
	_, err := d.Exec(`INSERT INTO set_state (set_num, image_url) VALUES (?,?)
		ON CONFLICT(set_num) DO UPDATE SET image_url = excluded.image_url`,
		setNum, strings.TrimSpace(url))
	return err
}

// SetBoxBarcode registers setNum's retail box's own UPC/EAN as a second way
// to resolve it (see DB.GetSetByNum) -- useful when a set's own printed set
// number is awkward to scan or type but the box's barcode is right there.
// Blank clears it. Rejects a barcode already registered to a different set --
// two sets sharing one barcode would make scanning ambiguous.
func (d *DB) SetBoxBarcode(setNum, barcode string) error {
	barcode = strings.TrimSpace(barcode)
	if barcode != "" {
		var owner string
		if err := d.QueryRow(`SELECT set_num FROM set_state WHERE box_barcode = ? AND set_num != ?`, barcode, setNum).Scan(&owner); err == nil {
			return fmt.Errorf("barcode %s is already registered to set %s", barcode, owner)
		} else if err != sql.ErrNoRows {
			return err
		}
	}
	_, err := d.Exec(`INSERT INTO set_state (set_num, box_barcode) VALUES (?,?)
		ON CONFLICT(set_num) DO UPDATE SET box_barcode = excluded.box_barcode`,
		setNum, barcode)
	return err
}

// SetPDBLocation remembers the Part-DB storage location made for the set.
func (d *DB) SetPDBLocation(setNum string, id int) error {
	_, err := d.Exec(`INSERT INTO set_state (set_num, pdb_location_id) VALUES (?,?)
		ON CONFLICT(set_num) DO UPDATE SET pdb_location_id = excluded.pdb_location_id`, setNum, id)
	return err
}

// Kanban backlog states for SetState.BacklogStatus — see DB.SetBacklogStatus.
const (
	BacklogQueued   = "backlog"
	BacklogReleased = "released"
)

// SetBacklogStatus moves a set between the kanban backlog and the active,
// checkable pool. Only "backlog" and "released" are valid; releasing records
// who and when, queuing clears both (a set only goes back to backlog by being
// newly added, never by being pulled back off the active pool).
func (d *DB) SetBacklogStatus(setNum, status, by string) error {
	if status != BacklogQueued && status != BacklogReleased {
		return fmt.Errorf("backlog status must be %q or %q", BacklogQueued, BacklogReleased)
	}
	releasedBy, releasedAt := "", ""
	if status == BacklogReleased {
		releasedBy, releasedAt = by, time.Now().UTC().Format(timeLayout)
	}
	_, err := d.Exec(`INSERT INTO set_state (set_num, backlog_status, backlog_released_by, backlog_released_at) VALUES (?,?,?,?)
		ON CONFLICT(set_num) DO UPDATE SET backlog_status = excluded.backlog_status, backlog_released_by = excluded.backlog_released_by, backlog_released_at = excluded.backlog_released_at`,
		setNum, status, releasedBy, releasedAt)
	return err
}

// BacklogSets lists sets still queued in the kanban backlog, newest-added first.
func (d *DB) BacklogSets() ([]SetState, error) {
	rows, err := d.Query(`SELECT set_num FROM set_state WHERE backlog_status = ? ORDER BY set_num DESC`, BacklogQueued)
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
	return out, rows.Err()
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
