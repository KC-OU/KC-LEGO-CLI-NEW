package lego

import (
	"bytes"
	"html/template"
	"sort"
)

// SortingHTML is a set's parts grouped by colour, then category, with pictures,
// counts and tick boxes: a sheet for sorting a set's parts or building it.
func SortingHTML(d *ExportData) ([]byte, error) {
	type group struct {
		Colour string
		Pieces int
		Rows   []ExportRow
	}
	byColour := map[string]*group{}
	var order []string
	for _, r := range d.Rows {
		c := r.ColorName
		if c == "" {
			c = "(no colour)"
		}
		g := byColour[c]
		if g == nil {
			g = &group{Colour: c}
			byColour[c] = g
			order = append(order, c)
		}
		g.Rows = append(g.Rows, r)
		g.Pieces += r.Qty
	}
	sort.Strings(order)
	var groups []group
	total := 0
	for _, c := range order {
		g := byColour[c]
		sort.SliceStable(g.Rows, func(i, j int) bool {
			if g.Rows[i].Category != g.Rows[j].Category {
				return g.Rows[i].Category < g.Rows[j].Category
			}
			return g.Rows[i].PartNum < g.Rows[j].PartNum
		})
		groups = append(groups, *g)
		total += g.Pieces
	}
	var b bytes.Buffer
	err := sortingTmpl.Execute(&b, map[string]any{"Title": d.Title, "Groups": groups, "Total": total, "Lines": len(d.Rows), "Date": d.When.Format("2006-01-02")})
	return b.Bytes(), err
}

var sortingTmpl = template.Must(template.New("sort").Funcs(template.FuncMap{"imgsrc": imgSrc}).Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>Sorting sheet — {{.Title}}</title>
<style>
 body{font:13px/1.35 system-ui,sans-serif;margin:1.5rem;color:#111}
 h1{margin:0} .meta{color:#555;margin:.2rem 0 1rem}
 h2{font-size:15px;margin:1.2rem 0 .3rem;border-bottom:2px solid #333;padding-bottom:2px}
 table{border-collapse:collapse;width:100%} td{border-bottom:1px solid #ddd;padding:2px 6px;vertical-align:middle}
 td.n{text-align:right;font-weight:600;width:3em} td.box{width:1.4em} .box span{display:inline-block;width:12px;height:12px;border:1.5px solid #333}
 img{width:40px;height:30px;object-fit:contain} .cat{color:#666}
 @media print{body{margin:0} h2{break-after:avoid} tr{break-inside:avoid}}
</style></head><body>
<h1>{{.Title}}</h1>
<div class="meta">Sorting sheet · {{.Lines}} lines · {{.Total}} pieces · by colour, then category · {{.Date}}</div>
{{range .Groups}}<h2>{{.Colour}} — {{.Pieces}} piece(s)</h2><table>
{{range .Rows}}<tr><td class="box"><span></span></td><td>{{with .Image}}<img alt="" loading="lazy" src="{{imgsrc .}}">{{end}}</td><td class="n">{{.Qty}}×</td><td>{{.PartNum}}</td><td>{{.Name}}</td><td class="cat">{{.Category}}</td></tr>
{{end}}</table>{{end}}
</body></html>`))
