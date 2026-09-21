package lego

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// The search chain used by every screen and command: the offline catalog first
// (instant, no key, no rate limit, works with the network down), live Rebrickable
// only when the catalog has nothing (a brand-new set, or no catalog yet), and the
// old imported reference tables as the last resort. Each result says where it came
// from, and a failing source is reported as a note rather than blanking the screen.

const searchLimit = 50

// CatalogLabel describes the offline catalog for a result title: "offline
// catalog, refreshed 2026-09-20", or just "offline catalog" if the date is unknown.
func (d *DB) CatalogLabel() string {
	if ms, err := strconv.ParseInt(d.metaGet("refreshed_ms"), 10, 64); err == nil {
		return "offline catalog, refreshed " + time.UnixMilli(ms).Format("2006-01-02")
	}
	return "offline catalog"
}

func (d *DB) catalogEmpty() bool {
	parts, _, _, _ := d.CatalogStatus()
	return parts == 0
}

// SetSearch is the outcome of searching for sets.
type SetSearch struct {
	Hits   []SetHit
	Source string
	Notes  []string
}

// FindSets searches by name, number or theme through the chain.
func (d *DB) FindSets(ctx context.Context, rb *Client, term string) *SetSearch {
	res := &SetSearch{}
	if hits, err := d.SearchCatalogSets(term, searchLimit); err != nil {
		res.Notes = append(res.Notes, "Offline catalog search failed: "+err.Error()+".")
	} else if len(hits) > 0 {
		res.Hits, res.Source = hits, d.CatalogLabel()
		return res
	}
	if rb != nil && rb.Enabled() {
		live, err := rb.SearchSets(ctx, term)
		if err == nil && len(live) > 0 {
			for _, r := range live {
				res.Hits = append(res.Hits, SetHit{Num: r.SetNum, Name: r.Name, Year: r.Year, Pieces: r.Pieces})
			}
			res.Source = "live — Rebrickable"
			return res
		}
		if err != nil {
			res.Notes = append(res.Notes, "Rebrickable is unreachable ("+shorten(err.Error(), 80)+") — showing what is stored locally.")
		}
	}
	if local, err := d.SearchRefSets(term); err == nil && len(local) > 0 {
		for _, r := range local {
			res.Hits = append(res.Hits, SetHit{Num: r.SetNum, Name: r.Name, Theme: r.Theme, Year: atoi(r.Year), Pieces: atoi(r.TotalPieces)})
		}
		res.Source = "local — imported lookup file"
		return res
	}
	res.Source = "no match"
	if d.catalogEmpty() && (rb == nil || !rb.Enabled()) {
		res.Notes = append(res.Notes, "There is no offline catalog yet and no Rebrickable key: run `wms lego catalog refresh`, or add a key under Admin > Settings & API Keys.")
	}
	return res
}

// PartSearch is the outcome of searching for parts.
type PartSearch struct {
	Hits   []PartHit
	Source string
	Notes  []string
}

// FindParts searches by name or number through the chain. Live results carry a
// category id (Rebrickable's search returns no name), shown as "category N".
func (d *DB) FindParts(ctx context.Context, rb *Client, term string) *PartSearch {
	res := &PartSearch{}
	if hits, err := d.SearchCatalogParts(term, searchLimit); err != nil {
		res.Notes = append(res.Notes, "Offline catalog search failed: "+err.Error()+".")
	} else if len(hits) > 0 {
		res.Hits, res.Source = hits, d.CatalogLabel()
		return res
	}
	if rb != nil && rb.Enabled() {
		live, err := rb.SearchParts(ctx, term)
		if err == nil && len(live) > 0 {
			for _, p := range live {
				cat := ""
				if p.PartCat > 0 {
					if name, cerr := rb.PartCategory(ctx, p.PartCat); cerr == nil {
						cat = name
					} else if !errors.Is(cerr, ErrNotFound) {
						cat = fmt.Sprintf("category %d", p.PartCat)
					}
				}
				res.Hits = append(res.Hits, PartHit{Num: p.PartNum, Name: p.Name, Category: cat})
			}
			res.Source = "live — Rebrickable"
			return res
		}
		if err != nil {
			res.Notes = append(res.Notes, "Rebrickable is unreachable ("+shorten(err.Error(), 80)+") — showing what is stored locally.")
		}
	}
	if local, err := d.SearchRefParts(term); err == nil && len(local) > 0 {
		for _, r := range local {
			res.Hits = append(res.Hits, PartHit{Num: r.PartNum, Name: r.Name, Category: r.Category})
		}
		res.Source = "local — imported lookup file"
		return res
	}
	res.Source = "no match"
	if d.catalogEmpty() && (rb == nil || !rb.Enabled()) {
		res.Notes = append(res.Notes, "There is no offline catalog yet and no Rebrickable key: run `wms lego catalog refresh`, or add a key under Admin > Settings & API Keys.")
	}
	return res
}

func shorten(s string, n int) string {
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}
