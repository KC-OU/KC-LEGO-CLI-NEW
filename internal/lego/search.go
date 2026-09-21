package lego

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// Offline search over the catalog: full-text (SQLite FTS5) with prefix and
// multi-word matching, so "brick 2 x 4", "bri 2x4" and "3001" all work with no
// API and no internet.

var (
	dimensionRE      = regexp.MustCompile(`^(?i)\d+(x\d+)+$`)
	dimensionSplitRE = regexp.MustCompile(`(?i)\d+|x`)
)

// ftsQuery turns what a person typed into a safe FTS5 query: every run of
// letters and digits becomes a quoted prefix term, ANDed together. Quotes and
// FTS operators in the input can therefore never change the query's meaning.
// It returns "" when nothing searchable was typed.
func ftsQuery(term string) string {
	var words []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			w := string(cur)
			if dimensionRE.MatchString(w) { // "2x4" is how people write "2 x 4"
				for _, part := range dimensionSplitRE.FindAllString(w, -1) {
					words = append(words, `"`+part+`"*`)
				}
			} else {
				words = append(words, `"`+w+`"*`)
			}
			cur = cur[:0]
		}
	}
	for _, r := range term {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			cur = append(cur, r)
		} else {
			flush()
		}
		if len(words) >= 8 { // a paragraph pasted into the box is not a search
			break
		}
	}
	flush()
	return strings.Join(words, " ")
}

// likePrefix is a LIKE pattern for "starts with term", with wildcards in the term escaped.
func likePrefix(term string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(strings.TrimSpace(term)) + "%"
}

// PartHit is one part search result.
type PartHit struct {
	Num      string
	Name     string
	Category string
}

// SearchCatalogParts finds parts by name or number, exact number first.
func (d *DB) SearchCatalogParts(term string, limit int) ([]PartHit, error) {
	q := ftsQuery(term)
	if q == "" {
		return nil, nil
	}
	rows, err := d.Query(`
		SELECT p.part_num, p.name, COALESCE(c.name, '')
		FROM fts_parts f JOIN cat_parts p ON p.rowid = f.rowid LEFT JOIN cat_categories c ON c.id = p.part_cat_id
		WHERE fts_parts MATCH ?
		ORDER BY (p.part_num = ? COLLATE NOCASE) DESC,   -- the number typed exactly
		         (p.name = ? COLLATE NOCASE) DESC,        -- then the name typed exactly
		         (p.name LIKE ? ESCAPE '\') DESC,          -- then names that start with it
		         bm25(fts_parts), p.part_num LIMIT ?`, q, strings.TrimSpace(term), strings.TrimSpace(term), likePrefix(term), limit)
	if err != nil {
		return nil, fmt.Errorf("searching the catalog: %w", err)
	}
	defer rows.Close()
	var out []PartHit
	for rows.Next() {
		var h PartHit
		if err := rows.Scan(&h.Num, &h.Name, &h.Category); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// SetHit is one set as the offline catalog knows it.
type SetHit struct {
	Num    string
	Name   string
	Theme  string // full theme path, "Star Wars - Ultimate Collector Series"
	Year   int
	Pieces int
	ImgURL string
}

func (d *DB) setHits(query string, args ...any) ([]SetHit, error) {
	rows, err := d.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("searching sets: %w", err)
	}
	defer rows.Close()
	var out []SetHit
	var themeIDs []int
	for rows.Next() {
		var h SetHit
		var tid int
		if err := rows.Scan(&h.Num, &h.Name, &h.Year, &tid, &h.Pieces, &h.ImgURL); err != nil {
			return nil, err
		}
		out = append(out, h)
		themeIDs = append(themeIDs, tid)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	cache := map[int]string{}
	for i := range out {
		if _, ok := cache[themeIDs[i]]; !ok {
			cache[themeIDs[i]] = d.ThemePath(themeIDs[i])
		}
		out[i].Theme = cache[themeIDs[i]]
	}
	return out, nil
}

const setCols = `s.set_num, s.name, s.year, s.theme_id, s.num_parts, s.img_url`

// SearchCatalogSets finds sets by name or number, exact number first, newest first among equals.
func (d *DB) SearchCatalogSets(term string, limit int) ([]SetHit, error) {
	q := ftsQuery(term)
	if q == "" {
		return nil, nil
	}
	num := rebrickableSetNum(term)
	return d.setHits(`
		SELECT `+setCols+`
		FROM fts_sets f JOIN cat_sets s ON s.rowid = f.rowid
		WHERE fts_sets MATCH ?
		ORDER BY (s.set_num = ? COLLATE NOCASE) DESC,
		         (s.name = ? COLLATE NOCASE) DESC,
		         (s.name LIKE ? ESCAPE '\') DESC,
		         bm25(fts_sets), s.year DESC, s.set_num LIMIT ?`, q, num, strings.TrimSpace(term), likePrefix(term), limit)
}

// CatalogSet returns one set by number ("75192" and "75192-1" both work), or nil.
func (d *DB) CatalogSet(num string) (*SetHit, error) {
	num = rebrickableSetNum(num)
	if num == "" {
		return nil, nil
	}
	hits, err := d.setHits(`SELECT `+setCols+` FROM cat_sets s WHERE s.set_num = ? COLLATE NOCASE LIMIT 1`, num)
	if err != nil || len(hits) == 0 {
		return nil, err
	}
	return &hits[0], nil
}

// ThemePath is a theme's name with its parents ("Star Wars - Ultimate Collector
// Series"), at most four levels so a malformed cycle cannot loop. "" if unknown.
func (d *DB) ThemePath(id int) string {
	var names []string
	for depth := 0; id > 0 && depth < 4; depth++ {
		var name string
		var parent int
		if err := d.QueryRow(`SELECT name, parent_id FROM cat_themes WHERE id = ?`, id).Scan(&name, &parent); err != nil {
			break
		}
		names = append([]string{name}, names...)
		id = parent
	}
	return strings.Join(names, " - ")
}

// MinifigHit is one minifigure result.
type MinifigHit struct {
	Num    string
	Name   string
	Pieces int
	ImgURL string
}

// SearchCatalogMinifigs finds minifigures by name or number.
func (d *DB) SearchCatalogMinifigs(term string, limit int) ([]MinifigHit, error) {
	q := ftsQuery(term)
	if q == "" {
		return nil, nil
	}
	rows, err := d.Query(`
		SELECT m.fig_num, m.name, m.num_parts, m.img_url
		FROM fts_minifigs f JOIN cat_minifigs m ON m.rowid = f.rowid
		WHERE fts_minifigs MATCH ? ORDER BY bm25(fts_minifigs), m.fig_num LIMIT ?`, q, limit)
	if err != nil {
		return nil, fmt.Errorf("searching minifigures: %w", err)
	}
	defer rows.Close()
	var out []MinifigHit
	for rows.Next() {
		var h MinifigHit
		if err := rows.Scan(&h.Num, &h.Name, &h.Pieces, &h.ImgURL); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// ElementPart resolves a LEGO element ID (the 6-8 digit number on a bag or
// brick) to its part number and Rebrickable colour id.
func (d *DB) ElementPart(elementID string) (part string, colorID int, ok bool) {
	elementID = strings.TrimSpace(elementID)
	if err := d.QueryRow(`SELECT part_num, color_id FROM cat_element_ids WHERE element_id = ?`, elementID).Scan(&part, &colorID); err != nil {
		return "", 0, false
	}
	return part, colorID, true
}

// Source is where an answer came from, for showing beside it.
type Source struct {
	Name string // "offline catalog", "Rebrickable (live)", "BrickLink", "legacy import", "your collection"
	Note string // a problem worth showing ("Rebrickable lookup failed: ...")
}

// SetLookup is everything known about a set number.
type SetLookup struct {
	Set    SetHit
	Source string // "" when no set was found
	Notes  []string
}

// LookupSet resolves a set number through the chain: offline catalog first (no
// key, no limit), then live Rebrickable when the catalog lacks it, then the old
// imported reference table. Nothing knowing the set is not an error, and it never
// fails: problems become Notes. The result is never nil; Found says whether a set was located.
func (d *DB) LookupSet(ctx context.Context, rb *Client, num string) *SetLookup {
	num = strings.TrimSpace(num)
	if num == "" {
		return &SetLookup{}
	}
	if hit, err := d.CatalogSet(num); err == nil && hit != nil {
		return &SetLookup{Set: *hit, Source: "offline catalog"}
	}
	res := &SetLookup{}
	if rb != nil && rb.Enabled() {
		api, err := rb.GetSet(ctx, num)
		switch {
		case err == nil:
			res.Set = SetHit{Num: api.SetNum, Name: api.Name, Year: api.Year, Pieces: api.Pieces}
			res.Source = "Rebrickable (live)"
			if theme, terr := rb.ThemePath(ctx, api.ThemeID); terr == nil {
				res.Set.Theme = theme
			} else {
				res.Notes = append(res.Notes, "Theme lookup failed ("+terr.Error()+") — type the theme yourself.")
			}
			return res
		case errors.Is(err, ErrNotFound):
			res.Notes = append(res.Notes, fmt.Sprintf("Rebrickable has no set %q.", num))
		default:
			res.Notes = append(res.Notes, "Rebrickable lookup failed: "+err.Error()+".")
		}
	}
	if ref, err := d.GetRefSet(num); err == nil && ref != nil {
		res.Set = SetHit{Num: ref.SetNum, Name: ref.Name, Theme: ref.Theme, Year: atoi(ref.Year), Pieces: atoi(ref.TotalPieces)}
		res.Source = "legacy import"
		return res
	}
	if len(res.Notes) == 0 && (rb == nil || !rb.Enabled()) {
		if parts, _, _, _ := d.CatalogStatus(); parts == 0 {
			res.Notes = append(res.Notes, errNoCatalog.Error()+".")
		}
	}
	return res
}

// Found says whether the lookup located a set (Notes may still hold problems worth showing).
func (l *SetLookup) Found() bool { return l.Source != "" }
