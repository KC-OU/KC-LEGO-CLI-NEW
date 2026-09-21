package lego

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

// History: every change to your collection is journaled, and a snapshot of the
// collection is taken before the first change of each day, so you can see what
// changed, chart it, and go back to an earlier day. Snapshots hold only your data
// (owned parts and sets), never the catalog.

const (
	maxSnapshots   = 400 // about a year of daily restore points
	snapshotDayFmt = "2006-01-02"
)

// JournalRow is one recorded change.
type JournalRow struct {
	ID        int64
	At        time.Time
	Actor     string
	Action    string // "add", "change", "delete", "min", "import", "restore", ...
	ItemType  string // "part" or "set"
	Item      string
	ColorID   int
	ColorName string
	Before    int
	After     int
	Note      string
}

func (d *DB) journal(ex execer, action, itemType, item string, colorID int, colorName string, before, after int, note string) {
	who := d.actor
	if who == "" {
		who = "system"
	}
	// A journal write must never make the change itself fail.
	_, _ = ex.Exec(`INSERT INTO journal (at, actor, action, item_type, item, color_id, color_name, qty_before, qty_after, note) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		time.Now().UTC().Format(timeLayout), who, action, itemType, item, colorID, colorName, before, after, note)
}

// History returns recent changes, newest first; item limits it to one part or set number.
func (d *DB) History(item string, limit int) ([]JournalRow, error) {
	if limit <= 0 {
		limit = 30
	}
	q := `SELECT id, at, actor, action, item_type, item, color_id, color_name, qty_before, qty_after, note FROM journal`
	var args []any
	if item = strings.TrimSpace(item); item != "" {
		q += ` WHERE item = ? COLLATE NOCASE`
		args = append(args, item)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []JournalRow
	for rows.Next() {
		var r JournalRow
		var at string
		if err := rows.Scan(&r.ID, &at, &r.Actor, &r.Action, &r.ItemType, &r.Item, &r.ColorID, &r.ColorName, &r.Before, &r.After, &r.Note); err != nil {
			return nil, err
		}
		r.At, _ = time.Parse(timeLayout, at)
		out = append(out, r)
	}
	return out, rows.Err()
}

// ---- snapshots ----

type snapPart struct {
	PartNum   string `json:"part_num"`
	Name      string `json:"name"`
	Category  string `json:"category"`
	ColorID   int    `json:"color_id"`
	ColorName string `json:"color_name"`
	Qty       int    `json:"qty"`
	MinQty    int    `json:"min_qty"`
}

type snapSet struct {
	SetNum        string `json:"set_num"`
	Name          string `json:"name"`
	Theme         string `json:"theme"`
	Year          int    `json:"year"`
	InstrNumber   string `json:"instruction_book_number"`
	InstrCount    int    `json:"instruction_book_count"`
	Qty           int    `json:"qty"`
	PartsQty      int    `json:"parts_qty"`
	PartedOut     bool   `json:"parted_out"`
	PartedOutInfo string `json:"parted_out_info"`
}

type snapData struct {
	Parts []snapPart `json:"parts"`
	Sets  []snapSet  `json:"sets"`
}

// Snapshot is a restore point's summary.
type Snapshot struct {
	ID     int64
	At     time.Time
	Day    string
	Label  string
	Pieces int
	Lines  int
	Sets   int
	Low    int
	Value  float64
	Priced int
}

func (d *DB) currentData() (*snapData, error) {
	var data snapData
	rows, err := d.Query(`SELECT part_num, name, category, color_id, color_name, qty, min_qty FROM owned_parts ORDER BY part_num, color_id, color_name`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var p snapPart
		if err := rows.Scan(&p.PartNum, &p.Name, &p.Category, &p.ColorID, &p.ColorName, &p.Qty, &p.MinQty); err != nil {
			rows.Close()
			return nil, err
		}
		data.Parts = append(data.Parts, p)
	}
	rows.Close()
	srows, err := d.Query(`SELECT set_num, name, theme, year, instruction_book_number, instruction_book_count, qty, parts_qty, parted_out, parted_out_info FROM sets ORDER BY set_num`)
	if err != nil {
		return nil, err
	}
	defer srows.Close()
	for srows.Next() {
		var s snapSet
		var out int
		if err := srows.Scan(&s.SetNum, &s.Name, &s.Theme, &s.Year, &s.InstrNumber, &s.InstrCount, &s.Qty, &s.PartsQty, &out, &s.PartedOutInfo); err != nil {
			return nil, err
		}
		s.PartedOut = out != 0
		data.Sets = append(data.Sets, s)
	}
	return &data, srows.Err()
}

// TakeSnapshot records the collection as it is now.
func (d *DB) TakeSnapshot(label string) (int64, error) {
	data, err := d.currentData()
	if err != nil {
		return 0, err
	}
	blob, err := json.Marshal(data)
	if err != nil {
		return 0, err
	}
	var pieces, sets, low int
	for _, p := range data.Parts {
		pieces += p.Qty
		if p.MinQty > 0 && p.Qty < p.MinQty {
			low++
		}
	}
	for _, s := range data.Sets {
		sets += s.Qty
	}
	var value float64
	var priced int
	if v, err := d.CollectionValue(strings.ToUpper(config.Get(config.BricklinkCondition))); err == nil {
		value, priced = v.Total, v.Priced
	}
	now := time.Now().UTC()
	res, err := d.Exec(`INSERT INTO snapshots (at, day, label, pieces, lines, sets, low, value, priced, data) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		now.Format(timeLayout), now.Format(snapshotDayFmt), label, pieces, len(data.Parts), sets, low, value, priced, string(blob))
	if err != nil {
		return 0, err
	}
	_, _ = d.Exec(`DELETE FROM snapshots WHERE id NOT IN (SELECT id FROM snapshots ORDER BY id DESC LIMIT ?)`, maxSnapshots)
	return res.LastInsertId()
}

// ensureDailySnapshot takes the day's restore point, holding the collection as it
// was before today's first change. An empty collection is not worth a snapshot.
func (d *DB) ensureDailySnapshot() {
	day := time.Now().UTC().Format(snapshotDayFmt)
	var have int
	if d.QueryRow(`SELECT 1 FROM snapshots WHERE day = ? LIMIT 1`, day).Scan(&have) == nil {
		return
	}
	var any int
	if d.QueryRow(`SELECT 1 FROM owned_parts LIMIT 1`).Scan(&any) != nil && d.QueryRow(`SELECT 1 FROM sets LIMIT 1`).Scan(&any) != nil {
		return
	}
	_, _ = d.TakeSnapshot("start of day, before the first change")
}

// Snapshots lists restore points, newest first.
func (d *DB) Snapshots(limit int) ([]Snapshot, error) {
	if limit <= 0 {
		limit = 30
	}
	rows, err := d.Query(`SELECT id, at, day, label, pieces, lines, sets, low, value, priced FROM snapshots ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Snapshot
	for rows.Next() {
		var s Snapshot
		var at string
		if err := rows.Scan(&s.ID, &at, &s.Day, &s.Label, &s.Pieces, &s.Lines, &s.Sets, &s.Low, &s.Value, &s.Priced); err != nil {
			return nil, err
		}
		s.At, _ = time.Parse(timeLayout, at)
		out = append(out, s)
	}
	return out, rows.Err()
}

// FindSnapshot resolves "12" (an id) or "2026-09-20" (the day's first snapshot).
func (d *DB) FindSnapshot(ref string) (*Snapshot, error) {
	ref = strings.TrimSpace(ref)
	var id int64
	var err error
	if _, perr := time.Parse(snapshotDayFmt, ref); perr == nil {
		err = d.QueryRow(`SELECT id FROM snapshots WHERE day = ? ORDER BY id LIMIT 1`, ref).Scan(&id)
	} else if n, cerr := fmt.Sscan(ref, &id); cerr != nil || n != 1 {
		return nil, fmt.Errorf("%q is neither a snapshot id nor a date like 2026-09-20", ref)
	} else {
		err = d.QueryRow(`SELECT id FROM snapshots WHERE id = ?`, id).Scan(&id)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("no snapshot for %q (see `wms lego snapshots`)", ref)
	}
	if err != nil {
		return nil, err
	}
	all, err := d.Snapshots(maxSnapshots)
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].ID == id {
			return &all[i], nil
		}
	}
	return nil, fmt.Errorf("no snapshot for %q", ref)
}

// RestorePlan says what restoring a snapshot would change.
type RestorePlan struct {
	Snapshot                 Snapshot
	PartsAdded, PartsRemoved int
	PartsChanged             int
	SetsAdded, SetsRemoved   int
	SetsChanged              int
	Samples                  []string
}

func partKey(p snapPart) string {
	c := ""
	if p.ColorID < 0 {
		c = p.ColorName
	}
	return strings.ToLower(p.PartNum) + "/" + fmt.Sprint(p.ColorID) + "/" + strings.ToLower(c)
}

func (d *DB) loadSnapshot(id int64) (*snapData, error) {
	var blob string
	if err := d.QueryRow(`SELECT data FROM snapshots WHERE id = ?`, id).Scan(&blob); err != nil {
		return nil, err
	}
	var data snapData
	if err := json.Unmarshal([]byte(blob), &data); err != nil {
		return nil, fmt.Errorf("snapshot %d is damaged: %w", id, err)
	}
	return &data, nil
}

// PlanRestore compares a snapshot with the collection now, changing nothing.
func (d *DB) PlanRestore(snap Snapshot) (*RestorePlan, error) {
	then, err := d.loadSnapshot(snap.ID)
	if err != nil {
		return nil, err
	}
	now, err := d.currentData()
	if err != nil {
		return nil, err
	}
	plan := &RestorePlan{Snapshot: snap}
	cur := map[string]snapPart{}
	for _, p := range now.Parts {
		cur[partKey(p)] = p
	}
	seen := map[string]bool{}
	sample := func(s string) {
		if len(plan.Samples) < 8 {
			plan.Samples = append(plan.Samples, s)
		}
	}
	for _, p := range then.Parts {
		k := partKey(p)
		seen[k] = true
		c, ok := cur[k]
		switch {
		case !ok:
			plan.PartsAdded++
			sample(fmt.Sprintf("+ %s %s: %d", p.PartNum, p.ColorName, p.Qty))
		case c.Qty != p.Qty || c.MinQty != p.MinQty:
			plan.PartsChanged++
			sample(fmt.Sprintf("~ %s %s: %d -> %d", p.PartNum, p.ColorName, c.Qty, p.Qty))
		}
	}
	for k, p := range cur {
		if !seen[k] {
			plan.PartsRemoved++
			sample(fmt.Sprintf("- %s %s: %d removed", p.PartNum, p.ColorName, p.Qty))
		}
	}
	curSets := map[string]snapSet{}
	for _, s := range now.Sets {
		curSets[s.SetNum] = s
	}
	seenSets := map[string]bool{}
	for _, s := range then.Sets {
		seenSets[s.SetNum] = true
		c, ok := curSets[s.SetNum]
		switch {
		case !ok:
			plan.SetsAdded++
			sample(fmt.Sprintf("+ set %s %s: x%d", s.SetNum, s.Name, s.Qty))
		case c.Qty != s.Qty || c.PartedOut != s.PartedOut || c.Name != s.Name:
			plan.SetsChanged++
			sample(fmt.Sprintf("~ set %s: x%d -> x%d", s.SetNum, c.Qty, s.Qty))
		}
	}
	for num, s := range curSets {
		if !seenSets[num] {
			plan.SetsRemoved++
			sample(fmt.Sprintf("- set %s %s removed", num, s.Name))
		}
	}
	return plan, nil
}

// RestoreSnapshot puts your owned parts and sets back as they were in the snapshot.
// It first snapshots the present (so a restore can itself be undone), then replaces
// both tables in one transaction. Part-DB links (synced ids) are kept for parts that
// exist both then and now; restored parts are re-linked by the next `wms lego sync-parts`.
func (d *DB) RestoreSnapshot(ctx context.Context, snap Snapshot) (*RestorePlan, error) {
	plan, err := d.PlanRestore(snap)
	if err != nil {
		return nil, err
	}
	data, err := d.loadSnapshot(snap.ID)
	if err != nil {
		return nil, err
	}
	if _, err := d.TakeSnapshot(fmt.Sprintf("before restoring snapshot #%d", snap.ID)); err != nil {
		return nil, fmt.Errorf("could not save the present first, so nothing was changed: %w", err)
	}
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	synced := map[string]int{}
	rows, err := tx.Query(`SELECT part_num, color_id, color_name, synced_part_id FROM owned_parts WHERE synced_part_id > 0`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var p snapPart
		var id int
		if err := rows.Scan(&p.PartNum, &p.ColorID, &p.ColorName, &id); err != nil {
			rows.Close()
			return nil, err
		}
		synced[partKey(p)] = id
	}
	rows.Close()
	if _, err := tx.Exec(`DELETE FROM owned_parts`); err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(timeLayout)
	for _, p := range data.Parts {
		if _, err := tx.Exec(`INSERT INTO owned_parts (part_num, name, category, color_id, color_name, qty, min_qty, synced_part_id, updated_at) VALUES (?,?,?,?,?,?,?,?,?)`,
			p.PartNum, p.Name, p.Category, p.ColorID, p.ColorName, p.Qty, p.MinQty, synced[partKey(p)], now); err != nil {
			return nil, fmt.Errorf("restoring %s: %w", p.PartNum, err)
		}
	}
	if _, err := tx.Exec(`DELETE FROM sets`); err != nil {
		return nil, err
	}
	for _, s := range data.Sets {
		if _, err := tx.Exec(`INSERT INTO sets (set_num, name, theme, year, instruction_book_number, instruction_book_count, qty, parts_qty, parted_out, parted_out_info, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`, s.SetNum, s.Name, s.Theme, s.Year, s.InstrNumber, s.InstrCount, s.Qty, s.PartsQty, b2i(s.PartedOut), nonEmpty(s.PartedOutInfo, "{}"), now, now); err != nil {
			return nil, fmt.Errorf("restoring set %s: %w", s.SetNum, err)
		}
	}
	d.journal(tx, "restore", "part", fmt.Sprintf("snapshot #%d", snap.ID), -1, "", 0, len(data.Parts),
		fmt.Sprintf("restored %d part line(s) and %d set(s) from %s", len(data.Parts), len(data.Sets), snap.Day))
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return plan, nil
}

func nonEmpty(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

// Sparkline draws values as a row of block characters, scaled to their own range.
func Sparkline(vals []float64) string {
	if len(vals) == 0 {
		return ""
	}
	const bars = "▁▂▃▄▅▆▇█"
	lo, hi := vals[0], vals[0]
	for _, v := range vals {
		lo, hi = min(lo, v), max(hi, v)
	}
	r := []rune(bars)
	var b strings.Builder
	for _, v := range vals {
		idx := 0
		if hi > lo {
			idx = int((v - lo) / (hi - lo) * float64(len(r)-1))
		} else {
			idx = len(r) / 2
		}
		b.WriteRune(r[idx])
	}
	return b.String()
}
