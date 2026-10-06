package lego

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// BagSmall/BagMain are the two bags a part can go in while picking/checking
// — small, easy-to-lose parts (loose 1x1s, minifig accessories, anything
// that wants its own bag rather than going in with everything else) versus
// everything else. BagMain is the default: nothing is "small" unless an
// admin has said so.
const (
	BagSmall = "small"
	BagMain  = "main"
)

// PartBagSize reports which bag partNum goes in. An explicit SetBagSize call
// always wins; absent that, every part defaults to the main bag — there's no
// category-based default here the way IsOptional has for Stickers, since bag
// size doesn't follow from the catalog the same reliable way.
func (d *DB) PartBagSize(partNum string) (string, error) {
	var v string
	err := d.QueryRow(`SELECT size FROM part_bag_size WHERE part_num = ?`, partNum).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return BagMain, nil
	}
	if err != nil {
		return BagMain, fmt.Errorf("looking up bag size for %s: %w", partNum, err)
	}
	return v, nil
}

// SetBagSize records an explicit small/main choice for partNum. Setting it
// to BagMain explicitly (rather than clearing the row) is harmless — same
// effective result as no row at all, just an audit trail of who decided.
func (d *DB) SetBagSize(partNum, size string) error {
	if size != BagSmall && size != BagMain {
		return fmt.Errorf("size must be %q or %q", BagSmall, BagMain)
	}
	_, err := d.Exec(`INSERT INTO part_bag_size (part_num, size) VALUES (?, ?)
		ON CONFLICT(part_num) DO UPDATE SET size = excluded.size`, partNum, size)
	return err
}

// SmallBagLines is the subset of c's lines that go in the small bag — what
// a "does this check need a bag code yet?" check and the small-bag label
// both actually need, not every line.
func SmallBagLines(c *SetCheck) []CheckLine {
	var out []CheckLine
	for _, l := range c.Lines {
		if l.SmallBag {
			out = append(out, l)
		}
	}
	return out
}

// RecordCheckBag confirms checkID's small bag's own barcode/number — the
// verified link between a physical bag and the set it belongs to, not just
// a printed label. Upserts: scanning a different code later (a mis-scan,
// corrected) replaces it rather than erroring.
func (d *DB) RecordCheckBag(checkID int64, bagCode, createdBy string) error {
	if bagCode == "" {
		return errors.New("a bag code is required")
	}
	_, err := d.Exec(`INSERT INTO check_bags (check_id, bag_code, created_by, created_at) VALUES (?,?,?,?)
		ON CONFLICT(check_id) DO UPDATE SET bag_code = excluded.bag_code, created_by = excluded.created_by, created_at = excluded.created_at`,
		checkID, bagCode, createdBy, time.Now().Format(time.RFC3339))
	return err
}

// CheckBagCode is checkID's confirmed small-bag code, or "" if none has been recorded yet.
func (d *DB) CheckBagCode(checkID int64) (string, error) {
	var code string
	err := d.QueryRow(`SELECT bag_code FROM check_bags WHERE check_id = ?`, checkID).Scan(&code)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return code, err
}
