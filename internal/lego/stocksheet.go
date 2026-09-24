package lego

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"sort"
)

// StockSheetLine is one line of a printable stock-check sheet: what you'd expect to
// have, with a blank box to write what you actually count.
type StockSheetLine struct {
	PartNum, PartName, Category, ColorName string
	Expected                               int
	Optional                               bool // still listed to count against, but not in Total — see DB.IsOptional
}

// StockSheet builds a printable checklist for setNum: the last check's parts if it
// has one (so a recount sheet matches exactly what was last recorded), or the
// catalog's parts list when it has never been checked — so you can print a blank
// sheet to count against before the first digital check too.
func (d *DB) StockSheet(ctx context.Context, rb *Client, setNum string) (title string, lines []StockSheetLine, err error) {
	title = setTitle(d, setNum)
	if c, cerr := d.LastCheck(setNum); cerr == nil && c != nil {
		for _, l := range c.Lines {
			lines = append(lines, StockSheetLine{PartNum: l.PartNum, PartName: l.PartName, Category: l.Category, ColorName: l.ColorName, Expected: l.Need, Optional: l.Optional})
		}
		return title, lines, nil
	}
	inv := d.LookupSetInventory(ctx, rb, setNum)
	if len(inv.Items) == 0 {
		return title, nil, fmt.Errorf("no parts list for set %s", setNum)
	}
	cats := map[string]string{}
	seen := map[[2]any]int{}
	for _, it := range inv.Items {
		if it.Qty <= 0 {
			continue
		}
		k := [2]any{it.PartNum, it.ColorID}
		if i, ok := seen[k]; ok {
			lines[i].Expected += it.Qty
			continue
		}
		cat, ok := cats[it.PartNum]
		if !ok {
			if cp, _ := d.CatalogPart(it.PartNum); cp != nil {
				cat = cp.Category
			}
			cats[it.PartNum] = cat
		}
		optional, _ := d.IsOptional(it.PartNum, cat)
		seen[k] = len(lines)
		lines = append(lines, StockSheetLine{PartNum: it.PartNum, PartName: it.PartName, Category: cat, ColorName: it.ColorName, Expected: it.Qty, Optional: optional})
	}
	sort.SliceStable(lines, func(i, j int) bool {
		if lines[i].ColorName != lines[j].ColorName {
			return lines[i].ColorName < lines[j].ColorName
		}
		return lines[i].PartNum < lines[j].PartNum
	})
	return title, lines, nil
}

var stockSheetTmpl = template.Must(template.New("stocksheet").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>Stock check — {{.Title}}</title>
<style>
 body{font:13px/1.35 system-ui,sans-serif;margin:1.2rem;color:#111}
 .head{display:flex;justify-content:space-between;align-items:flex-end;border-bottom:3px solid #222;padding-bottom:.6rem;margin-bottom:1rem}
 h1{margin:0;font-size:20px} .meta{color:#555;font-size:12px}
 .who{display:flex;gap:2rem;margin-bottom:1rem;font-size:12px}
 .who .field{border-bottom:1px solid #333;min-width:14rem;display:inline-block;height:1.2em}
 table{border-collapse:collapse;width:100%} th,td{border-bottom:1px solid #ccc;padding:.35rem .5rem;text-align:left}
 th{background:#eee} td.n,th.n{text-align:right;width:5em}
 td.box{width:2.4rem} .box span{display:inline-block;width:1.5rem;height:1.5rem;border:1.5px solid #333}
 .opt{color:#888;font-weight:normal}
 tr{page-break-inside:avoid}
 .foot{margin-top:1.5rem;font-size:11px;color:#888;text-align:center}
 @media print{body{margin:0}}
</style></head><body>
<div class="head"><h1>Stock check — {{.Title}}</h1><div class="meta">{{.Lines}} line(s), {{.Total}} piece(s) expected</div></div>
<div class="who">Checked by <span class="field"></span> &nbsp; Date <span class="field"></span></div>
<table><tr><th class="box">✓</th><th>Part</th><th>Colour</th><th>Name</th><th class="n">Expected</th><th class="n">Counted</th></tr>
{{range .Lines}}<tr><td class="box"><span></span></td><td>{{.PartNum}}</td><td>{{.ColorName}}</td><td>{{.PartName}}{{if .Optional}} <span class="opt">(optional)</span>{{end}}</td><td class="n">{{.Expected}}</td><td class="n"></td></tr>
{{end}}</table>
<div class="foot">Print at 100% · KC-PARTS</div>
</body></html>`))

// StockSheetHTML renders the checklist.
func StockSheetHTML(title string, lines []StockSheetLine) ([]byte, error) {
	total := 0
	for _, l := range lines {
		if !l.Optional {
			total += l.Expected
		}
	}
	var b bytes.Buffer
	err := stockSheetTmpl.Execute(&b, struct {
		Title string
		Lines []StockSheetLine
		Total int
	}{title, lines, total})
	return b.Bytes(), err
}
