package lego

import (
	"strings"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/partdb"
)

// QualityIssue is one thing worth fixing in the stock data.
type QualityIssue struct {
	Kind, Part, Colour, Detail string
	Qty                        int
}

const (
	IssueNoLocation = "no shelf location"
	IssueFreeColour = "free-text colour"
	IssueNoBLColour = "no BrickLink colour (can't be priced)"
	IssueStalePrice = "price missing or older than 30 days"
)

// DataQuality scans owned stock for the problems that make other features quietly
// worse: lots with no Part-DB shelf location (pick lists can't place them), colours
// typed as free text that match a real colour (they split one lot in two and never
// price), colours with no BrickLink mapping, and lots with no recent price. pdb may
// be nil (then location checks are skipped). Nothing is changed.
func (d *DB) DataQuality(pdb *partdb.DB) ([]QualityIssue, error) {
	owned, err := d.ListOwnedParts()
	if err != nil {
		return nil, err
	}
	var held []OwnedPart
	for _, o := range owned {
		if o.Qty > 0 {
			held = append(held, o)
		}
	}
	var out []QualityIssue
	if pdb != nil {
		part, color, name := make([]string, len(held)), make([]int, len(held)), make([]string, len(held))
		for i, o := range held {
			part[i], color[i], name[i] = o.PartNum, o.ColorID, o.ColorName
		}
		for i, loc := range d.LocationNames(pdb, part, color, name) {
			if loc == "" {
				out = append(out, QualityIssue{IssueNoLocation, held[i].PartNum, held[i].ColorName, "not linked to a Part-DB lot with a storage location", held[i].Qty})
			}
		}
	}
	known := map[string]int{}
	rows, err := d.Query(`SELECT id, name FROM cat_colors`)
	if err == nil {
		for rows.Next() {
			var id int
			var n string
			if rows.Scan(&id, &n) == nil {
				known[strings.ToLower(n)] = id
			}
		}
		rows.Close()
	}
	stale := time.Now().AddDate(0, 0, -30)
	for _, o := range held {
		if o.ColorID < 0 {
			if _, ok := known[strings.ToLower(strings.TrimSpace(o.ColorName))]; ok {
				out = append(out, QualityIssue{IssueFreeColour, o.PartNum, o.ColorName, "typed as text but \"" + o.ColorName + "\" is a real colour: re-add it with the colour picked", o.Qty})
			}
			continue
		}
		bl, ok := d.BLColorFor(o.ColorID)
		if !ok {
			out = append(out, QualityIssue{IssueNoBLColour, o.PartNum, o.ColorName, "run the BrickLink colour sync", o.Qty})
			continue
		}
		p, _ := d.getPrice("PART", o.PartNum, bl, "U")
		if p == nil || p.Missing || p.FetchedAt.Before(stale) {
			out = append(out, QualityIssue{IssueStalePrice, o.PartNum, o.ColorName, "refresh prices", o.Qty})
		}
	}
	return out, nil
}
