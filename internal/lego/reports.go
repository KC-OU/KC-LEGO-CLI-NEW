package lego

import (
	"bytes"
	"fmt"
	"html/template"
	"sort"
	"strings"
	"time"
)

// Reports turn what already exists (ShoppingList, Stats, set checks) into something
// meant to be read or printed — a title, totals, and sections — rather than a flat
// table export. Every report also assembles an *ExportData so the existing xlsx/csv/
// json writers (Encode) work on it unchanged; the HTML here is the "clean form" you
// asked for specifically, closer to a real document than SpreadsheetCSV/HTML's table.

// CheckSummary is one past check, without its lines (for history listings — the
// per-set totals are already stored on set_checks, so this never re-reads
// set_check_lines).
type CheckSummary struct {
	ID                                  int64
	SetNum, Kind, CheckedBy             string
	FinishedAt                          time.Time
	Lines, Pieces, Have, Missing, Extra int
}

// CheckHistory lists finished checks, newest first: for one set (setNum != ""), or
// across the whole collection.
func (d *DB) CheckHistory(setNum string, limit int) ([]CheckSummary, error) {
	if limit <= 0 {
		limit = 50
	}
	q := `SELECT id, set_num, kind, checked_by, finished_at, lines, pieces, have, missing, extra
		FROM set_checks WHERE status = ?`
	args := []any{StatusDone}
	if setNum != "" {
		q += ` AND set_num = ?`
		args = append(args, setNum)
	}
	q += ` ORDER BY finished_at DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CheckSummary
	for rows.Next() {
		var s CheckSummary
		var finished string
		if err := rows.Scan(&s.ID, &s.SetNum, &s.Kind, &s.CheckedBy, &finished, &s.Lines, &s.Pieces, &s.Have, &s.Missing, &s.Extra); err != nil {
			return nil, err
		}
		s.FinishedAt, _ = time.Parse(timeLayout, finished)
		out = append(out, s)
	}
	return out, rows.Err()
}

func setTitle(d *DB, setNum string) string {
	if s, _ := d.CatalogSet(setNum); s != nil && s.Name != "" {
		return setNum + " " + s.Name
	}
	if s, _ := d.GetSetByNum(strings.TrimSuffix(setNum, "-1")); s != nil && s.Name != "" {
		return setNum + " " + s.Name
	}
	return setNum
}

// MissingPartsReport covers the given sets, or every incomplete set when sets is
// empty. Rows carry SetNum so a multi-set report stays one document.
func (d *DB) MissingPartsReport(sets []string) (*ExportData, error) {
	if len(sets) == 0 {
		inc, err := d.IncompleteSets()
		if err != nil {
			return nil, err
		}
		for _, s := range inc {
			sets = append(sets, s.SetNum)
		}
	}
	data := &ExportData{Title: "Missing parts report", When: time.Now()}
	totalMissing, totalOnOrder := 0, 0
	for _, num := range sets {
		lines, err := d.ShoppingList(num)
		if err != nil {
			continue // not checked, or nothing missing — not a report failure
		}
		title := setTitle(d, num)
		setMissing, setOnOrder := 0, 0
		for _, l := range lines {
			data.Rows = append(data.Rows, ExportRow{SetNum: title, PartNum: l.PartNum, Name: l.PartName, Category: l.Category,
				ColorID: l.ColorID, ColorName: l.ColorName, Qty: l.Short, BLColor: l.BLColor})
			setMissing += l.Short
			setOnOrder += l.OnOrder
		}
		if len(lines) > 0 {
			data.Facts = append(data.Facts, [2]string{title, fmt.Sprintf("%d line(s), %d part(s) missing, %d on order", len(lines), setMissing, setOnOrder)})
		}
		totalMissing += setMissing
		totalOnOrder += setOnOrder
	}
	data.Facts = append([][2]string{{"Sets covered", fmt.Sprint(len(sets))}, {"Total missing", fmt.Sprint(totalMissing)}, {"Total on order", fmt.Sprint(totalOnOrder)}}, data.Facts...)
	return data, nil
}

// CollectionReport is every set and every loose part you own, with a summary.
func (d *DB) CollectionReport() (*ExportData, error) {
	owned, err := d.ExportOwned()
	if err != nil {
		return nil, err
	}
	st, err := d.Stats()
	if err != nil {
		return nil, err
	}
	data := &ExportData{Title: "Collection report", When: time.Now(), Rows: owned.Rows, Sets: owned.Sets}
	data.Facts = [][2]string{
		{"Sets", fmt.Sprintf("%d title(s), %d cop(ies)", st.SetTitles, st.SetCopies)},
		{"Pieces in sets", fmt.Sprint(st.SetPieces)},
		{"Loose parts", fmt.Sprintf("%d line(s), %d piece(s)", st.PartLines, st.LoosePieces)},
		{"Distinct parts", fmt.Sprint(st.DistinctParts)},
		{"Below minimum", fmt.Sprint(st.LowStock)},
	}
	return data, nil
}

// SetPartsReport is one or several sets, everything in each side by side.
func (d *DB) SetPartsReport(sets []string) (*ExportData, error) {
	data := &ExportData{Title: "Set parts report", When: time.Now()}
	for _, num := range sets {
		items, err := d.CatalogSetInventory(num)
		if err != nil || len(items) == 0 {
			continue
		}
		title := setTitle(d, num)
		pieces := 0
		for _, it := range items {
			data.Rows = append(data.Rows, ExportRow{SetNum: title, PartNum: it.PartNum, Name: it.PartName, ColorID: it.ColorID, ColorName: it.ColorName, Qty: it.Qty, BLColor: it.BLColor})
			pieces += it.Qty
		}
		data.Facts = append(data.Facts, [2]string{title, fmt.Sprintf("%d line(s), %d piece(s)", len(items), pieces)})
	}
	return data, nil
}

// SetsListReport is every set you own — no loose parts (see CollectionReport for
// sets and loose parts together). Its own SetsListHTML renders it: the generic
// ReportHTML only ever shows ExportData.Rows (parts), never .Sets.
func (d *DB) SetsListReport() (*ExportData, error) {
	st, err := d.Stats()
	if err != nil {
		return nil, err
	}
	rows, err := d.Query(`SELECT set_num, name, theme, year, qty, parts_qty, parted_out FROM sets ORDER BY set_num`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	data := &ExportData{Title: "List of sets", When: time.Now()}
	for rows.Next() {
		var s Set
		var partedOut int
		if err := rows.Scan(&s.SetNum, &s.Name, &s.Theme, &s.Year, &s.Qty, &s.PartsQty, &partedOut); err != nil {
			return nil, err
		}
		s.PartedOut = partedOut != 0
		data.Sets = append(data.Sets, s)
	}
	data.Facts = [][2]string{
		{"Sets", fmt.Sprintf("%d title(s), %d cop(ies)", st.SetTitles, st.SetCopies)},
		{"Pieces in sets", fmt.Sprint(st.SetPieces)},
	}
	return data, rows.Err()
}

// ExtraPartsReport is loose parts that came from a completed set check's overage
// (part_origins — "2 spare in 10696"), grouped by which set they came from. sets
// filters to those origin sets; empty means every set with recorded extras.
func (d *DB) ExtraPartsReport(sets []string) (*ExportData, error) {
	data := &ExportData{Title: "Extra parts report", When: time.Now()}
	q := `SELECT po.part_num, po.color_id, o.name, o.category, o.color_name, po.origin_set, po.qty
		FROM part_origins po JOIN owned_parts o ON o.part_num = po.part_num AND o.color_id = po.color_id
		WHERE po.qty > 0`
	var args []any
	if len(sets) > 0 {
		ph := make([]string, len(sets))
		for i, s := range sets {
			ph[i] = "?"
			args = append(args, s)
		}
		q += ` AND po.origin_set IN (` + strings.Join(ph, ",") + `)`
	}
	rows, err := d.Query(q+` ORDER BY po.origin_set, po.part_num`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	titleFor := map[string]string{}
	total := 0
	for rows.Next() {
		var partNum, name, category, colorName, originSet string
		var colorID, qty int
		if err := rows.Scan(&partNum, &colorID, &name, &category, &colorName, &originSet, &qty); err != nil {
			return nil, err
		}
		title, ok := titleFor[originSet]
		if !ok {
			title = setTitle(d, originSet)
			titleFor[originSet] = title
		}
		data.Rows = append(data.Rows, ExportRow{SetNum: title, PartNum: partNum, Name: name, Category: category, ColorID: colorID, ColorName: colorName, Qty: qty})
		total += qty
	}
	data.Facts = [][2]string{{"Extra line(s)", fmt.Sprint(len(data.Rows))}, {"Total extra pieces", fmt.Sprint(total)}}
	return data, rows.Err()
}

// OrderListReport is every parts order (status "" = all, "open" = not yet received/
// cancelled), most recent first, one group per order — reusing the generic report
// template's existing by-SetNum grouping to group rows by order instead of by set.
func (d *DB) OrderListReport(status string) (*ExportData, error) {
	orders, err := d.ListOrders(status)
	if err != nil {
		return nil, err
	}
	data := &ExportData{Title: "Order list", When: time.Now()}
	total := 0.0
	for _, o := range orders {
		supplier := o.Supplier
		if supplier == "" {
			supplier = "—"
		}
		label := fmt.Sprintf("Order #%d — %s (%s)", o.ID, supplier, o.Status)
		for _, l := range o.Lines {
			name := l.PartName
			if l.UnitPrice > 0 {
				name = fmt.Sprintf("%s — %.2f each", name, l.UnitPrice)
			}
			data.Rows = append(data.Rows, ExportRow{SetNum: label, PartNum: l.PartNum, Name: name, ColorID: l.ColorID, ColorName: l.ColorName, Qty: l.Qty})
		}
		total += o.Total()
	}
	data.Facts = [][2]string{{"Orders", fmt.Sprint(len(orders))}, {"Total spent", fmt.Sprintf("%.2f", total)}}
	return data, nil
}

// ---- report HTML (the "clean form") ----

func reportPieces(rows []ExportRow) int {
	n := 0
	for _, r := range rows {
		n += r.Qty
	}
	return n
}

// bySet groups rows by SetNum, in first-seen order — the report is read set by set.
func bySet(rows []ExportRow) (order []string, groups map[string][]ExportRow) {
	groups = map[string][]ExportRow{}
	for _, r := range rows {
		k := r.SetNum
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], r)
	}
	return order, groups
}

var reportTmpl = template.Must(template.New("report").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>{{.Title}}</title>
<style>
 body{font:14px/1.45 system-ui,sans-serif;margin:0;color:#111}
 .page{max-width:900px;margin:0 auto;padding:2rem}
 .title{text-align:center;padding:3rem 0 2rem;border-bottom:4px solid #222;margin-bottom:2rem}
 .title h1{margin:0 0 .3rem;font-size:28px} .title .meta{color:#666}
 table.facts{width:100%;border-collapse:collapse;margin-bottom:2rem}
 table.facts td{padding:.35rem .5rem;border-bottom:1px solid #eee} table.facts td:first-child{font-weight:600;width:40%}
 h2{margin:2rem 0 .5rem;padding-bottom:.25rem;border-bottom:2px solid #333;font-size:18px}
 table.rows{border-collapse:collapse;width:100%;margin-bottom:1rem}
 table.rows th,table.rows td{border-bottom:1px solid #ddd;padding:.3rem .5rem;text-align:left;font-size:13px}
 table.rows th{background:#f2f2f2} td.n,th.n{text-align:right}
 .footer{color:#999;font-size:11px;text-align:center;margin-top:3rem;padding-top:1rem;border-top:1px solid #eee}
 @media print{.page{max-width:none} h2{break-after:avoid} tr{break-inside:avoid}}
</style></head><body><div class="page">
<div class="title"><h1>{{.Title}}</h1><div class="meta">Generated {{.Date}}</div></div>
{{if .Facts}}<table class="facts">{{range .Facts}}<tr><td>{{index . 0}}</td><td>{{index . 1}}</td></tr>{{end}}</table>{{end}}
{{range .Groups}}<h2>{{.Title}}</h2><table class="rows"><tr><th>Part</th><th>Colour</th><th>Name</th><th class="n">Qty</th></tr>
{{range .Rows}}<tr><td>{{.PartNum}}</td><td>{{.ColorName}}</td><td>{{.Name}}</td><td class="n">{{.Qty}}</td></tr>{{end}}
</table>{{end}}
<div class="footer">{{.Pieces}} piece(s) across {{.Lines}} line(s) · KC-PARTS</div>
</div></body></html>`))

// ReportHTML renders a report's rows as the clean, title-page-style document (grouped
// by set when the rows carry more than one).
func ReportHTML(d *ExportData) ([]byte, error) {
	order, groups := bySet(d.Rows)
	sort.Strings(order)
	type group struct {
		Title string
		Rows  []ExportRow
	}
	var gs []group
	for _, k := range order {
		title := k
		if title == "" {
			title = "Loose parts"
		}
		gs = append(gs, group{Title: title, Rows: groups[k]})
	}
	if len(order) == 0 && len(d.Rows) > 0 {
		gs = append(gs, group{Title: d.Title, Rows: d.Rows})
	}
	var b bytes.Buffer
	err := reportTmpl.Execute(&b, struct {
		Title, Date   string
		Facts         [][2]string
		Groups        []group
		Pieces, Lines int
	}{d.Title, d.When.Format("2 January 2006, 15:04"), d.Facts, gs, reportPieces(d.Rows), len(d.Rows)})
	return b.Bytes(), err
}

// ---- check history HTML ----

var historyTmpl = template.Must(template.New("history").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>{{.Title}}</title>
<style>
 body{font:14px/1.45 system-ui,sans-serif;margin:0;color:#111}
 .page{max-width:900px;margin:0 auto;padding:2rem}
 .title{text-align:center;padding:3rem 0 2rem;border-bottom:4px solid #222;margin-bottom:2rem}
 .title h1{margin:0 0 .3rem;font-size:28px} .title .meta{color:#666}
 table{border-collapse:collapse;width:100%} th,td{border-bottom:1px solid #ddd;padding:.3rem .5rem;text-align:left;font-size:13px}
 th{background:#f2f2f2} td.n,th.n{text-align:right}
 .complete{color:#0a7d2c;font-weight:600} .incomplete{color:#b02a2a;font-weight:600}
 @media print{.page{max-width:none} tr{break-inside:avoid}}
</style></head><body><div class="page">
<div class="title"><h1>{{.Title}}</h1><div class="meta">Generated {{.Date}} · {{len .Checks}} check(s)</div></div>
<table><tr><th>Date</th><th>Set</th><th>Kind</th><th>By</th><th class="n">Pieces</th><th class="n">Have</th><th class="n">Missing</th><th class="n">Extra</th><th>Result</th></tr>
{{range .Checks}}<tr><td>{{.FinishedAt.Format "2 Jan 2006 15:04"}}</td><td>{{.SetNum}}</td><td>{{.Kind}}</td><td>{{.CheckedBy}}</td>
<td class="n">{{.Pieces}}</td><td class="n">{{.Have}}</td><td class="n">{{.Missing}}</td><td class="n">{{.Extra}}</td>
<td>{{if eq .Missing 0}}<span class="complete">COMPLETE</span>{{else}}<span class="incomplete">{{.Missing}} missing</span>{{end}}</td></tr>
{{end}}</table>
</div></body></html>`))

// CheckHistoryHTML renders a history listing as a clean, printable page.
func CheckHistoryHTML(title string, checks []CheckSummary) ([]byte, error) {
	var b bytes.Buffer
	err := historyTmpl.Execute(&b, struct {
		Title, Date string
		Checks      []CheckSummary
	}{title, time.Now().Format("2 January 2006, 15:04"), checks})
	return b.Bytes(), err
}

// ---- wishlist HTML (a read-only page for someone without the CLI) ----

// wishItem is one watch, with a set's watch given its catalog title — for
// someone else reading this (family before a birthday), a bare set number
// alone means nothing.
type wishItem struct {
	Watch
	Title string // "75192 Millennium Falcon" for a set watch; "" for a part watch
}

var wishlistTmpl = template.Must(template.New("wishlist").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>{{.Title}}</title>
<style>
 body{font:15px/1.5 system-ui,sans-serif;margin:0;color:#111;background:#fafafa}
 .page{max-width:700px;margin:0 auto;padding:2.5rem 1.5rem}
 .title{text-align:center;padding-bottom:1.5rem;margin-bottom:2rem;border-bottom:3px solid #222}
 .title h1{margin:0 0 .3rem;font-size:26px} .title .meta{color:#666;font-size:14px}
 ul{list-style:none;padding:0;margin:0} li{background:#fff;border:1px solid #e3e3e3;border-radius:8px;padding:1rem 1.2rem;margin-bottom:.8rem}
 .name{font-weight:600;font-size:17px} .sub{color:#666;font-size:13px;margin-top:.2rem}
 .price{float:right;color:#0a7d2c;font-weight:600}
 .empty{text-align:center;color:#888;padding:3rem 0}
 @media print{body{background:#fff} li{break-inside:avoid}}
</style></head><body><div class="page">
<div class="title"><h1>{{.Title}}</h1><div class="meta">{{.Date}}</div></div>
{{if .Items}}<ul>
{{range .Items}}<li><span class="price">{{if .LastPrice}}{{printf "%.2f" .LastPrice}}{{end}}</span>
<div class="name">{{if .Title}}{{.Title}}{{else}}{{.ItemNo}}{{if .ColorName}} — {{.ColorName}}{{end}}{{end}}</div>
<div class="sub">{{.ItemType}} · wants it at {{printf "%.2f" .MaxPrice}} or less{{if .Cond}} ({{if eq .Cond "N"}}new{{else}}used{{end}}){{end}}</div>
</li>{{end}}
</ul>{{else}}<div class="empty">Nothing on the list right now.</div>{{end}}
</div></body></html>`))

// WishlistHTML renders watches as a plain-English page for someone without
// the CLI — the watch list ("wms lego watch") read as a wishlist, since
// that's what it already is: things wanted, at a price.
func WishlistHTML(d *DB, title string, watches []Watch) ([]byte, error) {
	items := make([]wishItem, len(watches))
	for i, w := range watches {
		it := wishItem{Watch: w}
		if w.ItemType == "SET" {
			it.Title = setTitle(d, w.ItemNo)
		}
		items[i] = it
	}
	var b bytes.Buffer
	err := wishlistTmpl.Execute(&b, struct {
		Title, Date string
		Items       []wishItem
	}{title, time.Now().Format("2 January 2006"), items})
	return b.Bytes(), err
}

// ---- sets list HTML (a clean form for ExportData.Sets, which ReportHTML never renders) ----

var setsListTmpl = template.Must(template.New("setslist").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>{{.Title}}</title>
<style>
 body{font:14px/1.45 system-ui,sans-serif;margin:0;color:#111}
 .page{max-width:900px;margin:0 auto;padding:2rem}
 .title{text-align:center;padding:3rem 0 2rem;border-bottom:4px solid #222;margin-bottom:2rem}
 .title h1{margin:0 0 .3rem;font-size:28px} .title .meta{color:#666}
 table.facts{width:100%;border-collapse:collapse;margin-bottom:2rem}
 table.facts td{padding:.35rem .5rem;border-bottom:1px solid #eee} table.facts td:first-child{font-weight:600;width:40%}
 table.rows{border-collapse:collapse;width:100%}
 table.rows th,table.rows td{border-bottom:1px solid #ddd;padding:.35rem .5rem;text-align:left;font-size:13px}
 table.rows th{background:#f2f2f2} td.n,th.n{text-align:right}
 .parted{color:#999}
 @media print{.page{max-width:none} tr{break-inside:avoid}}
</style></head><body><div class="page">
<div class="title"><h1>{{.Title}}</h1><div class="meta">Generated {{.Date}} · {{len .Sets}} set(s)</div></div>
{{if .Facts}}<table class="facts">{{range .Facts}}<tr><td>{{index . 0}}</td><td>{{index . 1}}</td></tr>{{end}}</table>{{end}}
<table class="rows"><tr><th>Set</th><th>Name</th><th>Theme</th><th class="n">Year</th><th class="n">Copies</th><th class="n">Pieces</th><th>Status</th></tr>
{{range .Sets}}<tr{{if .PartedOut}} class="parted"{{end}}><td>{{.SetNum}}</td><td>{{.Name}}</td><td>{{.Theme}}</td><td class="n">{{.Year}}</td><td class="n">{{.Qty}}</td><td class="n">{{.PartsQty}}</td><td>{{if .PartedOut}}parted out{{else}}kept{{end}}</td></tr>
{{end}}</table>
</div></body></html>`))

// ---- cheat sheet HTML (a printable page from the same key/description pairs the
// in-app F1 help shows — see uiapp.CheatSheetSections, the only place this content
// is written down) ----

// CheatSheetSection is one titled block of key/description pairs.
type CheatSheetSection struct {
	Title string
	Keys  [][2]string
}

var cheatSheetTmpl = template.Must(template.New("cheatsheet").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>{{.Title}}</title>
<style>
 body{font:14px/1.45 system-ui,sans-serif;margin:0;color:#111}
 .page{max-width:700px;margin:0 auto;padding:2rem}
 .title{text-align:center;padding:2rem 0 1.5rem;border-bottom:4px solid #222;margin-bottom:1.5rem}
 .title h1{margin:0 0 .3rem;font-size:26px} .title .meta{color:#666}
 h2{margin:1.5rem 0 .4rem;padding-bottom:.2rem;border-bottom:2px solid #333;font-size:16px}
 table{border-collapse:collapse;width:100%;margin-bottom:.5rem}
 td{padding:.25rem .5rem;border-bottom:1px solid #eee;font-size:13px;vertical-align:top}
 td:first-child{font-weight:600;white-space:nowrap;width:1%}
 @media print{.page{max-width:none} h2{break-after:avoid} tr{break-inside:avoid}}
</style></head><body><div class="page">
<div class="title"><h1>{{.Title}}</h1><div class="meta">Generated {{.Date}}</div></div>
{{range .Sections}}<h2>{{.Title}}</h2><table>{{range .Keys}}<tr><td>{{index . 0}}</td><td>{{index . 1}}</td></tr>{{end}}</table>{{end}}
</div></body></html>`))

// CheatSheetHTML renders the key/description sections as a printable page.
func CheatSheetHTML(title string, sections []CheatSheetSection) ([]byte, error) {
	var b bytes.Buffer
	err := cheatSheetTmpl.Execute(&b, struct {
		Title, Date string
		Sections    []CheatSheetSection
	}{title, time.Now().Format("2 January 2006"), sections})
	return b.Bytes(), err
}

// SetsListHTML renders a SetsListReport's sets as a clean, printable page.
func SetsListHTML(d *ExportData) ([]byte, error) {
	var b bytes.Buffer
	err := setsListTmpl.Execute(&b, struct {
		Title, Date string
		Facts       [][2]string
		Sets        []Set
	}{d.Title, d.When.Format("2 January 2006, 15:04"), d.Facts, d.Sets})
	return b.Bytes(), err
}
