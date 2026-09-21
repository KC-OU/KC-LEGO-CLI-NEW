package lego

import (
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// maxCatalogDownload bounds one compressed file; the biggest (inventory_parts) is about 14 MB.
const maxCatalogDownload = 96 << 20

// stmtSpec is one INSERT run for every row of a file; args returns nil to skip a row.
type stmtSpec struct {
	sql  string
	args func(v []string) []any
}

// catalogSpec describes how one Rebrickable CSV becomes rows. Columns are found
// by header NAME, not position, and a missing required column is an error, so a
// changed file format fails loudly instead of loading garbage.
type catalogSpec struct {
	name     string   // file base name on the CDN
	deletes  []string // tables emptied before loading
	required []string // columns that must be in the header
	stmts    []stmtSpec
	fts      string // full-text index to rebuild afterwards, if any
}

func atoi(s string) int { n, _ := strconv.Atoi(strings.TrimSpace(s)); return n }

func isTrue(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "t", "true", "1", "yes":
		return true
	}
	return false
}

// catalogSpecs lists the files in load order. Each spec's args func receives the
// values of `required` columns (plus the optional ones named after them) in order.
var catalogSpecs = []catalogSpec{
	{name: "colors", deletes: []string{"cat_colors"}, required: []string{"id", "name", "rgb", "is_trans"},
		stmts: []stmtSpec{{"INSERT OR REPLACE INTO cat_colors (id, name, rgb, is_trans) VALUES (?,?,?,?)",
			func(v []string) []any { return []any{atoi(v[0]), v[1], v[2], b2i(isTrue(v[3]))} }}}},
	{name: "part_categories", deletes: []string{"cat_categories"}, required: []string{"id", "name"},
		stmts: []stmtSpec{{"INSERT OR REPLACE INTO cat_categories (id, name) VALUES (?,?)",
			func(v []string) []any { return []any{atoi(v[0]), v[1]} }}}},
	{name: "parts", deletes: []string{"cat_parts"}, required: []string{"part_num", "name", "part_cat_id"}, fts: "fts_parts",
		stmts: []stmtSpec{{"INSERT OR REPLACE INTO cat_parts (part_num, name, part_cat_id) VALUES (?,?,?)",
			func(v []string) []any { return []any{v[0], v[1], atoi(v[2])} }}}},
	{name: "elements", deletes: []string{"cat_elements", "cat_element_ids"}, required: []string{"element_id", "part_num", "color_id"},
		stmts: []stmtSpec{
			{"INSERT OR IGNORE INTO cat_elements (part_num, color_id) VALUES (?,?)",
				func(v []string) []any { return []any{v[1], atoi(v[2])} }},
			{"INSERT OR REPLACE INTO cat_element_ids (element_id, part_num, color_id) VALUES (?,?,?)",
				func(v []string) []any { return []any{v[0], v[1], atoi(v[2])} }}}},
	{name: "themes", deletes: []string{"cat_themes"}, required: []string{"id", "name", "parent_id"},
		stmts: []stmtSpec{{"INSERT OR REPLACE INTO cat_themes (id, name, parent_id) VALUES (?,?,?)",
			func(v []string) []any { return []any{atoi(v[0]), v[1], atoi(v[2])} }}}},
	{name: "sets", deletes: []string{"cat_sets"}, required: []string{"set_num", "name", "year", "theme_id", "num_parts", "img_url"}, fts: "fts_sets",
		stmts: []stmtSpec{{"INSERT OR REPLACE INTO cat_sets (set_num, name, year, theme_id, num_parts, img_url) VALUES (?,?,?,?,?,?)",
			func(v []string) []any { return []any{v[0], v[1], atoi(v[2]), atoi(v[3]), atoi(v[4]), v[5]} }}}},
	{name: "minifigs", deletes: []string{"cat_minifigs"}, required: []string{"fig_num", "name", "num_parts", "img_url"}, fts: "fts_minifigs",
		stmts: []stmtSpec{{"INSERT OR REPLACE INTO cat_minifigs (fig_num, name, num_parts, img_url) VALUES (?,?,?,?)",
			func(v []string) []any { return []any{v[0], v[1], atoi(v[2]), v[3]} }}}},
	{name: "inventories", deletes: []string{"cat_inventories"}, required: []string{"id", "version", "set_num"},
		stmts: []stmtSpec{{"INSERT OR REPLACE INTO cat_inventories (id, version, set_num) VALUES (?,?,?)",
			func(v []string) []any { return []any{atoi(v[0]), atoi(v[1]), v[2]} }}}},
	// Spare parts are left out: nothing here needs them and they are about 5% of the rows.
	{name: "inventory_parts", deletes: []string{"cat_inventory_parts"}, required: []string{"inventory_id", "part_num", "color_id", "quantity", "is_spare"},
		stmts: []stmtSpec{{"INSERT INTO cat_inventory_parts (inventory_id, part_num, color_id, quantity) VALUES (?,?,?,?)",
			func(v []string) []any {
				if isTrue(v[4]) {
					return nil
				}
				return []any{atoi(v[0]), v[1], atoi(v[2]), atoi(v[3])}
			}}}},
	{name: "inventory_sets", deletes: []string{"cat_inventory_sets"}, required: []string{"inventory_id", "set_num", "quantity"},
		stmts: []stmtSpec{{"INSERT INTO cat_inventory_sets (inventory_id, set_num, quantity) VALUES (?,?,?)",
			func(v []string) []any { return []any{atoi(v[0]), v[1], atoi(v[2])} }}}},
	{name: "inventory_minifigs", deletes: []string{"cat_inventory_minifigs"}, required: []string{"inventory_id", "fig_num", "quantity"},
		stmts: []stmtSpec{{"INSERT INTO cat_inventory_minifigs (inventory_id, fig_num, quantity) VALUES (?,?,?)",
			func(v []string) []any { return []any{atoi(v[0]), v[1], atoi(v[2])} }}}},
	{name: "part_relationships", deletes: []string{"cat_part_relations"}, required: []string{"rel_type", "child_part_num", "parent_part_num"},
		stmts: []stmtSpec{{"INSERT INTO cat_part_relations (rel_type, child_part, parent_part) VALUES (?,?,?)",
			func(v []string) []any { return []any{strings.ToUpper(strings.TrimSpace(v[0])), v[1], v[2]} }}}},
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// CatalogFileNames lists the CSV files the offline catalog is built from.
func CatalogFileNames() []string {
	names := make([]string, len(catalogSpecs))
	for i, s := range catalogSpecs {
		names[i] = s.name
	}
	return names
}

// downloadCatalogFile fetches one compressed CSV into dir. notModified is true
// (and no file written) when the server says nothing changed since ifModifiedSince.
func downloadCatalogFile(ctx context.Context, opt CatalogOptions, name, ifModifiedSince, dir string) (path, lastModified string, notModified bool, err error) {
	base := opt.BaseURL
	if base == "" {
		base = catalogBaseURL
	}
	hc := opt.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 3 * time.Minute}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+name+".csv.gz", nil)
	if err != nil {
		return "", "", false, err
	}
	req.Header.Set("User-Agent", "wms-go/1.0 (+personal LEGO inventory; daily catalog refresh)")
	if ifModifiedSince != "" {
		req.Header.Set("If-Modified-Since", ifModifiedSince)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return "", "", false, fmt.Errorf("downloading %s: %w", name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		return "", ifModifiedSince, true, nil
	}
	if resp.StatusCode != http.StatusOK {
		return "", "", false, fmt.Errorf("downloading %s: HTTP %d", name, resp.StatusCode)
	}
	path = filepath.Join(dir, name+".csv.gz")
	f, err := os.Create(path)
	if err != nil {
		return "", "", false, err
	}
	n, err := io.Copy(f, io.LimitReader(resp.Body, maxCatalogDownload+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", "", false, fmt.Errorf("downloading %s: %w", name, err)
	}
	if n > maxCatalogDownload {
		return "", "", false, fmt.Errorf("%s is larger than %d MB — refusing it", name, maxCatalogDownload>>20)
	}
	return path, resp.Header.Get("Last-Modified"), false, nil
}

// loadCatalogFile streams one downloaded CSV into its tables inside tx and
// returns how many rows it read. It never holds the file in memory.
func loadCatalogFile(tx *sql.Tx, spec catalogSpec, path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return 0, fmt.Errorf("%s is not gzip data: %w", spec.name, err)
	}
	defer zr.Close()
	body := &capReader{r: zr, left: maxCatalogFileBytes}
	cr := csv.NewReader(body)
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true
	cr.ReuseRecord = true
	header, err := cr.Read()
	if err != nil {
		return 0, fmt.Errorf("%s: reading the header line: %w", spec.name, err)
	}
	col := map[string]int{}
	for i, h := range header {
		col[strings.ToLower(strings.TrimSpace(strings.TrimPrefix(h, "\ufeff")))] = i
	}
	idx := make([]int, len(spec.required))
	for i, want := range spec.required {
		j, ok := col[want]
		if !ok {
			return 0, fmt.Errorf("%s: the file format changed — column %q is missing (columns are: %s)", spec.name, want, strings.Join(header, ", "))
		}
		idx[i] = j
	}

	var prev int
	for _, t := range spec.deletes[:1] {
		_ = tx.QueryRow("SELECT COUNT(*) FROM " + t).Scan(&prev)
	}
	for _, t := range spec.deletes {
		if _, err := tx.Exec("DELETE FROM " + t); err != nil {
			return 0, err
		}
	}
	stmts := make([]*sql.Stmt, len(spec.stmts))
	for i, s := range spec.stmts {
		if stmts[i], err = tx.Prepare(s.sql); err != nil {
			return 0, err
		}
		defer stmts[i].Close()
	}
	vals := make([]string, len(idx))
	rows := 0
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, fmt.Errorf("%s: reading row %d: %w", spec.name, rows+2, err)
		}
		short := false
		for i, j := range idx {
			if j >= len(rec) {
				short = true
				break
			}
			vals[i] = rec[j]
		}
		if short {
			continue
		}
		rows++
		for i, s := range spec.stmts {
			if args := s.args(vals); args != nil {
				if _, err := stmts[i].Exec(args...); err != nil {
					return 0, fmt.Errorf("loading %s: %w", spec.name, err)
				}
			}
		}
	}
	// The gzip trailer is checked when the stream is read to its end: a truncated download fails here.
	if _, err := io.Copy(io.Discard, body); err != nil {
		return 0, fmt.Errorf("%s: the download is damaged: %w", spec.name, err)
	}
	if rows == 0 {
		return 0, fmt.Errorf("%s has no data rows", spec.name)
	}
	if prev >= 100 && rows < prev/2 {
		return 0, fmt.Errorf("%s shrank from %d to %d rows — refusing to replace the catalog with something that looks broken", spec.name, prev, rows)
	}
	return rows, nil
}

// RefreshCatalog downloads whichever catalog files changed and reloads them in a
// single transaction, so a failed or partial download leaves the old catalog
// intact. Files go to disk first and are streamed into the database, so memory
// stays small even for the 1.5 million-row parts lists.
func (d *DB) RefreshCatalog(ctx context.Context, opt CatalogOptions) (*CatalogResult, error) {
	if !opt.Force && opt.Dir == "" { // the once-a-day rule is about downloads; local files can be loaded any time
		if ms, err := strconv.ParseInt(d.metaGet("refreshed_ms"), 10, 64); err == nil && time.Since(time.UnixMilli(ms)) < catalogMinAge {
			return &CatalogResult{Skipped: true, Message: "Catalog was refreshed less than a day ago — Rebrickable asks for at most one automated download a day (use --force to override)."}, nil
		}
	}
	dir, err := os.MkdirTemp("", "wms-catalog-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	type fetched struct {
		spec     catalogSpec
		path, lm string
	}
	var todo []fetched
	found := 0
	for _, spec := range catalogSpecs {
		if opt.Dir != "" {
			path := filepath.Join(opt.Dir, spec.name+".csv.gz")
			fi, err := os.Stat(path)
			if err != nil {
				continue
			}
			lm := "file:" + fi.ModTime().UTC().Format(time.RFC3339)
			if lm != d.metaGet("lm:"+spec.name) || opt.Force {
				todo = append(todo, fetched{spec, path, lm})
			}
			found++
			continue
		}
		if opt.Progress != nil {
			opt.Progress("downloading " + spec.name)
		}
		path, lm, same, err := downloadCatalogFile(ctx, opt, spec.name, d.metaGet("lm:"+spec.name), dir)
		if err != nil {
			return nil, err
		}
		if !same {
			todo = append(todo, fetched{spec, path, lm})
		}
	}
	if opt.Dir != "" && found == 0 {
		return nil, fmt.Errorf("no catalog files (<name>.csv.gz, such as sets.csv.gz) found in %s", opt.Dir)
	}

	tx, err := d.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	loaded := map[string]int{}
	for _, f := range todo {
		if opt.Progress != nil {
			opt.Progress("loading " + f.spec.name)
		}
		n, err := loadCatalogFile(tx, f.spec, f.path)
		if err != nil {
			return nil, err
		}
		loaded[f.spec.name] = n
		if f.spec.fts != "" {
			if _, err := tx.Exec("INSERT INTO " + f.spec.fts + "(" + f.spec.fts + ") VALUES('rebuild')"); err != nil {
				return nil, fmt.Errorf("indexing %s: %w", f.spec.name, err)
			}
		}
		for k, v := range map[string]string{"lm:" + f.spec.name: f.lm, "rows:" + f.spec.name: strconv.Itoa(n)} {
			if _, err := tx.Exec(`INSERT INTO cat_meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, k, v); err != nil {
				return nil, err
			}
		}
	}
	if _, err := tx.Exec(`INSERT INTO cat_meta (key, value) VALUES ('refreshed_ms', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, strconv.FormatInt(time.Now().UnixMilli(), 10)); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	parts, colors, _, _ := d.CatalogStatus()
	msg := fmt.Sprintf("Catalog up to date: %d parts, %d colours, %d sets. %s", parts, colors, d.count("cat_sets"), CatalogAttribution)
	if len(todo) == 0 {
		msg = "Catalog already current (nothing changed upstream). " + CatalogAttribution
	}
	return &CatalogResult{Updated: len(todo), Rows: loaded, Message: msg}, nil
}

func (d *DB) count(table string) int {
	var n int
	_ = d.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n)
	return n
}

// CatalogTable is one part of the offline catalog and what it holds.
type CatalogTable struct {
	File   string
	Rows   int
	Loaded time.Time // zero when never loaded
}

// CatalogTables reports, per source file, how many rows are loaded.
func (d *DB) CatalogTables() []CatalogTable {
	tables := map[string]string{"colors": "cat_colors", "part_categories": "cat_categories", "parts": "cat_parts", "elements": "cat_element_ids",
		"themes": "cat_themes", "sets": "cat_sets", "minifigs": "cat_minifigs", "inventories": "cat_inventories", "inventory_parts": "cat_inventory_parts",
		"inventory_sets": "cat_inventory_sets", "inventory_minifigs": "cat_inventory_minifigs", "part_relationships": "cat_part_relations"}
	var out []CatalogTable
	for _, spec := range catalogSpecs {
		t := CatalogTable{File: spec.name, Rows: d.count(tables[spec.name])}
		if ms, err := strconv.ParseInt(d.metaGet("refreshed_ms"), 10, 64); err == nil && t.Rows > 0 {
			t.Loaded = time.UnixMilli(ms)
		}
		out = append(out, t)
	}
	return out
}

var errNoCatalog = errors.New("the offline catalog is empty — run: wms lego catalog refresh")

// maxCatalogFileBytes bounds one unpacked CSV (the largest real one is under 1 GB) so a hostile or
// broken download cannot fill the disk or memory.
const maxCatalogFileBytes = 4 << 30

type capReader struct {
	r    io.Reader
	left int64
}

func (c *capReader) Read(p []byte) (int, error) {
	if c.left <= 0 {
		return 0, errors.New("the file unpacks to an unreasonable size")
	}
	if int64(len(p)) > c.left {
		p = p[:c.left]
	}
	n, err := c.r.Read(p)
	c.left -= int64(n)
	return n, err
}
