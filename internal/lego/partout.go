package lego

// PartOut is what a set is worth taken apart and sold as loose parts, from
// stored BrickLink average prices. Unpriced lines are never counted as zero:
// Coverage says how much of the set the number actually reflects.
type PartOut struct {
	SetNum    string  `json:"set_num"`
	Cond      string  `json:"cond"`
	Currency  string  `json:"currency,omitempty"`
	Lines     int     `json:"lines"`
	Priced    int     `json:"priced"`
	Coverage  float64 `json:"coverage"` // priced pieces / all pieces, 0..1
	Gross     float64 `json:"gross"`
	Fees      float64 `json:"fees"`
	Labour    float64 `json:"labour"`
	BreakEven float64 `json:"break_even"` // the most to pay for the set and still make nothing
}

// computePartOut is the pure arithmetic: lookup returns a line's average price.
func computePartOut(items []InvItem, lookup func(InvItem) (avg float64, cur string, ok bool), feePct, labourPct float64) PartOut {
	var out PartOut
	var pieces, pricedPieces int
	for _, it := range items {
		out.Lines++
		pieces += it.Qty
		avg, cur, ok := lookup(it)
		if !ok {
			continue
		}
		out.Priced++
		pricedPieces += it.Qty
		out.Gross += avg * float64(it.Qty)
		if out.Currency == "" {
			out.Currency = cur
		}
	}
	if pieces > 0 {
		out.Coverage = float64(pricedPieces) / float64(pieces)
	}
	out.Fees = out.Gross * feePct / 100
	out.Labour = out.Gross * labourPct / 100
	out.BreakEven = out.Gross - out.Fees - out.Labour
	return out
}

// PartOutValue totals a catalog set's inventory against the prices already
// stored (it never calls BrickLink, so it costs no API budget).
func (d *DB) PartOutValue(setNum, cond string, feePct, labourPct float64) (*PartOut, error) {
	items, err := d.CatalogSetInventory(setNum)
	if err != nil {
		return nil, err
	}
	if cond != "N" {
		cond = "U"
	}
	out := computePartOut(items, func(it InvItem) (float64, string, bool) {
		bl := it.BLColor
		if bl == 0 && it.ColorID >= 0 {
			if id, ok := d.BLColorFor(it.ColorID); ok {
				bl = id
			}
		}
		p, err := d.getPrice("PART", it.PartNum, bl, cond)
		if err != nil || p == nil || p.Missing {
			return 0, "", false
		}
		return p.Avg, p.Currency, true
	}, feePct, labourPct)
	out.SetNum, out.Cond = setNum, cond
	return &out, nil
}
