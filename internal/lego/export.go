package lego

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html/template"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Export writes your collection (or one set's shopping list) in formats other tools
// read. Formats: rebrickable-csv (Part,Color,Quantity — what import-parts reads back),
// bricklink-xml (an inventory, QTY per line), csv (opens in Excel), json, and html
// (a printable page: use the browser's Print > Save as PDF).

// ExportRow is one line of an export.
type ExportRow struct {
	PartNum   string
	Name      string
	Category  string
	ColorID   int // Rebrickable colour id, -1 for a typed colour or none
	ColorName string
	Qty       int
	MinQty    int
	PartDBID  int
	BLColor   int // BrickLink colour number, 0 when unknown
}

// ExportData is what gets exported.
type ExportData struct {
	Title string
	Rows  []ExportRow
	Sets  []Set
	When  time.Time
}

// ExportOwned gathers your owned parts and sets.
func (d *DB) ExportOwned() (*ExportData, error) {
	owned, err := d.ListOwnedParts()
	if err != nil {
		return nil, err
	}
	sort.Slice(owned, func(i, j int) bool {
		if owned[i].PartNum != owned[j].PartNum {
			return owned[i].PartNum < owned[j].PartNum
		}
		return owned[i].ColorName < owned[j].ColorName
	})
	data := &ExportData{Title: "LEGO collection", When: time.Now()}
	for _, p := range owned {
		r := ExportRow{PartNum: p.PartNum, Name: p.Name, Category: p.Category, ColorID: p.ColorID, ColorName: p.ColorName, Qty: p.Qty, MinQty: p.MinQty, PartDBID: p.SyncedPartID}
		if p.ColorID >= 0 {
			r.BLColor, _ = d.BLColorFor(p.ColorID)
		}
		data.Rows = append(data.Rows, r)
	}
	rows, err := d.Query(`SELECT set_num, name, theme, year, qty, parts_qty, parted_out FROM sets ORDER BY set_num`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var s Set
		var out int
		if err := rows.Scan(&s.SetNum, &s.Name, &s.Theme, &s.Year, &s.Qty, &s.PartsQty, &out); err != nil {
			return nil, err
		}
		s.PartedOut = out != 0
		data.Sets = append(data.Sets, s)
	}
	return data, rows.Err()
}

// ExportMissing turns a missing-parts report into export rows (Qty is what is short).
func (d *DB) ExportMissing(setNum string, r *MissingReport) *ExportData {
	data := &ExportData{Title: fmt.Sprintf("Parts missing for set %s (x%d)", setNum, r.Copies), When: time.Now()}
	for _, m := range r.Missing {
		row := ExportRow{PartNum: m.PartNum, Name: m.PartName, ColorID: m.ColorID, ColorName: m.ColorName, Qty: m.Short, BLColor: m.BLColor}
		if row.BLColor == 0 && m.ColorID >= 0 {
			row.BLColor, _ = d.BLColorFor(m.ColorID)
		}
		data.Rows = append(data.Rows, row)
	}
	return data
}

// safeCell stops a spreadsheet treating text as a formula (CSV injection): a value
// starting with = + - @ tab or CR is prefixed with an apostrophe.
func safeCell(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

// ExportWarnings lists rows a format cannot express faithfully.
type ExportWarnings []string

// RebrickableCSV is a parts list Rebrickable and `wms lego import-parts` both read.
// Parts held in a typed (free-text) colour have no colour id and are left out, and
// counted in the warnings.
func RebrickableCSV(d *ExportData) ([]byte, ExportWarnings) {
	var b bytes.Buffer
	w := csv.NewWriter(&b)
	_ = w.Write([]string{"Part", "Color", "Quantity"})
	skipped := 0
	for _, r := range d.Rows {
		if r.ColorID < 0 {
			skipped++
			continue
		}
		_ = w.Write([]string{r.PartNum, strconv.Itoa(r.ColorID), strconv.Itoa(r.Qty)})
	}
	w.Flush()
	var warn ExportWarnings
	if skipped > 0 {
		warn = append(warn, fmt.Sprintf("%d line(s) in a typed colour have no Rebrickable colour id and were left out", skipped))
	}
	return b.Bytes(), warn
}

type blItemOut struct {
	XMLName  xml.Name `xml:"ITEM"`
	ItemType string   `xml:"ITEMTYPE"`
	ItemID   string   `xml:"ITEMID"`
	Color    *int     `xml:"COLOR,omitempty"`
	Qty      int      `xml:"QTY"`
}

// BrickLinkXML is your parts as a BrickLink inventory (one ITEM per line with QTY).
// Colours need the BrickLink colour map (`wms bricklink colors sync`); lines without
// one are written with the colour left out, and counted in the warnings.
func BrickLinkXML(d *ExportData) ([]byte, ExportWarnings, error) {
	inv := struct {
		XMLName xml.Name    `xml:"INVENTORY"`
		Items   []blItemOut `xml:"ITEM"`
	}{}
	noColor := 0
	for _, r := range d.Rows {
		if r.Qty <= 0 {
			continue
		}
		it := blItemOut{ItemType: "P", ItemID: r.PartNum, Qty: r.Qty}
		if r.BLColor > 0 {
			c := r.BLColor
			it.Color = &c
		} else {
			noColor++
		}
		inv.Items = append(inv.Items, it)
	}
	b, err := xml.MarshalIndent(inv, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	var warn ExportWarnings
	if noColor > 0 {
		warn = append(warn, fmt.Sprintf("%d line(s) have no BrickLink colour number, so their colour is left out (run `wms bricklink colors sync`)", noColor))
	}
	return append(b, '\n'), warn, nil
}

// SpreadsheetCSV is a CSV that opens cleanly in Excel or LibreOffice (text that could
// be read as a formula is neutralised).
func SpreadsheetCSV(d *ExportData) []byte {
	var b bytes.Buffer
	b.WriteString("\xef\xbb\xbf") // a byte-order mark, so Excel reads the file as UTF-8
	w := csv.NewWriter(&b)
	_ = w.Write([]string{"Part", "Name", "Category", "Colour", "Colour id", "Quantity", "Minimum", "Part-DB id"})
	for _, r := range d.Rows {
		id, pdb := "", ""
		if r.ColorID >= 0 {
			id = strconv.Itoa(r.ColorID)
		}
		if r.PartDBID > 0 {
			pdb = strconv.Itoa(r.PartDBID)
		}
		_ = w.Write([]string{safeCell(r.PartNum), safeCell(r.Name), safeCell(r.Category), safeCell(r.ColorName), id, strconv.Itoa(r.Qty), strconv.Itoa(r.MinQty), pdb})
	}
	w.Flush()
	return b.Bytes()
}

// SetsCSV lists your sets (opens in Excel).
func SetsCSV(d *ExportData) []byte {
	var b bytes.Buffer
	b.WriteString("\xef\xbb\xbf")
	w := csv.NewWriter(&b)
	_ = w.Write([]string{"Set", "Name", "Theme", "Year", "Copies", "Pieces", "Parted out"})
	for _, s := range d.Sets {
		_ = w.Write([]string{safeCell(s.SetNum), safeCell(s.Name), safeCell(s.Theme), strconv.Itoa(s.Year), strconv.Itoa(s.Qty), strconv.Itoa(s.PartsQty), map[bool]string{true: "yes", false: "no"}[s.PartedOut]})
	}
	w.Flush()
	return b.Bytes()
}

// JSON is everything, structured.
func JSON(d *ExportData) ([]byte, error) {
	type row struct {
		Part     string `json:"part"`
		Name     string `json:"name"`
		Category string `json:"category"`
		Colour   string `json:"colour"`
		ColourID int    `json:"colour_id"`
		Quantity int    `json:"quantity"`
		Minimum  int    `json:"minimum"`
		PartDBID int    `json:"part_db_id,omitempty"`
		BLColour int    `json:"bricklink_colour,omitempty"`
	}
	type set struct {
		Set       string `json:"set"`
		Name      string `json:"name"`
		Theme     string `json:"theme"`
		Year      int    `json:"year"`
		Copies    int    `json:"copies"`
		Pieces    int    `json:"pieces"`
		PartedOut bool   `json:"parted_out"`
	}
	out := struct {
		Title    string `json:"title"`
		Exported string `json:"exported"`
		Parts    []row  `json:"parts"`
		Sets     []set  `json:"sets"`
	}{Title: d.Title, Exported: d.When.UTC().Format(time.RFC3339), Parts: []row{}, Sets: []set{}}
	for _, r := range d.Rows {
		out.Parts = append(out.Parts, row{r.PartNum, r.Name, r.Category, r.ColorName, r.ColorID, r.Qty, r.MinQty, r.PartDBID, r.BLColor})
	}
	for _, s := range d.Sets {
		out.Sets = append(out.Sets, set{s.SetNum, s.Name, s.Theme, s.Year, s.Qty, s.PartsQty, s.PartedOut})
	}
	return json.MarshalIndent(out, "", "  ")
}

var htmlTmpl = template.Must(template.New("inv").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>{{.Title}}</title>
<style>
 body{font:14px/1.4 system-ui,sans-serif;margin:2rem;color:#111}
 h1{margin:0 0 .25rem} .meta{color:#555;margin-bottom:1rem}
 table{border-collapse:collapse;width:100%;margin-bottom:2rem} th,td{border-bottom:1px solid #ccc;padding:.25rem .5rem;text-align:left}
 th{background:#eee} td.n,th.n{text-align:right} tr{page-break-inside:avoid}
 @media print{body{margin:0} th{background:#ddd !important;-webkit-print-color-adjust:exact}}
</style></head><body>
<h1>{{.Title}}</h1>
<div class="meta">Exported {{.Date}} · {{.PartLines}} part line(s), {{.Pieces}} piece(s){{if .Sets}}, {{len .Sets}} set(s){{end}} · Data: Rebrickable</div>
{{if .Sets}}<h2>Sets</h2><table><tr><th>Set</th><th>Name</th><th>Theme</th><th class="n">Year</th><th class="n">Copies</th><th class="n">Pieces</th></tr>
{{range .Sets}}<tr><td>{{.SetNum}}</td><td>{{.Name}}</td><td>{{.Theme}}</td><td class="n">{{.Year}}</td><td class="n">{{.Qty}}</td><td class="n">{{.PartsQty}}</td></tr>
{{end}}</table>{{end}}
{{if .Rows}}<h2>Parts</h2><table><tr><th>Part</th><th>Name</th><th>Colour</th><th class="n">Quantity</th></tr>
{{range .Rows}}<tr><td>{{.PartNum}}</td><td>{{.Name}}</td><td>{{.ColorName}}</td><td class="n">{{.Qty}}</td></tr>
{{end}}</table>{{end}}
</body></html>`))

// HTML is a printable page (all values are escaped).
func HTML(d *ExportData) ([]byte, error) {
	pieces := 0
	for _, r := range d.Rows {
		pieces += r.Qty
	}
	var b bytes.Buffer
	err := htmlTmpl.Execute(&b, struct {
		*ExportData
		Date      string
		PartLines int
		Pieces    int
	}{d, d.When.Format("2006-01-02 15:04"), len(d.Rows), pieces})
	return b.Bytes(), err
}
