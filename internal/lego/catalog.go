package lego

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui/img"
)

// The offline catalog is a local copy of Rebrickable's free bulk CSV
// downloads: parts, categories, colours, elements (which part exists in which
// colour), sets, themes, minifigures, every set's parts list, and which parts
// are alternates or moulds of each other. It needs no API key, has no rate
// limit, and answers lookups and searches instantly. Rebrickable asks that automated downloads
// happen at most once a day and that the data is credited.

const (
	catalogBaseURL = "https://cdn.rebrickable.com/media/downloads/"
	// CatalogAttribution must be shown wherever catalog data is presented.
	CatalogAttribution = "Data: Rebrickable (rebrickable.com)"
	catalogMinAge      = 24 * time.Hour
)

// Color is one LEGO colour. RGB is a hex string without '#', or "" if unknown.
type Color struct {
	ID    int
	Name  string
	RGB   string
	Trans bool
}

// CatalogPart is a part as the offline catalog knows it.
type CatalogPart struct {
	Num      string
	Name     string
	Category string
}

// CatalogOptions tunes a refresh; the zero value downloads from Rebrickable's CDN.
type CatalogOptions struct {
	BaseURL string
	HTTP    *http.Client
	Force   bool // ignore the once-a-day limit
	// Dir, when set, loads <name>.csv.gz files from this folder instead of downloading
	// (a manual download, or a copy on a USB stick when the network is down). Files that
	// are not there are skipped; at least one must be.
	Dir string
	// Progress, when set, is told what the refresh is doing ("downloading sets").
	Progress func(step string)
}

// CatalogResult says what a refresh did.
type CatalogResult struct {
	Skipped bool           // refreshed less than a day ago (and not forced)
	Updated int            // files that changed and were reloaded
	Rows    map[string]int // rows loaded per file
	Message string         // one line for the user
}

func (d *DB) metaGet(key string) string {
	var v string
	_ = d.QueryRow("SELECT value FROM cat_meta WHERE key = ?", key).Scan(&v)
	return v
}

// CatalogStatus reports what the offline catalog holds and how old it is.
func (d *DB) CatalogStatus() (parts, colors int, refreshed time.Time, err error) {
	if err = d.QueryRow("SELECT COUNT(*) FROM cat_parts").Scan(&parts); err != nil {
		return
	}
	if err = d.QueryRow("SELECT COUNT(*) FROM cat_colors").Scan(&colors); err != nil {
		return
	}
	if ms, perr := strconv.ParseInt(d.metaGet("refreshed_ms"), 10, 64); perr == nil {
		refreshed = time.UnixMilli(ms)
	}
	return
}

// CatalogPart looks one part up in the offline catalog; nil if it isn't there
// (or the catalog was never downloaded).
func (d *DB) CatalogPart(partNum string) (*CatalogPart, error) {
	var p CatalogPart
	err := d.QueryRow(`SELECT p.part_num, p.name, COALESCE(c.name, '') FROM cat_parts p
		LEFT JOIN cat_categories c ON c.id = p.part_cat_id
		WHERE p.part_num = ? COLLATE NOCASE LIMIT 1`, strings.TrimSpace(partNum)).Scan(&p.Num, &p.Name, &p.Category)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("looking up catalog part %s: %w", partNum, err)
	}
	return &p, nil
}

// ElementImageURLs returns Rebrickable's photographed-element picture URLs
// for a part, requested colour first, then its other known colours:
// https://cdn.rebrickable.com/media/parts/elements/<element_id>.jpg, built
// from cat_element_ids (synced with the rest of the offline catalog, no live
// API call here). This is a different, much more complete picture source
// than img.PartURLs' LDraw CAD renders (internal/ui/img/fetch.go) — that
// path 404s for every colour of some very common parts (3023, "Plate 1 x 2",
// has no LDraw render at all) because not every part has an LDraw model,
// while cat_element_ids covers whatever Rebrickable has photographed.
// Callers should try this first and fall back to img.PartURLs.
func (d *DB) ElementImageURLs(partNum string, colorID int) ([]string, error) {
	rows, err := d.Query(`SELECT element_id FROM cat_element_ids WHERE part_num = ?1 COLLATE NOCASE
			ORDER BY (color_id != ?2), color_id LIMIT 6`, strings.TrimSpace(partNum), colorID)
	if err != nil {
		return nil, fmt.Errorf("reading element ids: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var elementID string
		if err := rows.Scan(&elementID); err != nil {
			return nil, err
		}
		out = append(out, "https://cdn.rebrickable.com/media/parts/elements/"+elementID+".jpg")
	}
	return out, rows.Err()
}

// PartImageURLs is the one place that decides what picture to show for a
// part: ElementImageURLs' photographed-element pictures first (the more
// complete source), then img.PartURLs' LDraw renders as a second-chance
// fallback, tried across every colour the part is known in — so a mobile
// client or the TUI's guided walk only needs to try URLs in order, never
// duplicate this colour-fallback decision themselves.
func (d *DB) PartImageURLs(partNum string, colorID int) []string {
	urls, _ := d.ElementImageURLs(partNum, colorID)
	colors, _ := d.CatalogColorsFor(partNum)
	colorIDs := make([]int, 0, len(colors))
	for _, c := range colors {
		colorIDs = append(colorIDs, c.ID)
	}
	return append(urls, img.PartURLs(partNum, colorID, colorIDs)...)
}

// CatalogColorsFor returns the colours a part is known to exist in: those the
// element list gives, plus any colour it appears in inside a set's parts list
// (which covers parts the element list misses).
func (d *DB) CatalogColorsFor(partNum string) ([]Color, error) {
	rows, err := d.Query(`SELECT c.id, c.name, c.rgb, c.is_trans FROM cat_colors c WHERE c.id IN (
			SELECT color_id FROM cat_elements WHERE part_num = ?1 COLLATE NOCASE
			UNION
			SELECT color_id FROM cat_inventory_parts WHERE part_num = ?1) ORDER BY c.name`, strings.TrimSpace(partNum))
	if err != nil {
		return nil, fmt.Errorf("reading catalog colours: %w", err)
	}
	defer rows.Close()
	var out []Color
	for rows.Next() {
		var c Color
		var trans int
		if err := rows.Scan(&c.ID, &c.Name, &c.RGB, &trans); err != nil {
			return nil, err
		}
		c.Trans = trans != 0
		out = append(out, c)
	}
	return out, rows.Err()
}

// CatalogColors returns the whole colour table (for swatches and free-text matching).
func (d *DB) CatalogColors() ([]Color, error) {
	rows, err := d.Query(`SELECT id, name, rgb, is_trans FROM cat_colors ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Color
	for rows.Next() {
		var c Color
		var trans int
		if err := rows.Scan(&c.ID, &c.Name, &c.RGB, &trans); err != nil {
			return nil, err
		}
		c.Trans = trans != 0
		out = append(out, c)
	}
	return out, rows.Err()
}

// CatalogPartNums lists catalog part numbers starting with prefix (shell
// completion of `wms lego add-part <TAB>`); limit caps the answer.
func (d *DB) CatalogPartNums(prefix string, limit int) ([]string, error) {
	esc := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(strings.TrimSpace(prefix))
	rows, err := d.Query(`SELECT part_num FROM cat_parts WHERE part_num LIKE ? ESCAPE '\' ORDER BY part_num LIMIT ?`, esc+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// SetsInCatalog is how many sets the offline catalog holds.
func (d *DB) SetsInCatalog() int { return d.count("cat_sets") }
