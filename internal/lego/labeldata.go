package lego

import (
	"strings"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/labels"
)

// LabelData is what a set's label says: from the catalog (name, year, pieces), your
// collection, and the last parts check (missing, on order, who checked it, when).
func (d *DB) LabelData(setNum string) labels.Data {
	num := strings.TrimSpace(setNum)
	if !strings.Contains(num, "-") {
		num += "-1"
	}
	l := labels.Data{SetNum: num, QR: "https://rebrickable.com/sets/" + num + "/"}
	if s, _ := d.CatalogSet(num); s != nil {
		l.Name, l.Year, l.Pieces = s.Name, s.Year, s.Pieces
	}
	if mine, _ := d.GetSetByNum(strings.TrimSuffix(num, "-1")); mine != nil {
		if l.Name == "" {
			l.Name, l.Year = mine.Name, mine.Year
		}
		if l.Pieces == 0 {
			l.Pieces = mine.PartsQty
		}
	}
	st := d.GetSetState(num)
	l.Missing, l.OnOrder, l.Location = st.MissingQty, st.OnOrderQty, st.Location
	if c := st.LastCheck; c != nil {
		l.Checked, l.CheckedBy, l.CheckedAt = true, c.CheckedBy, c.FinishedAt
		if full, _ := d.GetCheck(c.ID); full != nil {
			l.Pieces, _, _, _, _ = full.Totals()
		}
	}
	return l
}

// LabelSets resolves "all" (every set you own), "incomplete" or set numbers.
func (d *DB) LabelSets(args []string) ([]string, error) {
	var out []string
	for _, a := range args {
		switch strings.ToLower(strings.TrimSpace(a)) {
		case "":
		case "all":
			sets, err := d.SearchSets("")
			if err != nil {
				return nil, err
			}
			for _, s := range sets {
				out = append(out, s.SetNum)
			}
		case "incomplete":
			inc, err := d.IncompleteSets()
			if err != nil {
				return nil, err
			}
			for _, s := range inc {
				out = append(out, s.SetNum)
			}
		default:
			out = append(out, a)
		}
	}
	return out, nil
}
