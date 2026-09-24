package lego

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"time"
)

// OwnedPart is an individual LEGO part you hold (spares, a bulk lot, parts
// out of a broken-up set) in one colour — distinct from Set, and the only LEGO
// data that reaches the shared Part-DB inventory (see partsync.go).
type OwnedPart struct {
	ID           int64
	PartNum      string
	Name         string
	Category     string
	ColorID      int    // Rebrickable colour id; NoColor when unknown or free text
	ColorName    string // display name, or the free-text colour when ColorID is NoColor
	Qty          int
	MinQty       int
	SyncedPartID int
	UpdatedAt    time.Time
}

// NoColor is the ColorID of an owned part whose colour is unknown or typed in.
const NoColor = -1

const ownedCols = `id, part_num, name, category, color_id, color_name, qty, min_qty, synced_part_id, updated_at`

func scanOwned(row interface{ Scan(...any) error }) (*OwnedPart, error) {
	var p OwnedPart
	var updatedAt string
	if err := row.Scan(&p.ID, &p.PartNum, &p.Name, &p.Category, &p.ColorID, &p.ColorName, &p.Qty, &p.MinQty, &p.SyncedPartID, &updatedAt); err != nil {
		return nil, err
	}
	p.UpdatedAt, _ = time.Parse(timeLayout, updatedAt)
	return &p, nil
}

var partNumRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,39}$`)

// CheckPartNum refuses what cannot be a part number (commas, quotes, spaces, formulas, very long text)
// where a person or a file supplies one, so junk does not become a collection row and a Part-DB name.
func CheckPartNum(s string) error {
	if !partNumRE.MatchString(s) {
		return fmt.Errorf("%q is not a part number: use letters, digits, dot, dash or underscore, up to 40 characters", s)
	}
	return nil
}

// ownedMatch is the WHERE clause identifying one (part, colour) row: a known
// colour by id alone, a free-text one by its name.
const ownedMatch = `part_num = ? AND color_id = ? AND (color_id >= 0 OR color_name = ?)`

// AddOwnedPart records or updates the part+colour row (matched as above); qty
// is the total you hold, not an increment. MinQty and SyncedPartID on an
// existing row are left alone.
func (d *DB) AddOwnedPart(p OwnedPart) error {
	d.ensureDailySnapshot()
	before, existed := 0, false
	if cur, err := d.GetOwnedPart(p.PartNum, p.ColorID, p.ColorName); err == nil && cur != nil {
		before, existed = cur.Qty, true
	}
	if err := addOwned(d.DB, p); err != nil {
		return err
	}
	if !existed {
		d.journal(d.DB, "add", "part", p.PartNum, p.ColorID, p.ColorName, 0, p.Qty, "")
	} else if before != p.Qty {
		d.journal(d.DB, "change", "part", p.PartNum, p.ColorID, p.ColorName, before, p.Qty, "")
	}
	return nil
}

// execer is what a *sql.DB and a *sql.Tx share, so the same upsert runs alone or inside a transaction.
type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

func addOwned(d execer, p OwnedPart) error {
	now := time.Now().UTC().Format(timeLayout)
	res, err := d.Exec(`UPDATE owned_parts SET name = ?, category = ?, color_name = ?, qty = ?, updated_at = ? WHERE `+ownedMatch,
		p.Name, p.Category, p.ColorName, p.Qty, now, p.PartNum, p.ColorID, p.ColorName)
	if err != nil {
		return fmt.Errorf("recording owned part %s: %w", p.PartNum, err)
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	if _, err := d.Exec(`INSERT INTO owned_parts (part_num, name, category, color_id, color_name, qty, min_qty, synced_part_id, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?)`, p.PartNum, p.Name, p.Category, p.ColorID, p.ColorName, p.Qty, p.MinQty, now); err != nil {
		return fmt.Errorf("recording owned part %s: %w", p.PartNum, err)
	}
	return nil
}

// GetOwnedPart returns the row for this part in this colour, or nil.
func (d *DB) GetOwnedPart(partNum string, colorID int, colorName string) (*OwnedPart, error) {
	row := d.QueryRow(`SELECT `+ownedCols+` FROM owned_parts WHERE `+ownedMatch, partNum, colorID, colorName)
	p, err := scanOwned(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("looking up owned part %s: %w", partNum, err)
	}
	return p, nil
}

// OwnedPartsOf returns every colour of one part number you hold.
func (d *DB) OwnedPartsOf(partNum string) ([]OwnedPart, error) {
	return d.queryOwned(`SELECT `+ownedCols+` FROM owned_parts WHERE part_num = ? ORDER BY color_name`, partNum)
}

func (d *DB) ListOwnedParts() ([]OwnedPart, error) {
	return d.queryOwned(`SELECT ` + ownedCols + ` FROM owned_parts ORDER BY updated_at DESC`)
}

func (d *DB) queryOwned(q string, args ...any) ([]OwnedPart, error) {
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("listing owned parts: %w", err)
	}
	defer rows.Close()
	var out []OwnedPart
	for rows.Next() {
		p, err := scanOwned(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (d *DB) SetSyncedPartID(ownedID int64, partDBID int) error {
	_, err := d.Exec(`UPDATE owned_parts SET synced_part_id = ? WHERE id = ?`, partDBID, ownedID)
	return err
}

// OptionalCategory is the catalog category that defaults to optional (see IsOptional)
// without anyone having to mark a single sticker sheet by hand.
const OptionalCategory = "Stickers"

// IsOptional reports whether a part counts toward missing-parts/completion totals.
// An explicit SetOptional call always wins; absent that, only Stickers default to
// optional — so every sticker sheet, existing or new, is covered for free.
func (d *DB) IsOptional(partNum, category string) (bool, error) {
	var v int
	err := d.QueryRow(`SELECT optional FROM part_optional WHERE part_num = ?`, partNum).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return category == OptionalCategory, nil
	}
	if err != nil {
		return false, fmt.Errorf("looking up optional flag for %s: %w", partNum, err)
	}
	return v != 0, nil
}

// SetOptional records an explicit optional/required choice for a part number,
// overriding the category default either way.
func (d *DB) SetOptional(partNum string, optional bool) error {
	v := 0
	if optional {
		v = 1
	}
	_, err := d.Exec(`INSERT INTO part_optional (part_num, optional) VALUES (?, ?)
		ON CONFLICT(part_num) DO UPDATE SET optional = excluded.optional`, partNum, v)
	return err
}

// DeleteOwnedPart removes one part+colour row (used to undo an add).
func (d *DB) DeleteOwnedPart(partNum string, colorID int, colorName string) error {
	d.ensureDailySnapshot()
	before := 0
	if cur, err := d.GetOwnedPart(partNum, colorID, colorName); err == nil && cur != nil {
		before = cur.Qty
	}
	res, err := d.Exec(`DELETE FROM owned_parts WHERE `+ownedMatch, partNum, colorID, colorName)
	if err == nil {
		if n, _ := res.RowsAffected(); n > 0 {
			d.journal(d.DB, "delete", "part", partNum, colorID, colorName, before, 0, "")
		}
	}
	return err
}
