package lego

import (
	"context"
	"database/sql"
	"encoding/xml"
	"errors"
	"fmt"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Collection insights: what is running low, how much you have, what a set
// still needs, and the BrickLink wanted-list file for the difference.

const invTTL = 30 * 24 * time.Hour

// ---- low stock ----

// SetMinQty sets how few of a part+colour you want to hold before it counts as
// low (0 stops tracking it).
func (d *DB) SetMinQty(partNum string, colorID int, colorName string, min int) error {
	if min < 0 {
		return errors.New("the minimum cannot be negative")
	}
	d.ensureDailySnapshot()
	oldMin, qty := 0, 0
	if cur, err := d.GetOwnedPart(partNum, colorID, colorName); err == nil && cur != nil {
		oldMin, qty = cur.MinQty, cur.Qty
	}
	res, err := d.Exec(`UPDATE owned_parts SET min_qty = ?, updated_at = ? WHERE `+ownedMatch,
		min, time.Now().UTC().Format(timeLayout), partNum, colorID, colorName)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("you do not hold part %s in that colour", partNum)
	}
	if oldMin != min {
		d.journal(d.DB, "min", "part", partNum, colorID, colorName, qty, qty, fmt.Sprintf("minimum %d -> %d", oldMin, min))
	}
	return nil
}

// LowStock lists the parts you asked to keep a minimum of that have fallen
// below it, worst shortfall first.
func (d *DB) LowStock() ([]OwnedPart, error) {
	return d.queryOwned(`SELECT ` + ownedCols + ` FROM owned_parts WHERE min_qty > 0 AND qty < min_qty ORDER BY (min_qty - qty) DESC, part_num`)
}

// IsLow is true when a minimum is set and the quantity is below it.
func (p OwnedPart) IsLow() bool { return p.MinQty > 0 && p.Qty < p.MinQty }

// ---- stats ----

type Count struct {
	Name string
	N    int
}

// Stats is the collection at a glance.
type Stats struct {
	SetTitles     int // distinct sets
	SetCopies     int // sum of copies owned
	SetPieces     int // pieces in owned sets that are not parted out
	PartedOut     int
	PartLines     int // part + colour rows
	DistinctParts int
	LoosePieces   int // sum of owned part quantities
	LowStock      int
	Themes        []Count // sets by theme, most first
	Colours       []Count // loose pieces by colour, most first
	Categories    []Count // loose pieces by category, most first
}

func (d *DB) countBy(q string, limit int) ([]Count, error) {
	rows, err := d.Query(q+` LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Count
	for rows.Next() {
		var c Count
		if err := rows.Scan(&c.Name, &c.N); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (d *DB) Stats() (*Stats, error) {
	var s Stats
	if err := d.QueryRow(`SELECT COUNT(*), COALESCE(SUM(qty),0), COALESCE(SUM(CASE WHEN parted_out = 0 THEN qty * parts_qty ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN parted_out != 0 THEN 1 ELSE 0 END),0) FROM sets`).Scan(&s.SetTitles, &s.SetCopies, &s.SetPieces, &s.PartedOut); err != nil {
		return nil, fmt.Errorf("counting sets: %w", err)
	}
	if err := d.QueryRow(`SELECT COUNT(*), COUNT(DISTINCT part_num), COALESCE(SUM(qty),0),
		COALESCE(SUM(CASE WHEN min_qty > 0 AND qty < min_qty THEN 1 ELSE 0 END),0) FROM owned_parts`).Scan(&s.PartLines, &s.DistinctParts, &s.LoosePieces, &s.LowStock); err != nil {
		return nil, fmt.Errorf("counting parts: %w", err)
	}
	var err error
	if s.Themes, err = d.countBy(`SELECT CASE WHEN theme = '' THEN '(no theme)' ELSE theme END, SUM(qty) FROM sets GROUP BY 1 ORDER BY 2 DESC, 1`, 8); err != nil {
		return nil, err
	}
	if s.Colours, err = d.countBy(`SELECT CASE WHEN color_name = '' THEN '(no colour)' ELSE color_name END, SUM(qty) FROM owned_parts GROUP BY color_id, color_name ORDER BY 2 DESC, 1`, 8); err != nil {
		return nil, err
	}
	if s.Categories, err = d.countBy(`SELECT CASE WHEN category = '' THEN '(none)' ELSE category END, SUM(qty) FROM owned_parts GROUP BY 1 ORDER BY 2 DESC, 1`, 8); err != nil {
		return nil, err
	}
	return &s, nil
}

// ---- set inventory and what is missing ----

// InvItem is one line of a set's parts list.
type InvItem struct {
	PartNum     string
	PartName    string
	ColorID     int
	ColorName   string
	RGB         string
	Qty         int
	BrickLinkID string // the part's BrickLink number, "" when Rebrickable lists none
	BLColor     int    // the colour's BrickLink number, 0 when unknown
}

type invRow struct {
	Part struct {
		PartNum     string         `json:"part_num"`
		Name        string         `json:"name"`
		ExternalIDs map[string]any `json:"external_ids"`
	} `json:"part"`
	Color struct {
		ID          int                       `json:"id"`
		Name        string                    `json:"name"`
		RGB         string                    `json:"rgb"`
		ExternalIDs map[string]map[string]any `json:"external_ids"`
	} `json:"color"`
	Quantity int  `json:"quantity"`
	IsSpare  bool `json:"is_spare"`
}

func firstString(v any) string {
	if list, _ := v.([]any); len(list) > 0 {
		if s, ok := list[0].(string); ok {
			return s
		}
	}
	return ""
}

func firstInt(v any) int {
	if list, _ := v.([]any); len(list) > 0 {
		if f, ok := list[0].(float64); ok {
			return int(f)
		}
	}
	return 0
}

// BrickLinkID is the BrickLink number of a colour, 0 when Rebrickable lists none.
func (c ColorInfo) BrickLinkID() int { return firstInt(c.ExternalIDs["BrickLink"]["ext_ids"]) }

// GetSetInventory lists a set's parts (spares left out), following Rebrickable's
// pages. Each page is cached for a month, so a set is fetched once.
func (c *Client) GetSetInventory(ctx context.Context, setNum string) ([]InvItem, error) {
	num := rebrickableSetNum(setNum)
	if num == "" {
		return nil, ErrNotFound
	}
	path := "/sets/" + url.PathEscape(num) + "/parts/?page_size=1000&inc_part_details=1&inc_color_details=1"
	var items []InvItem
	for pages := 0; path != ""; pages++ {
		if pages >= 30 {
			return nil, errors.New("the parts list has more pages than expected")
		}
		var pg struct {
			Next    string   `json:"next"`
			Results []invRow `json:"results"`
		}
		if err := c.getCached(ctx, path, invTTL, &pg); err != nil {
			return nil, err
		}
		for _, r := range pg.Results {
			if r.IsSpare || r.Quantity <= 0 {
				continue
			}
			bl := firstString(r.Part.ExternalIDs["BrickLink"])
			items = append(items, InvItem{
				PartNum: r.Part.PartNum, PartName: r.Part.Name, ColorID: r.Color.ID, ColorName: r.Color.Name, RGB: r.Color.RGB,
				Qty: r.Quantity, BrickLinkID: bl, BLColor: firstInt(r.Color.ExternalIDs["BrickLink"]["ext_ids"]),
			})
		}
		path = ""
		if pg.Next != "" {
			if !strings.HasPrefix(pg.Next, c.BaseURL) {
				return nil, errors.New("Rebrickable sent a next-page link this client does not follow")
			}
			path = strings.TrimPrefix(pg.Next, c.BaseURL)
		}
	}
	return items, nil
}

// MissingLine is a part+colour a set needs more of than you hold.
type MissingLine struct {
	InvItem
	Need        int // for all the copies asked about
	Have        int // exact matches plus equivalents
	Short       int
	Substituted []string // "3 x 3001a": what equivalent parts covered, so you can check them
}

// MissingReport is the comparison of a set's parts with your loose parts.
type MissingReport struct {
	Copies       int
	Lines        int // part+colour lines the set has
	Complete     int // lines you fully hold
	PiecesNeeded int
	PiecesHeld   int // of those needed, how many you hold
	// SubstitutedPieces is how many of PiecesHeld are equivalent parts rather than the exact number.
	SubstitutedPieces int
	Missing           []MissingLine
}

// Percent is how much of the set's pieces you hold.
func (r *MissingReport) Percent() int {
	if r.PiecesNeeded == 0 {
		return 100
	}
	return r.PiecesHeld * 100 / r.PiecesNeeded
}

// Equivalents says which kinds of "same part, different number" count as held.
// Rebrickable's relation types: A alternate, M mould variant, P printed version,
// T pattern, R pair, B sub-part (only A, M and P are used here).
type Equivalents string

const (
	EquivNone      Equivalents = ""
	EquivDefault   Equivalents = "AM" // alternates and moulds: interchangeable in a build
	EquivWithPrint Equivalents = "AMP"
)

// ParseEquivalents reads "none", "default" or a list such as "alt,mold,print" (letters A M P also work).
func ParseEquivalents(s string) (Equivalents, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "", "default":
		return EquivDefault, nil
	case "none", "off", "no":
		return EquivNone, nil
	}
	var out []byte
	for _, w := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '+' }) {
		switch w {
		case "a", "alt", "alternate", "alternates":
			out = append(out, 'A')
		case "m", "mold", "mould", "molds", "moulds":
			out = append(out, 'M')
		case "p", "print", "prints", "printed":
			out = append(out, 'P')
		default:
			return "", fmt.Errorf("unknown kind %q: use alt, mold, print, none or default", w)
		}
	}
	return Equivalents(out), nil
}

// EquivalentsFromConfig is the WMS_EQUIVALENTS setting ("default", "none", "alt,mold,print");
// an unreadable value falls back to the default rather than break every report.
func EquivalentsFromConfig() Equivalents {
	e, err := ParseEquivalents(config.Get(config.Equivalents))
	if err != nil {
		return EquivDefault
	}
	return e
}

// Label describes the setting for display ("alternates + moulds").
func (e Equivalents) Label() string {
	var parts []string
	for _, c := range string(e) {
		parts = append(parts, map[rune]string{'A': "alternates", 'M': "moulds", 'P': "printed versions"}[c])
	}
	if len(parts) == 0 {
		return "exact part numbers only"
	}
	return strings.Join(parts, " + ")
}

// equivalentsMap returns, for each part number, the parts related to it by the chosen
// relation types (one hop, both directions, lower-cased).
func (d *DB) equivalentsMap(e Equivalents) (map[string][]string, error) {
	if e == "" {
		return nil, nil
	}
	rows, err := d.Query(`SELECT rel_type, child_part, parent_part FROM cat_part_relations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var typ, child, parent string
		if err := rows.Scan(&typ, &child, &parent); err != nil {
			return nil, err
		}
		if !strings.Contains(string(e), typ) {
			continue
		}
		c, p := strings.ToLower(child), strings.ToLower(parent)
		out[c] = append(out[c], p)
		out[p] = append(out[p], c)
	}
	return out, rows.Err()
}

// MissingFor compares copies of a set (its parts list) with the loose parts you
// hold, matching on part number and Rebrickable colour id, and counting alternates
// and moulds as the same part (see MissingForWith). Parts held in a free-text
// colour cannot be matched.
func (d *DB) MissingFor(items []InvItem, copies int) (*MissingReport, error) {
	return d.MissingForWith(items, copies, EquivalentsFromConfig())
}

// MissingForWith is MissingFor with a chosen set of equivalent-part kinds. Exact
// matches are used first; only the shortfall is then covered from equivalents, and
// each held piece is used once, so two lines can't both claim the same part.
func (d *DB) MissingForWith(items []InvItem, copies int, eq Equivalents) (*MissingReport, error) {
	if copies < 1 {
		copies = 1
	}
	key := func(part string, color int) string { return strings.ToLower(part) + "/" + fmt.Sprint(color) }
	avail := map[string]int{}
	owned, err := d.ListOwnedParts()
	if err != nil {
		return nil, err
	}
	for _, p := range owned {
		if p.ColorID >= 0 {
			avail[key(p.PartNum, p.ColorID)] += p.Qty
		}
	}
	rel, err := d.equivalentsMap(eq)
	if err != nil {
		return nil, err
	}

	r := &MissingReport{Copies: copies, Lines: len(items)}
	lines := make([]MissingLine, len(items))
	for i, it := range items { // pass 1: exact part and colour
		need := it.Qty * copies
		use := min(avail[key(it.PartNum, it.ColorID)], need)
		avail[key(it.PartNum, it.ColorID)] -= use
		lines[i] = MissingLine{InvItem: it, Need: need, Have: use}
	}
	for i := range lines { // pass 2: cover what is still short with equivalents
		l := &lines[i]
		for _, alt := range rel[strings.ToLower(l.PartNum)] {
			if l.Have >= l.Need {
				break
			}
			use := min(avail[key(alt, l.ColorID)], l.Need-l.Have)
			if use > 0 {
				avail[key(alt, l.ColorID)] -= use
				l.Have += use
				l.Substituted = append(l.Substituted, fmt.Sprintf("%d x %s", use, alt))
				r.SubstitutedPieces += use
			}
		}
	}
	for _, l := range lines {
		r.PiecesNeeded += l.Need
		r.PiecesHeld += min(l.Have, l.Need)
		if l.Have >= l.Need {
			r.Complete++
			continue
		}
		l.Short = l.Need - l.Have
		r.Missing = append(r.Missing, l)
	}
	sort.SliceStable(r.Missing, func(i, j int) bool {
		if r.Missing[i].Short != r.Missing[j].Short {
			return r.Missing[i].Short > r.Missing[j].Short
		}
		return r.Missing[i].PartNum < r.Missing[j].PartNum
	})
	return r, nil
}

// ---- BrickLink wanted list ----

// WantedItem is one line of a BrickLink wanted list.
type WantedItem struct {
	ItemID string // the BrickLink part number
	Color  int    // BrickLink colour id; 0 leaves the colour out
	MinQty int
}

type xmlItem struct {
	XMLName  xml.Name `xml:"ITEM"`
	ItemType string   `xml:"ITEMTYPE"`
	ItemID   string   `xml:"ITEMID"`
	Color    *int     `xml:"COLOR,omitempty"`
	MinQty   int      `xml:"MINQTY"`
}

type xmlInventory struct {
	XMLName xml.Name  `xml:"INVENTORY"`
	Items   []xmlItem `xml:"ITEM"`
}

// WantedXML renders the wanted list in BrickLink's upload format
// (Wanted > Upload Wanted List Items): parts only, one ITEM per line.
func WantedXML(items []WantedItem) ([]byte, error) {
	inv := xmlInventory{}
	for _, it := range items {
		if it.ItemID == "" || it.MinQty < 1 {
			continue
		}
		x := xmlItem{ItemType: "P", ItemID: it.ItemID, MinQty: it.MinQty}
		if it.Color > 0 {
			c := it.Color
			x.Color = &c
		}
		inv.Items = append(inv.Items, x)
	}
	b, err := xml.MarshalIndent(inv, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// WantedFromMissing turns a missing-parts report into a wanted list, and says
// how many lines had to fall back to Rebrickable's number (no BrickLink one
// known) or leave the colour out.
func WantedFromMissing(r *MissingReport) (items []WantedItem, noPartID, noColor int) {
	for _, m := range r.Missing {
		id := m.BrickLinkID
		if id == "" {
			id, noPartID = m.PartNum, noPartID+1
		}
		if m.BLColor == 0 {
			noColor++
		}
		items = append(items, WantedItem{ItemID: id, Color: m.BLColor, MinQty: m.Short})
	}
	return
}

// ---- recent parts ----

const recentKeep = 30

// AddRecent remembers that partNum was just used, for the part prompt's quick pick.
func (d *DB) AddRecent(partNum string) error {
	partNum = strings.TrimSpace(partNum)
	if partNum == "" {
		return nil
	}
	now := time.Now().UTC().Format(timeLayout)
	if _, err := d.Exec(`INSERT INTO recent_parts (part_num, used_at) VALUES (?, ?)
		ON CONFLICT(part_num) DO UPDATE SET used_at = excluded.used_at`, partNum, now); err != nil {
		return err
	}
	_, err := d.Exec(`DELETE FROM recent_parts WHERE part_num NOT IN (SELECT part_num FROM recent_parts ORDER BY used_at DESC, rowid DESC LIMIT ?)`, recentKeep)
	return err
}

// Recents returns up to limit part numbers, most recently used first.
func (d *DB) Recents(limit int) ([]string, error) {
	rows, err := d.Query(`SELECT part_num FROM recent_parts ORDER BY used_at DESC, rowid DESC LIMIT ?`, limit)
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

// BackupCollection writes a consistent copy of the database into dir as
// lego_db_<timestamp>.db (0600) and returns its path. The copy is taken with
// VACUUM INTO, which is safe while other sessions use the database, and then the
// catalog, cache and full-text tables — all re-downloadable — are dropped from the
// copy so the file holds only what is yours: sets, owned parts and their history.
func (d *DB) BackupCollection(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "lego_db_"+time.Now().Format("20060102_150405")+".db")
	for n := 1; ; n++ { // two backups in the same second: add a counter instead of failing
		if _, err := os.Stat(path); err != nil {
			break
		}
		path = filepath.Join(dir, fmt.Sprintf("lego_db_%s-%d.db", time.Now().Format("20060102_150405"), n))
	}
	if _, err := d.Exec(`VACUUM INTO ?`, path); err != nil {
		return "", fmt.Errorf("copying the database: %w", err)
	}
	ok := false
	defer func() {
		if !ok {
			os.Remove(path)
		}
	}()
	if err := os.Chmod(path, 0o600); err != nil {
		return "", err
	}
	cp, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		return "", err
	}
	defer cp.Close()
	rows, err := cp.Query(`SELECT name, type FROM sqlite_master WHERE (name LIKE 'cat\_%' ESCAPE '\' OR name LIKE 'fts\_%' ESCAPE '\' OR name IN ('rb_cache', 'rb_meta', 'img_cache')) AND type = 'table' AND name NOT LIKE '%\_data' ESCAPE '\' AND name NOT LIKE '%\_idx' ESCAPE '\' AND name NOT LIKE '%\_docsize' ESCAPE '\' AND name NOT LIKE '%\_config' ESCAPE '\' AND name NOT LIKE '%\_content' ESCAPE '\'`)
	if err != nil {
		return "", err
	}
	var drop []string
	for rows.Next() {
		var name, typ string
		if err := rows.Scan(&name, &typ); err != nil {
			rows.Close()
			return "", err
		}
		drop = append(drop, name)
	}
	rows.Close()
	for _, name := range drop {
		if _, err := cp.Exec(`DROP TABLE IF EXISTS "` + name + `"`); err != nil {
			return "", fmt.Errorf("trimming the backup (%s): %w", name, err)
		}
	}
	if _, err := cp.Exec(`VACUUM`); err != nil {
		return "", err
	}
	ok = true
	return path, nil
}

// CatalogSetInventory lists a set's parts from the offline catalog (its newest
// inventory version, spares excluded), so "missing parts" and set pages work with
// no API key and no network. BrickLink part numbers are not in the CSVs, so
// BrickLinkID is left empty (callers fall back to the Rebrickable number); the
// BrickLink colour comes from the colour map when one has been synced.
func (d *DB) CatalogSetInventory(setNum string) ([]InvItem, error) {
	num := rebrickableSetNum(setNum)
	if num == "" {
		return nil, nil
	}
	var inv int
	if err := d.QueryRow(`SELECT id FROM cat_inventories WHERE set_num = ? COLLATE NOCASE ORDER BY version DESC LIMIT 1`, num).Scan(&inv); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	rows, err := d.Query(`
		SELECT ip.part_num, COALESCE(p.name, ip.part_num), ip.color_id, COALESCE(c.name, ''), COALESCE(c.rgb, ''), SUM(ip.quantity), COALESCE(bl.bl_id, 0)
		FROM cat_inventory_parts ip
		LEFT JOIN cat_parts p ON p.part_num = ip.part_num
		LEFT JOIN cat_colors c ON c.id = ip.color_id
		LEFT JOIN bl_colors bl ON bl.rb_id = ip.color_id
		WHERE ip.inventory_id = ?
		GROUP BY ip.part_num, ip.color_id
		ORDER BY ip.part_num, ip.color_id`, inv)
	if err != nil {
		return nil, fmt.Errorf("reading the set's parts list: %w", err)
	}
	defer rows.Close()
	var out []InvItem
	for rows.Next() {
		var it InvItem
		if err := rows.Scan(&it.PartNum, &it.PartName, &it.ColorID, &it.ColorName, &it.RGB, &it.Qty, &it.BLColor); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// SetInventory is a set's parts list and where it came from.
type SetInventory struct {
	Items  []InvItem
	Source string
	Notes  []string
}

// LookupSetInventory gets a set's parts: the offline catalog first (instant, no
// key), then live Rebrickable if the catalog lacks the set. Items is empty when
// nothing knows the set; Notes say why.
func (d *DB) LookupSetInventory(ctx context.Context, rb *Client, setNum string) *SetInventory {
	res := &SetInventory{}
	if items, err := d.CatalogSetInventory(setNum); err != nil {
		res.Notes = append(res.Notes, "Offline parts list failed: "+err.Error()+".")
	} else if len(items) > 0 {
		res.Items, res.Source = items, d.CatalogLabel()
		return res
	}
	if rb != nil && rb.Enabled() {
		items, err := rb.GetSetInventory(ctx, setNum)
		switch {
		case err == nil && len(items) > 0:
			res.Items, res.Source = items, "live — Rebrickable"
			return res
		case errors.Is(err, ErrNotFound):
			res.Notes = append(res.Notes, fmt.Sprintf("Rebrickable has no set %q.", strings.TrimSpace(setNum)))
		case err != nil:
			res.Notes = append(res.Notes, "Rebrickable is unreachable ("+shorten(err.Error(), 80)+").")
		}
	}
	if len(res.Notes) == 0 {
		if d.catalogEmpty() {
			res.Notes = append(res.Notes, "There is no offline catalog yet and no Rebrickable key: run `wms lego catalog refresh`.")
		} else {
			res.Notes = append(res.Notes, fmt.Sprintf("The catalog has no parts list for set %q.", strings.TrimSpace(setNum)))
		}
	}
	res.Source = "no parts list"
	return res
}

// SetsUsingPart returns how many sets contain a part (in any colour) and a few
// examples, newest first, from the offline catalog.
func (d *DB) SetsUsingPart(partNum string, limit int) (total int, examples []SetHit, err error) {
	partNum = strings.TrimSpace(partNum)
	if err := d.QueryRow(`SELECT COUNT(DISTINCT i.set_num) FROM cat_inventory_parts ip JOIN cat_inventories i ON i.id = ip.inventory_id WHERE ip.part_num = ?`, partNum).Scan(&total); err != nil {
		return 0, nil, err
	}
	if total == 0 {
		return 0, nil, nil
	}
	examples, err = d.setHits(`SELECT `+setCols+` FROM cat_sets s WHERE s.set_num IN (
			SELECT DISTINCT i.set_num FROM cat_inventory_parts ip JOIN cat_inventories i ON i.id = ip.inventory_id WHERE ip.part_num = ?)
		ORDER BY s.year DESC, s.num_parts DESC LIMIT ?`, partNum, limit)
	return total, examples, err
}

// MinifigsInSet returns the names of the minifigures in a set (offline), up to limit.
func (d *DB) MinifigsInSet(setNum string, limit int) ([]string, error) {
	num := rebrickableSetNum(setNum)
	rows, err := d.Query(`
		SELECT m.name, im.quantity FROM cat_inventory_minifigs im
		JOIN cat_minifigs m ON m.fig_num = im.fig_num
		WHERE im.inventory_id = (SELECT id FROM cat_inventories WHERE set_num = ? COLLATE NOCASE ORDER BY version DESC LIMIT 1)
		ORDER BY m.name LIMIT ?`, num, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		var qty int
		if err := rows.Scan(&name, &qty); err != nil {
			return nil, err
		}
		if qty > 1 {
			name = fmt.Sprintf("%s x%d", name, qty)
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// OwnedPartsOfSafe is OwnedPartsOf that returns nothing on error (for existence checks).
func (d *DB) OwnedPartsOfSafe(partNum string) []OwnedPart {
	p, _ := d.OwnedPartsOf(partNum)
	return p
}
