package lego

import (
	"regexp"
	"strings"
)

// NLFilter is what a language model turns "red 2x4 bricks under 10p" into. It is
// UNTRUSTED (a model produced it from user text and part names): Clean bounds and
// sanitises every field, and SearchOwnedNL only ever binds it as SQL parameters.
type NLFilter struct {
	Words    []string `json:"words"`
	Color    string   `json:"color"`
	MaxPrice float64  `json:"max_price"` // in the price currency's main unit (10p = 0.10)
}

var (
	safeWord = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 .\-/]{0,29}$`)
	dimsWord = regexp.MustCompile(`^(\d+)\s*[xX×]\s*(\d+)$`)
)

// Clean returns a copy that is safe to use: at most 6 words of letters/digits/space/.-/, "2x4" written the
// way part names are ("2 x 4"), a bounded colour, and a price between 0 and 10000.
func (f NLFilter) Clean() NLFilter {
	var out NLFilter
	for _, w := range f.Words {
		w = strings.TrimSpace(w)
		if m := dimsWord.FindStringSubmatch(w); m != nil {
			w = m[1] + " x " + m[2]
		}
		if safeWord.MatchString(w) && len(out.Words) < 6 {
			out.Words = append(out.Words, strings.ToLower(w))
		}
	}
	if c := strings.TrimSpace(f.Color); safeWord.MatchString(c) {
		out.Color = strings.ToLower(c)
	}
	if f.MaxPrice > 0 && f.MaxPrice <= 10000 {
		out.MaxPrice = f.MaxPrice
	}
	return out
}

type NLHit struct {
	PartNum, Name, ColorName string
	ColorID, Qty             int
	Price                    float64 // stored average, 0 = not priced
	Currency                 string
}

// SearchOwnedNL finds owned stock matching every word (in the part name or number), the colour, and the price cap.
// With a price cap, unpriced lots are left out (an unknown price is not "cheap").
func (d *DB) SearchOwnedNL(f NLFilter, limit int) ([]NLHit, error) {
	f = f.Clean()
	q := `SELECT part_num, name, color_id, color_name, qty FROM owned_parts WHERE qty > 0`
	var args []any
	for _, w := range f.Words {
		q += ` AND (lower(name) LIKE ? ESCAPE '\' OR lower(part_num) LIKE ? ESCAPE '\')`
		like := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(w) + "%"
		args = append(args, like, like)
	}
	if f.Color != "" {
		q += ` AND lower(color_name) LIKE ? ESCAPE '\'`
		args = append(args, "%"+strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(f.Color)+"%")
	}
	rows, err := d.Query(q+` ORDER BY qty DESC LIMIT 500`, args...)
	if err != nil {
		return nil, err
	}
	type cand struct {
		h     NLHit
		color int
	}
	var cands []cand
	for rows.Next() {
		var c cand
		if err := rows.Scan(&c.h.PartNum, &c.h.Name, &c.color, &c.h.ColorName, &c.h.Qty); err != nil {
			rows.Close()
			return nil, err
		}
		c.h.ColorID = c.color
		cands = append(cands, c)
	}
	rows.Close()
	var out []NLHit
	for _, c := range cands {
		bl := 0
		if c.color >= 0 {
			if id, ok := d.BLColorFor(c.color); ok {
				bl = id
			}
		}
		if p, _ := d.getPrice("PART", c.h.PartNum, bl, "U"); p != nil && !p.Missing {
			c.h.Price, c.h.Currency = p.Avg, p.Currency
		}
		if f.MaxPrice > 0 && (c.h.Price == 0 || c.h.Price > f.MaxPrice) {
			continue
		}
		out = append(out, c.h)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}
