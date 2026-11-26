package lego

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
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
// A term that names a theme ("star wars", "technic") lists every set in it, with the
// sets whose name or number match after them; "/" on the screen narrows the list.
func (d *DB) FindSets(ctx context.Context, rb *Client, term string) *SetSearch {
	if nums := bulkSetNums(term); nums != nil {
		return d.findSetList(ctx, rb, nums)
	}
	res := &SetSearch{}
	themed, terr := d.SearchCatalogThemeSets(term)
	hits, err := d.SearchCatalogSets(term, searchLimit)
	if err = errors.Join(terr, err); err != nil {
		res.Notes = append(res.Notes, "Offline catalog search failed: "+err.Error()+".")
	}
	if seen := make(map[string]bool, len(themed)); len(themed)+len(hits) > 0 {
		for _, h := range themed {
			seen[h.Num] = true
		}
		for _, h := range hits {
			if !seen[h.Num] {
				themed = append(themed, h)
			}
		}
		res.Hits, res.Source = themed, d.CatalogLabel()
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

// SortSetHits orders a result list: "year" newest first, "year_asc" oldest first, "num" /
// "num_desc" by set number (numerically, so 3866 comes before 40622). Any other mode keeps
// the order the search gave. Ties keep their relative order.
func SortSetHits(hits []SetHit, mode string) {
	setNo := func(n string) int { v, _ := strconv.Atoi(strings.SplitN(n, "-", 2)[0]); return v }
	var less func(a, b SetHit) bool
	switch mode {
	case "year":
		less = func(a, b SetHit) bool { return a.Year > b.Year }
	case "year_asc":
		less = func(a, b SetHit) bool { return a.Year < b.Year }
	case "num":
		less = func(a, b SetHit) bool { return setNo(a.Num) < setNo(b.Num) }
	case "num_desc":
		less = func(a, b SetHit) bool { return setNo(a.Num) > setNo(b.Num) }
	default:
		return
	}
	sort.SliceStable(hits, func(i, j int) bool { return less(hits[i], hits[j]) })
}

var setNumRE = regexp.MustCompile(`^\d+(-\d+)?$`)

// bulkSetNums reads "3866, 40622,75344" as a list of set numbers; nil when the term
// is not a comma list of numbers (so a name containing a comma still searches normally).
func bulkSetNums(term string) []string {
	if !strings.Contains(term, ",") {
		return nil
	}
	var nums []string
	seen := map[string]bool{}
	for _, t := range strings.FieldsFunc(term, func(r rune) bool { return r == ',' || r == ';' || unicode.IsSpace(r) }) {
		if !setNumRE.MatchString(t) {
			return nil
		}
		if !seen[t] {
			seen[t] = true
			nums = append(nums, t)
		}
	}
	return nums
}

// findSetList shows exactly the sets typed, in the order typed, each looked up
// through the usual chain (offline catalog, live Rebrickable, imported table).
func (d *DB) findSetList(ctx context.Context, rb *Client, nums []string) *SetSearch {
	res := &SetSearch{Source: "bulk list, " + d.CatalogLabel()}
	var missing []string
	for _, n := range nums {
		l := d.LookupSet(ctx, rb, n)
		if !l.Found() {
			missing = append(missing, n)
			continue
		}
		res.Hits = append(res.Hits, l.Set)
	}
	if len(missing) > 0 {
		res.Notes = append(res.Notes, "Not found: "+strings.Join(missing, ", ")+".")
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
// A term that names a theme also lists every part used by a set in it.
func (d *DB) FindParts(ctx context.Context, rb *Client, term string) *PartSearch {
	res := &PartSearch{}
	themed, terr := d.SearchCatalogThemeParts(term)
	hits, err := d.SearchCatalogParts(term, searchLimit)
	if err = errors.Join(terr, err); err != nil {
		res.Notes = append(res.Notes, "Offline catalog search failed: "+err.Error()+".")
	}
	if seen := make(map[string]bool, len(themed)); len(themed)+len(hits) > 0 {
		for _, h := range themed {
			seen[h.Num] = true
		}
		for _, h := range hits {
			if !seen[h.Num] {
				themed = append(themed, h)
			}
		}
		res.Hits, res.Source = themed, d.CatalogLabel()
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
