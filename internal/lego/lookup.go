package lego

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// PartLookup is everything known about a part number before the user types the
// colour and quantity: where the details came from, and any problems worth
// showing (a rejected API key, a rate limit) that did not stop the lookup.
type PartLookup struct {
	Num         string
	Name        string
	Category    string  // Rebrickable's category name, "" if unknown
	Colors      []Color // the colours this part exists in; may be empty
	Source      string
	BrickLinkID string // from Rebrickable's external ids when the live API answered
	Notes       []string
}

// LookupPart resolves a part number: offline catalog first (instant, no key, no
// rate limit), then live Rebrickable for anything the catalog lacks, then what
// you already hold. It returns nil when nothing knows the part — the caller
// then asks for every detail by hand. It never fails: problems become Notes.
func (d *DB) LookupPart(ctx context.Context, rb *Client, num string) *PartLookup {
	num = strings.TrimSpace(num)
	if num == "" {
		return nil
	}
	info := &PartLookup{Num: num}

	if cp, err := d.CatalogPart(num); err == nil && cp != nil {
		info.Num, info.Name, info.Category, info.Source = cp.Num, cp.Name, cp.Category, "offline catalog"
		if cols, err := d.CatalogColorsFor(cp.Num); err == nil {
			info.Colors = cols
		}
	}

	needLive := info.Name == "" || len(info.Colors) == 0
	if needLive && rb != nil && rb.Enabled() {
		d.liveLookup(ctx, rb, info)
	}

	if info.Name == "" { // the old imported lookup file
		if ref, err := d.GetRefPart(num); err == nil && ref != nil {
			info.Name, info.Category, info.Source = ref.Name, ref.Category, "legacy import"
		}
	}
	if info.Name == "" { // last resort: something you already own under this number
		if owned, err := d.OwnedPartsOf(num); err == nil && len(owned) > 0 {
			info.Name, info.Category, info.Source = owned[0].Name, owned[0].Category, "your collection"
		}
	}
	if info.Name == "" {
		return nil
	}
	return info
}

func (d *DB) liveLookup(ctx context.Context, rb *Client, info *PartLookup) {
	fromCatalog := info.Name != ""
	if !fromCatalog {
		pd, err := rb.GetPart(ctx, info.Num)
		if err != nil {
			info.note(err, fmt.Sprintf("Rebrickable has no part %q", info.Num))
			return
		}
		info.Num, info.Name, info.Source = pd.PartNum, pd.Name, "Rebrickable (live)"
		info.BrickLinkID = pd.ExternalID("BrickLink")
		if cat, err := rb.PartCategory(ctx, pd.CatID); err == nil {
			info.Category = cat
		} else {
			info.note(err, "Rebrickable has no category for this part")
		}
	}
	if len(info.Colors) == 0 {
		pcs, err := rb.GetPartColors(ctx, info.Num)
		if err != nil {
			info.note(err, "")
			return
		}
		rgb := d.colorTable(ctx, rb)
		for _, pc := range pcs {
			c := Color{ID: pc.ColorID, Name: pc.ColorName}
			if known, ok := rgb[pc.ColorID]; ok {
				c.RGB, c.Trans = known.RGB, known.Trans
			}
			info.Colors = append(info.Colors, c)
		}
		if fromCatalog {
			info.Source += " + Rebrickable (live)"
		}
	}
}

// colorTable maps colour id to colour, for swatches: the offline catalog if it
// has one, else the live colour table (a single cached request).
func (d *DB) colorTable(ctx context.Context, rb *Client) map[int]Color {
	out := map[int]Color{}
	if cols, err := d.CatalogColors(); err == nil {
		for _, c := range cols {
			out[c.ID] = c
		}
	}
	if len(out) == 0 && rb != nil && rb.Enabled() {
		if all, err := rb.AllColors(ctx); err == nil {
			for _, c := range all {
				out[c.ID] = Color{ID: c.ID, Name: c.Name, RGB: c.RGB, Trans: c.IsTrans}
			}
		}
	}
	return out
}

// note turns a Rebrickable error into a message for the user. notFound is what
// to say for a plain 404 ("" = say nothing about it).
func (info *PartLookup) note(err error, notFound string) {
	switch {
	case errors.Is(err, ErrNotFound):
		if notFound != "" {
			info.Notes = append(info.Notes, notFound+".")
		}
	default:
		info.Notes = append(info.Notes, "Rebrickable lookup failed: "+err.Error()+".")
	}
}

// MatchColor picks a colour from list by Rebrickable id ("4") or name
// ("red", case-insensitive; a unique substring also matches).
func MatchColor(list []Color, input string) (Color, bool) {
	input = strings.TrimSpace(input)
	if input == "" {
		return Color{}, false
	}
	if n, err := strconv.Atoi(input); err == nil {
		for _, c := range list {
			if c.ID == n {
				return c, true
			}
		}
	}
	var hits []Color
	for _, c := range list {
		if strings.EqualFold(c.Name, input) {
			return c, true
		}
		if strings.Contains(strings.ToLower(c.Name), strings.ToLower(input)) {
			hits = append(hits, c)
		}
	}
	if len(hits) == 1 {
		return hits[0], true
	}
	return Color{}, false
}

// ColorByID returns a colour from the offline catalog's colour table.
func (d *DB) ColorByID(id int) (Color, bool) {
	var c Color
	var trans int
	if err := d.QueryRow(`SELECT id, name, rgb, is_trans FROM cat_colors WHERE id = ?`, id).Scan(&c.ID, &c.Name, &c.RGB, &trans); err != nil {
		return Color{}, false
	}
	c.Trans = trans != 0
	return c, true
}
