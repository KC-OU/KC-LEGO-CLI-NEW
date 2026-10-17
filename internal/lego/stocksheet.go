package lego

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"path/filepath"
	"sort"
	"strings"
)

// StockSheetLine is one line of a printable stock-check sheet: what you'd expect to
// have, with a blank box to write what you actually count.
type StockSheetLine struct {
	PartNum, PartName, Category, ColorName string
	Expected                               int
	Optional                               bool // still listed to count against, but not in Total — see DB.IsOptional
}

// StockSheetMeta is everything about the set itself that goes on the front sheet's
// header/condition/cost/sign-off blocks: prefilled wherever it's already known
// (catalog data, a stored BrickLink price, the last check), left for the paper copy
// to fill in by hand otherwise. HasCounts/ExtraLines/MissingLines/the Parts* and
// Minifigs* counts only come from an existing finished check — a set printed before
// its first check has none of those yet, same as always.
type StockSheetMeta struct {
	SetNum, Name, Theme, Storage, Condition string
	PiecesExpected                          int
	BrickLinkPrice                          string // formatted "avg currency", e.g. "42.10 GBP" — blank when never fetched
	HasCounts                               bool
	PartsGot, PartsMissing                  int
	MinifigsGot, MinifigsMissing            int
	ExtraLines, MissingLines                []StockSheetLine
	ProofFiled                              bool
	ProofRef                                string
	EnteredInCLI                            bool
}

func isMinifigCategory(cat string) bool { return strings.Contains(strings.ToLower(cat), "minifig") }

// StockSheet builds a printable checklist for setNum: the last check's parts if it
// has one (so a recount sheet matches exactly what was last recorded), or the
// catalog's parts list when it has never been checked — so you can print a blank
// sheet to count against before the first digital check too. meta carries the
// set-level fields (header, cost, condition, counts, sign-off) the HTML template
// renders around that line list.
func (d *DB) StockSheet(ctx context.Context, rb *Client, setNum string) (title string, lines []StockSheetLine, meta StockSheetMeta, err error) {
	title = setTitle(d, setNum)
	st := d.GetSetState(setNum)
	meta = StockSheetMeta{SetNum: setNum, Storage: st.Location, Condition: st.Condition}
	if hit, _ := d.CatalogSet(setNum); hit != nil {
		meta.Name, meta.Theme, meta.PiecesExpected = hit.Name, hit.Theme, hit.Pieces
	}
	if p, _ := d.LatestPrice("SET", setNum, 0, "U"); p != nil && !p.Missing && p.Avg > 0 {
		meta.BrickLinkPrice = fmt.Sprintf("%.2f %s", p.Avg, p.Currency)
	}
	if c, cerr := d.LastCheck(setNum); cerr == nil && c != nil {
		meta.HasCounts, meta.EnteredInCLI = true, true
		if proofs, _ := d.ProofsFor(c.ID); len(proofs) > 0 {
			last := proofs[len(proofs)-1]
			meta.ProofFiled, meta.ProofRef = true, filepath.Base(last.FilePath)
		}
		for _, l := range c.Lines {
			lines = append(lines, StockSheetLine{PartNum: l.PartNum, PartName: l.PartName, Category: l.Category, ColorName: l.ColorName, Expected: l.Need, Optional: l.Optional})
			minifig := isMinifigCategory(l.Category)
			if l.Extra > 0 {
				meta.ExtraLines = append(meta.ExtraLines, StockSheetLine{PartNum: l.PartNum, PartName: l.PartName, Category: l.Category, ColorName: l.ColorName, Expected: l.Extra})
			}
			if m := l.Missing(); m > 0 {
				meta.MissingLines = append(meta.MissingLines, StockSheetLine{PartNum: l.PartNum, PartName: l.PartName, Category: l.Category, ColorName: l.ColorName, Expected: m})
				if minifig {
					meta.MinifigsMissing += m
				} else {
					meta.PartsMissing += m
				}
			}
			got := min(l.Have, l.Need)
			if minifig {
				meta.MinifigsGot += got
			} else {
				meta.PartsGot += got
			}
		}
		return title, lines, meta, nil
	}
	inv := d.LookupSetInventory(ctx, rb, setNum)
	if len(inv.Items) == 0 {
		return title, nil, meta, fmt.Errorf("no parts list for set %s", setNum)
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
	return title, lines, meta, nil
}

var stockSheetTmpl = template.Must(template.New("stocksheet").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>Stock check — {{.Title}}</title>
<style>
 body{font:13px/1.35 system-ui,sans-serif;margin:1.2rem;color:#111}
 .head{display:flex;justify-content:space-between;align-items:flex-end;border-bottom:3px solid #222;padding-bottom:.6rem;margin-bottom:1rem}
 h1{margin:0;font-size:20px} .meta{color:#555;font-size:12px}
 h2.section{font-size:12px;letter-spacing:.04em;text-transform:uppercase;background:#1d2a44;color:#fff;margin:1.1rem 0 .4rem;padding:.3rem .5rem}
 .grid{display:grid;grid-template-columns:repeat(3,1fr);gap:.6rem 1.4rem;margin-bottom:.8rem}
 .grid.cond{grid-template-columns:repeat(4,1fr)}
 .field label{display:block;font-size:10px;color:#666;text-transform:uppercase;letter-spacing:.03em;margin-bottom:.15rem}
 .field .v{border-bottom:1px solid #333;min-height:1.3em;font-size:13px}
 .box span{display:inline-block;width:.9em;height:.9em;border:1.3px solid #333;vertical-align:-1px;margin-right:.4em}
 .box.on span{background:#1d2a44;border-color:#1d2a44}
 .counts{display:flex;gap:1.6rem;flex-wrap:wrap;margin-bottom:.4rem}
 .counts .c{min-width:7em} .counts .n{font-size:18px;font-weight:600}
 .who{display:flex;gap:2rem;margin:.6rem 0;font-size:12px;flex-wrap:wrap}
 .who .field{border-bottom:1px solid #333;min-width:12rem;display:inline-block;height:1.2em}
 table{border-collapse:collapse;width:100%} th,td{border-bottom:1px solid #ccc;padding:.35rem .5rem;text-align:left}
 th{background:#eee} td.n,th.n{text-align:right;width:5em}
 td.box{width:2.4rem} .opt{color:#888;font-weight:normal}
 tr{page-break-inside:avoid}
 .sign{margin-top:1rem;font-size:11px;color:#555}
 .foot{margin-top:1.5rem;font-size:11px;color:#888;text-align:center}
 @media print{body{margin:0}}
</style></head><body>
<div class="head"><h1>{{.Meta.SetNum}} — {{.Title}}</h1><div class="meta">{{.Lines}} line(s), {{.Total}} piece(s) expected</div></div>

<div class="grid">
 <div class="field"><label>Theme</label><div class="v">{{.Meta.Theme}}</div></div>
 <div class="field"><label>Pieces</label><div class="v">{{if .Meta.PiecesExpected}}{{.Meta.PiecesExpected}}{{end}}</div></div>
 <div class="field"><label>Storage / Box ID</label><div class="v">{{.Meta.Storage}}</div></div>
</div>

<h2 class="section">Condition</h2>
<div class="grid cond">
{{range .CondOptions}}<div class="box{{if eq . $.Meta.Condition}} on{{end}}"><span></span>{{.}}</div>
{{end}}</div>

<h2 class="section">Cost</h2>
<div class="grid">
 <div class="field"><label>LEGO RRP</label><div class="v"></div></div>
 <div class="field"><label>BrickLink (avg)</label><div class="v">{{.Meta.BrickLinkPrice}}</div></div>
</div>

<h2 class="section">Counts</h2>
<div class="counts">
 <div class="c"><label>Parts got</label><div class="n">{{if .Meta.HasCounts}}{{.Meta.PartsGot}}{{end}}</div></div>
 <div class="c"><label>Parts missing</label><div class="n">{{if .Meta.HasCounts}}{{.Meta.PartsMissing}}{{end}}</div></div>
 <div class="c"><label>Minifigs got</label><div class="n">{{if .Meta.HasCounts}}{{.Meta.MinifigsGot}}{{end}}</div></div>
 <div class="c"><label>Minifigs missing</label><div class="n">{{if .Meta.HasCounts}}{{.Meta.MinifigsMissing}}{{end}}</div></div>
</div>

{{if .Meta.MissingLines}}<h2 class="section">Missing parts — to order</h2>
<table><tr><th>Part</th><th>Colour</th><th>Name</th><th class="n">Qty</th></tr>
{{range .Meta.MissingLines}}<tr><td>{{.PartNum}}</td><td>{{.ColorName}}</td><td>{{.PartName}}</td><td class="n">{{.Expected}}</td></tr>
{{end}}</table>{{end}}

{{if .Meta.ExtraLines}}<h2 class="section">Extra parts</h2>
<table><tr><th>Part</th><th>Colour</th><th>Name</th><th class="n">Qty</th></tr>
{{range .Meta.ExtraLines}}<tr><td>{{.PartNum}}</td><td>{{.ColorName}}</td><td>{{.PartName}}</td><td class="n">{{.Expected}}</td></tr>
{{end}}</table>{{end}}

<h2 class="section">Parts check</h2>
<div class="who">Checked by <span class="field"></span> &nbsp; Date <span class="field"></span></div>
<table><tr><th class="box">✓</th><th>Part</th><th>Colour</th><th>Name</th><th class="n">Expected</th><th class="n">Counted</th></tr>
{{range .Lines}}<tr><td class="box"><span></span></td><td>{{.PartNum}}</td><td>{{.ColorName}}</td><td>{{.PartName}}{{if .Optional}} <span class="opt">(optional)</span>{{end}}</td><td class="n">{{.Expected}}</td><td class="n"></td></tr>
{{end}}</table>

<h2 class="section">Sign off</h2>
<div class="who">Checked by (print name) <span class="field"></span> &nbsp; Signature <span class="field"></span> &nbsp; Date <span class="field"></span></div>
<div class="sign">
 <div class="box{{if .Meta.EnteredInCLI}} on{{end}}"><span></span>Entered in KC-LEGO-CLI</div>
 <div class="box{{if .Meta.ProofFiled}} on{{end}}" style="margin-top:.3rem"><span></span>Proof photo/scan filed{{if .Meta.ProofRef}} — ref {{.Meta.ProofRef}}{{else}} — ref ____________________{{end}}</div>
</div>

<div class="foot">Print at 100% · KC-PARTS</div>
</body></html>`))

// StockSheetHTML renders the checklist.
func StockSheetHTML(title string, lines []StockSheetLine, meta StockSheetMeta) ([]byte, error) {
	total := 0
	for _, l := range lines {
		if !l.Optional {
			total += l.Expected
		}
	}
	var b bytes.Buffer
	err := stockSheetTmpl.Execute(&b, struct {
		Title       string
		Lines       []StockSheetLine
		Total       int
		Meta        StockSheetMeta
		CondOptions []string
	}{title, lines, total, meta, []string{"Sealed", "Opened — complete", "Opened — partial check", "Completely checked"}})
	return b.Bytes(), err
}
