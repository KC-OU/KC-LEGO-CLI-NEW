package lego

import (
	"database/sql"
	"errors"
	"fmt"
)

// BagSmall/BagMain/BagExtra are the built-in bag_kinds.key values -- small,
// easy-to-lose parts (loose 1x1s, minifig accessories) versus everything
// else, plus a third kind for spares a check turns up (see bags.go). BagMain
// is the default: nothing is "small" unless an admin has said so. An admin
// can register further kinds (see AddBagKind) beyond these three.
const (
	BagSmall = "small"
	BagMain  = "main"
	BagExtra = "extra"
)

// PartBagSize reports which bag kind partNum goes in (never BagExtra -- that
// one is reached only by marking a line Extra during a check, not a part's
// own default). An explicit SetBagSize call always wins; absent that, every
// part defaults to the main bag — there's no category-based default here the
// way IsOptional has for Stickers, since bag size doesn't follow from the
// catalog the same reliable way.
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

// SetBagSize records an explicit bag-kind choice for partNum, validated
// against the registered kinds (BagSmall/BagMain plus whatever an admin has
// added via AddBagKind) -- never BagExtra, which a part is never assigned to
// directly. Setting it to BagMain explicitly (rather than clearing the row)
// is harmless — same effective result as no row at all, just an audit trail
// of who decided.
func (d *DB) SetBagSize(partNum, size string) error {
	if size == BagExtra {
		return fmt.Errorf("%q is for spares found during a check, not a part's own bag", BagExtra)
	}
	kinds, err := d.BagKinds()
	if err != nil {
		return err
	}
	ok := false
	for _, k := range kinds {
		if k.Key == size {
			ok = true
			break
		}
	}
	if !ok {
		return fmt.Errorf("%q isn't a registered bag kind — see `wms lego bag-kind add` to register one", size)
	}
	_, err = d.Exec(`INSERT INTO part_bag_size (part_num, size) VALUES (?, ?)
		ON CONFLICT(part_num) DO UPDATE SET size = excluded.size`, partNum, size)
	return err
}

// SmallBagLines is the subset of c's lines that go in the small bag — what
// the small-bag label needs, not every line. (The live bag-gate itself
// resolves each line's required kind fresh via RequiredBagKind in bags.go,
// not this helper, since that also needs to recognise BagExtra lines.)
func SmallBagLines(c *SetCheck) []CheckLine {
	var out []CheckLine
	for _, l := range c.Lines {
		if l.SmallBag {
			out = append(out, l)
		}
	}
	return out
}
