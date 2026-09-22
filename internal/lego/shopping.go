package lego

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/brickowl"
)

// The shopping list for a set's missing parts: what is short, what is already on
// order, spares you hold elsewhere, and what BrickLink and BrickOwl charge — with
// links to each shop so you can buy them.

type ShoppingLine struct {
	CheckLine
	Short, OnOrder, Spare int
	ElementID             string
	BL, BO                float64 // average price each; 0 = unknown
	BOLow                 float64
	BLCur, BOCur          string
	BOID                  string
}

// Cheapest is the lower known price and where ("", 0 when neither is known).
func (l ShoppingLine) Cheapest() (string, float64) {
	switch {
	case l.BL > 0 && (l.BO <= 0 || l.BL <= l.BO):
		return "BrickLink", l.BL
	case l.BO > 0:
		return "BrickOwl", l.BO
	}
	return "", 0
}

// ToBuy is how many still need ordering.
func (l ShoppingLine) ToBuy() int { return max(0, l.Short-l.OnOrder) }

// ShoppingList lists a checked set's missing lines with what is known about them
// (stored prices only; FetchPrices gets fresh ones).
func (d *DB) ShoppingList(setNum string) ([]ShoppingLine, error) {
	c, err := d.LastCheck(setNum)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, fmt.Errorf("set %s has not been checked yet", setNum)
	}
	var out []ShoppingLine
	for _, l := range c.Lines {
		if l.Missing() == 0 {
			continue
		}
		s := ShoppingLine{CheckLine: l, Short: l.Missing(), OnOrder: d.onOrder(setNum, l.PartNum, l.ColorID), ElementID: d.ElementFor(l.PartNum, l.ColorID)}
		if sp, _ := d.SparesFor(l.PartNum, l.ColorID); len(sp) > 0 {
			for _, x := range sp {
				if x.OriginSet != setNum {
					s.Spare += x.Qty
				}
			}
		}
		d.fillPrices(&s)
		out = append(out, s)
	}
	return out, nil
}

func (d *DB) blKey(l CheckLine) (string, int) {
	no, col := l.BLID, l.BLColor
	if no == "" {
		no = l.PartNum
	}
	if col == 0 && l.ColorID >= 0 {
		col, _ = d.BLColorFor(l.ColorID)
	}
	return no, col
}

func (d *DB) fillPrices(s *ShoppingLine) {
	no, col := d.blKey(s.CheckLine)
	for _, cond := range []string{"N", "U"} {
		if p, _ := d.getPrice("PART", no, col, cond); p != nil && !p.Missing && p.Avg > 0 {
			s.BL, s.BLCur = p.Avg, p.Currency
			break
		}
	}
	var missing int
	_ = d.QueryRow(`SELECT boid, avg, low, currency, missing FROM bo_prices WHERE bl_id = ? AND bl_color = ?`, no, col).
		Scan(&s.BOID, &s.BO, &s.BOLow, &s.BOCur, &missing)
}

// FetchPrices refreshes BrickLink and BrickOwl prices for the list's lines whose
// stored price is missing or older than a week. bl or bo may be nil (not set up).
// It returns how many prices it fetched and the first error per source.
func (d *DB) FetchPrices(ctx context.Context, lines []ShoppingLine, bl PriceFetcher, bo *brickowl.Client, cond string) (fetched int, errs []error) {
	var blErr, boErr error
	for i := range lines {
		l := &lines[i]
		no, col := d.blKey(l.CheckLine)
		if bl != nil && blErr == nil {
			if p, _ := d.getPrice("PART", no, col, cond); p == nil || time.Since(p.FetchedAt) > priceMaxAge {
				avg, cur, found, err := bl.AvgPrice(ctx, "PART", no, col, cond)
				if err != nil {
					blErr = fmt.Errorf("BrickLink: %w", err)
				} else {
					_ = d.StorePrice("PART", no, col, cond, avg, cur, !found)
					fetched++
				}
			}
		}
		if bo.Enabled() && boErr == nil {
			var at string
			fresh := d.QueryRow(`SELECT fetched_at FROM bo_prices WHERE bl_id = ? AND bl_color = ?`, no, col).Scan(&at) == nil
			if t, err := time.Parse(timeLayout, at); !fresh || err != nil || time.Since(t) > priceMaxAge {
				boid, err := bo.Lookup(ctx, l.ElementID, l.PartNum)
				var p *brickowl.Price
				if err == nil {
					p, err = bo.Availability(ctx, boid)
				}
				switch {
				case errors.Is(err, brickowl.ErrNotFound):
					d.storeBO(no, col, boid, nil)
					fetched++
				case err != nil:
					boErr = err
				default:
					d.storeBO(no, col, boid, p)
					fetched++
				}
			}
		}
		d.fillPrices(l)
	}
	for _, e := range []error{blErr, boErr} {
		if e != nil {
			errs = append(errs, e)
		}
	}
	return fetched, errs
}

func (d *DB) storeBO(no string, col int, boid string, p *brickowl.Price) {
	now := time.Now().UTC().Format(timeLayout)
	if p == nil {
		_, _ = d.Exec(`INSERT OR REPLACE INTO bo_prices (bl_id, bl_color, boid, missing, fetched_at) VALUES (?,?,?,1,?)`, no, col, boid, now)
		return
	}
	_, _ = d.Exec(`INSERT OR REPLACE INTO bo_prices (bl_id, bl_color, boid, avg, low, currency, missing, fetched_at) VALUES (?,?,?,?,?,?,0,?)`,
		no, col, p.BOID, p.Avg, p.Low, p.Currency, now)
}

// ElementFor is a LEGO element id for part+colour ("" when the catalog has none).
func (d *DB) ElementFor(partNum string, colorID int) string {
	var id string
	if err := d.QueryRow(`SELECT element_id FROM cat_element_ids WHERE part_num = ? AND color_id = ? ORDER BY length(element_id), element_id LIMIT 1`, partNum, colorID).Scan(&id); errors.Is(err, sql.ErrNoRows) {
		return ""
	}
	return id
}

// ShopLinks are where to buy a line.
func (d *DB) ShopLinks(l ShoppingLine) [][2]string {
	no, col := d.blKey(l.CheckLine)
	bl := "https://www.bricklink.com/v2/catalog/catalogitem.page?P=" + url.QueryEscape(no)
	if col > 0 {
		bl += fmt.Sprintf("#T=S&C=%d", col)
	}
	links := [][2]string{
		{"BrickLink", bl},
		{"BrickOwl", brickowl.PartURL(l.BOID, l.PartNum)},
		{"Rebrickable", fmt.Sprintf("https://rebrickable.com/parts/%s/", url.PathEscape(l.PartNum))},
	}
	if l.ElementID != "" {
		links = append(links, [2]string{"LEGO Pick a Brick", "https://www.lego.com/pick-and-build/pick-a-brick?query=" + url.QueryEscape(l.ElementID)})
	} else {
		links = append(links, [2]string{"LEGO Pick a Brick", "https://www.lego.com/pick-and-build/pick-a-brick?query=" + url.QueryEscape(l.PartNum)})
	}
	return links
}

// ShoppingExport turns the list into export rows (Qty = still to buy).
func (d *DB) ShoppingExport(setNum string, lines []ShoppingLine) *ExportData {
	data := &ExportData{Title: "Shopping list for set " + setNum, When: time.Now()}
	for _, l := range lines {
		if l.ToBuy() == 0 {
			continue
		}
		_, col := d.blKey(l.CheckLine)
		data.Rows = append(data.Rows, ExportRow{PartNum: l.PartNum, Name: strings.TrimSpace(l.PartName), Category: l.Category, ColorID: l.ColorID, ColorName: l.ColorName, Qty: l.ToBuy(), BLColor: col})
	}
	return data
}
