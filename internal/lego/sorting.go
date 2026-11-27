package lego

import (
	"bytes"
	_ "embed"
	"encoding/base64"
	"html/template"
	"regexp"
	"sort"
	"strings"
)

// logoPNG is the KC-Parts logo, shown in the sheet's header band (embedded so a saved or printed copy keeps it).
//
//go:embed logo.png
var logoPNG []byte

var logoSrc = template.URL("data:image/png;base64," + base64.StdEncoding.EncodeToString(logoPNG))

// SortingHTML is a set's parts grouped by colour, then category, with pictures,
// counts and tick boxes: a sheet for sorting a set's parts or building it.
func SortingHTML(d *ExportData) ([]byte, error) {
	groups, total := sheetGroups(d)
	var b bytes.Buffer
	err := sortingTmpl.Execute(&b, map[string]any{"Title": d.Title, "Groups": groups, "Total": total, "Lines": len(d.Rows), "Colours": len(groups), "Date": d.When.Format("2006-01-02"),
		"CheckerID": idDigits(d.CheckerID), "Borrowed": d.Borrowed, "Blank": make([]int, 8), "Logo": logoSrc, "SetNum": d.SetNum})
	return b.Bytes(), err
}

// sheetGroup is one colour of a set's parts, in the order the sheets list them.
type sheetGroup struct {
	Colour string
	Swatch template.CSS // the colour as a CSS value, "" when unknown
	Pieces int
	Rows   []ExportRow
}

// sheetGroups groups a set's parts by colour, then category and part number, and totals the pieces.
func sheetGroups(d *ExportData) ([]sheetGroup, int) {
	byColour := map[string]*sheetGroup{}
	var order []string
	for _, r := range d.Rows {
		c := r.ColorName
		if c == "" {
			c = "(no colour)"
		}
		g := byColour[c]
		if g == nil {
			g = &sheetGroup{Colour: c}
			if hexRGB.MatchString(r.ColorRGB) {
				g.Swatch = template.CSS("#" + r.ColorRGB)
			}
			byColour[c] = g
			order = append(order, c)
		}
		g.Rows = append(g.Rows, r)
		g.Pieces += r.Qty
	}
	sort.Strings(order)
	var groups []sheetGroup
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
	return groups, total
}

// WorkerID returns s when it is a numeric worker ID (digits only, at most six) and "" otherwise, so a name or
// a username never lands on a sheet.
func WorkerID(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 6 || idDigits(s) != s {
		return ""
	}
	return s
}

var bracketedRE = regexp.MustCompile(`\s*[\(\[][^\)\]]*[\)\]]`)

// shortName drops the (bracketed) and [square] remarks from a part name ("(Closed Ends)", "[Complete Assembly]"):
// they only make a printed line long and untidy.
func shortName(n string) string {
	out := strings.Join(strings.Fields(bracketedRE.ReplaceAllString(n, "")), " ")
	if out == "" {
		return n
	}
	return out
}

// idDigits keeps only the digits of a worker ID (at most 6): the sheet carries ID numbers, never names.
func idDigits(s string) string {
	out := make([]byte, 0, 6)
	for i := 0; i < len(s) && len(out) < 6; i++ {
		if s[i] >= '0' && s[i] <= '9' {
			out = append(out, s[i])
		}
	}
	return string(out)
}

var sortingTmpl = template.Must(template.New("sort").Funcs(template.FuncMap{"imgsrc": imgSrc, "short": shortName}).Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Sorting sheet — {{.Title}}</title>
<style>
 :root{--navy:#1c2745;--ink:#1a2233;--muted:#5d6678;--line:#d5dae4;--band:#e8ebf2;--zebra:#f5f7fa;--red:#d01012}
 *{box-sizing:border-box}
 html{-webkit-print-color-adjust:exact;print-color-adjust:exact}
 body{margin:0;font:11px/1.3 Inter,"Segoe UI",system-ui,-apple-system,Arial,sans-serif;color:var(--ink);background:#fff}
 .page{max-width:1100px;margin:0 auto;padding:0 0 1rem}
 .band{display:flex;align-items:center;gap:.7rem;background:var(--navy);color:#fff;padding:.5rem .9rem}
 .logo{flex:none;width:38px;height:38px;border-radius:6px;background:#fff;padding:2px;object-fit:contain}
 .band h1{margin:0;font-size:16px;font-weight:600;line-height:1.15}
 .band p{margin:.1rem 0 0;font-size:10.5px;color:#c4cbe0}
 .band .pill{margin-left:auto;background:#fff;color:var(--navy);border:0;border-radius:999px;padding:.2rem .7rem;font:inherit;font-size:11px;font-weight:600;text-align:center;width:7.6rem}
 .sign{display:flex;align-items:center;gap:1.2rem;margin:.4rem .9rem .1rem;font-size:10px;color:var(--muted)}
 .hid{width:5.2rem;border:0;border-bottom:1px solid #8b93a5;background:transparent;font:inherit;font-weight:600;color:var(--ink);padding:0 .2rem;border-radius:0}
 .hid.d{width:6.4rem}
 .sid{width:34px;padding-left:.15rem;padding-right:.15rem} .dt{width:44px;padding-left:.15rem;padding-right:.15rem}
 .si{width:100%;border:0;border-bottom:1px solid #8b93a5;background:transparent;font:inherit;font-size:9.5px;text-align:center;padding:0;margin:0;border-radius:0;color:var(--ink)}
 .si:focus,.hid:focus,.bi:focus{outline:2px solid #8aa0d6;background:#fff}
 .np[data-arm]{background:#fdecea;border-color:#c62828;color:#c62828;font-weight:600}
 .borrow{margin:.6rem .9rem 0;break-inside:avoid}
 .borrow td{height:1.5rem} .borrow td.q{text-align:left}
 .bi{width:100%;border:0;border-bottom:1px solid #8b93a5;background:transparent;font:inherit;padding:0;border-radius:0}
 .borrow small{color:var(--muted);font-size:9px}
 .notes{margin:.6rem .9rem 0;break-inside:avoid}
 .notes textarea{display:block;width:100%;min-height:7.5rem;border:1px solid var(--line);border-top:0;padding:.2rem .5rem;font:inherit;font-size:11px;line-height:1.5rem;resize:vertical;color:var(--ink);background:repeating-linear-gradient(transparent,transparent calc(1.5rem - 1px),#c5cbd9 calc(1.5rem - 1px),#c5cbd9 1.5rem)}
 .ack{display:flex;align-items:flex-start;gap:.5rem;margin:.5rem 0 0;padding:.4rem .6rem;border:1.5px solid var(--navy);font-size:11px;font-weight:600;color:var(--ink)}
 .ack input{width:16px;height:16px;margin:0;flex:none;accent-color:var(--navy)}
 .key{display:flex;flex-wrap:wrap;gap:.2rem .9rem;align-items:center;margin:.35rem .9rem 0;padding:.25rem .5rem;border:1px solid var(--line);border-radius:4px;background:var(--zebra);font-size:9.5px;color:var(--muted)}
 .key b{color:var(--ink)}
 .kb{display:inline-grid;place-items:center;width:13px;height:13px;border:1.5px solid var(--navy);border-radius:2px;background:#fff;font:700 10px/1 Arial,sans-serif;font-style:normal;vertical-align:middle;margin-right:.2rem}
 .kb.t{color:#1a7f37}.kb.c{color:#c62828}.kb.kq{color:#b26a00}
 .cols{padding:0 .9rem}
 section{margin:.45rem 0 0}
 .bar{display:flex;align-items:center;gap:.45rem;background:var(--navy);color:#fff;padding:.22rem .5rem;font-size:11.5px;break-after:avoid}
 .sw{flex:none;width:11px;height:11px;border-radius:50%;border:1.5px solid #fff;background:#888}
 .bar .n{margin-left:auto;color:#c4cbe0;font-size:10px;display:flex;gap:.9rem;white-space:pre}
 .bar .ct{color:#fff}
 tr.barrow th{padding:0;background:var(--navy);font-weight:400}
 tr.sp td{padding:0;border:0;height:4.5mm;background:#fff}
 @media screen{tr.sp{display:none}}
 table{border-collapse:collapse;width:100%;table-layout:fixed}
 th{background:var(--band);font-weight:500;font-size:9.5px;color:var(--muted);text-align:left;padding:.18rem .3rem}
 td{padding:.1rem .3rem;border-bottom:1px solid var(--line);vertical-align:middle;overflow-wrap:anywhere}
 tbody tr:nth-child(even) td{background:var(--zebra)}
 tr{break-inside:avoid}
 .box{width:22px;text-align:center;padding-left:.2rem;padding-right:.1rem}
 .mk{display:inline-grid;place-items:center;width:13px;height:13px;padding:0;border:1.5px solid var(--navy);border-radius:2px;background:#fff;font:700 12px/1 Inter,Arial,sans-serif;cursor:pointer;vertical-align:middle}
 .mk[data-s="1"]::after{content:"\2713";color:#1a7f37} .mk[data-s="2"]::after{content:"\2717";color:#c62828} .mk[data-s="3"]::after{content:"?";color:#b26a00}
 tr:has(.mk[data-s="2"]) td{background:#fdecea} tr:has(.mk[data-s="3"]) td{background:#fff4dc}
 .me{width:58px;white-space:nowrap;text-align:center} th.me{text-align:center}
 .mi{width:28px;border:0;border-bottom:1px solid #8b93a5;background:transparent;font:inherit;font-weight:600;text-align:center;padding:0;margin:0 1px;border-radius:0}
 .mi:focus{outline:2px solid #8aa0d6;background:#fff}
 .tally{margin-left:auto;font-weight:600;color:var(--ink);white-space:pre} .np{font:inherit;color:var(--muted);background:none;border:1px solid var(--line);border-radius:3px;padding:0 .5rem;cursor:pointer}
 .ph{width:38px;padding:0 .3rem} .ph div{width:34px;height:24px;background:#fff;border:1px solid var(--line);border-radius:2px;display:grid;place-items:center;overflow:hidden}
 .ph img{max-width:32px;max-height:22px;object-fit:contain;display:block}
 .q{width:38px;text-align:right;font-weight:700;font-size:12px;white-space:nowrap} th.q{font-size:9.5px;font-weight:500}
 .c-pn{width:76px}
 .pn{font-weight:600;overflow-wrap:anywhere} .el{font-variant-numeric:tabular-nums;color:var(--muted);white-space:nowrap;padding-right:.45rem}
 .hl{font-weight:400;color:var(--muted)}
 .main{padding:.12rem .4rem}
 .l1{display:flex;gap:.4rem;align-items:baseline}
 .l1 .q{min-width:2.2em;text-align:right;font-size:11px;flex:none}.l1 .pn{font-size:11px;min-width:5.4em;flex:none}
 .dsc{font-weight:600;font-size:10px;line-height:1.2;min-width:0;overflow-wrap:anywhere}
 .l2{display:flex;justify-content:space-between;align-items:flex-end;gap:.5rem;margin-top:.12rem}
 .meta{color:var(--muted);font-size:9px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
 .fl{display:flex;gap:.3rem;flex:none}
 .fl input{width:28px;border:0;border-bottom:1px solid #8b93a5;background:transparent;font:inherit;font-size:10px;text-align:center;padding:0;border-radius:0;color:var(--ink)}
 .fl input.dtf{width:44px}
 .bg{width:46px;border:0;border-bottom:1px solid #8b93a5;background:transparent;font:inherit;font-size:10px;color:#a9b0c0;padding:0;cursor:pointer;text-align:center}
 .bg[data-b="1"]{color:var(--ink);font-weight:700}.bg[data-b="2"]{color:var(--ink);font-weight:700}.fl input::placeholder{color:#a9b0c0;font-size:8.5px}
 .fl input:focus{outline:2px solid #8aa0d6;background:#fff}
 td.ds{font-weight:600;line-height:1.25;overflow-wrap:anywhere}
 td.ds small{display:block;font-weight:400;color:var(--muted);font-size:9px;line-height:1.2}
 td.pn small{display:block;font-weight:400;font-size:9px;color:var(--muted);letter-spacing:.02em}
 .foot{margin:.8rem .9rem 0;padding-top:.3rem;border-top:1px solid var(--line);display:flex;justify-content:space-between;font-size:10px;color:var(--muted)}
 @page{size:A4 landscape;margin:5mm 5mm 10mm;@bottom-left{content:"KC-Parts · Sorting sheet · {{.Title}}";font:9px Inter,Arial,sans-serif;color:#5d6678}@bottom-right{content:"page " counter(page) " of " counter(pages);font:9px Inter,Arial,sans-serif;color:#5d6678}}
 @media print{html,body{width:100%}.page{max-width:none;padding:0 6mm}.foot,.np{display:none}.cols{padding:0;column-count:2;column-gap:8mm;column-rule:1px solid #d5dae4}.sign,.borrow,.notes,.key{margin-left:0;margin-right:0}section{margin-top:0;margin-bottom:.4rem}.borrow{margin-top:.5rem}.hid,.si,.bi{border-bottom-color:#8b93a5}}
</style></head><body><div class="page" data-set="{{.SetNum}}">
<header class="band"><img class="logo" alt="KC-Parts" src="{{.Logo}}"><div><h1>{{.Title}}</h1><p>Sorting sheet · {{.Lines}} part lines · {{.Total}} pieces · {{.Colours}} colours · by colour, then category · Use only your username ID number, and never your name</p></div><input class="pill" id="hd" value="{{.Date}}" aria-label="Date" size="10"></header>
<div class="sign"><span>Sorted by (ID) <input class="hid" id="hs" inputmode="numeric" maxlength="6" value="{{.CheckerID}}"></span><span>Checked by (ID) <input class="hid" id="hc" inputmode="numeric" maxlength="6" value="{{.CheckerID}}"></span><span class="tally" id="tally"></span><button type="button" class="np" id="save" title="Download a copy with every mark, ID and date saved in it, to upload as your paper check">Save filled sheet</button><button type="button" class="np" id="clear">Clear marks</button></div>
<div class="key"><b>Key</b>
<span><i class="kb"></i> not checked yet</span><span><i class="kb t">&#10003;</i> all there</span><span><i class="kb c">&#10007;</i> missing or wrong</span><span><i class="kb kq">?</i> not sure, ask</span>
<span><b>miss</b> how many are missing</span><span><b>extra</b> spare ones found</span><span><b>bag</b> tap: 1 big, 2 small</span><span><b>date</b> day checked</span></div>
<div class="cols">
{{range .Groups}}<section class="cs"><table><colgroup><col style="width:26px"><col style="width:40px"><col></colgroup><thead><tr class="sp"><td colspan="3"></td></tr><tr class="barrow"><th colspan="3"><div class="bar">{{if .Swatch}}<span class="sw" style="background:{{.Swatch}}"></span>{{else}}<span class="sw"></span>{{end}}<span>{{.Colour}} — {{.Pieces}} piece(s)</span><span class="n"><span class="ct">✓ 0  ✗ 0  ? 0  ·  missing 0  extra 0</span><span class="left" data-total="{{len .Rows}}">{{len .Rows}} to do</span></span></div></th></tr>
<tr class="colh"><th class="box"></th><th class="ph">Photo</th><th>Qty · part · description</th></tr></thead><tbody>
{{range .Rows}}<tr data-p="{{.PartNum}}" data-c="{{.ColorID}}"><td class="box"><button type="button" class="mk" aria-label="Tick, cross or question mark"></button></td><td class="ph"><div>{{with .Image}}<img alt="" src="{{imgsrc .}}" onerror="this.style.visibility='hidden'">{{end}}</div></td><td class="main"><div class="l1"><b class="q">{{.Qty}}×</b><b class="pn">{{.PartNum}}</b><span class="dsc">{{short .Name}}</span></div><div class="l2"><span class="meta">{{if .Category}}{{.Category}} · {{end}}element {{if .Element}}{{.Element}}{{else}}—{{end}}</span><span class="fl"><input class="mi" inputmode="numeric" placeholder="miss" aria-label="Missing"><input class="mi" inputmode="numeric" placeholder="extra" aria-label="Extra"><button type="button" class="bg" aria-label="Bag: tap for 1 big or 2 small">bag</button><input class="si dtf" placeholder="date" aria-label="Date"></span></div></td></tr>
{{end}}</tbody><tfoot><tr class="sp"><td colspan="3"></td></tr></tfoot></table></section>{{end}}
</div>
<section class="borrow"><div class="bar"><span>Borrowed parts — taken from another set to finish building this one</span><span class="n">{{len .Borrowed}} recorded</span></div>
<table><thead><tr><th class="c-pn">Part needed</th><th>Colour</th><th class="q">Qty</th><th>Borrowed from set</th><th>From part (if different)</th><th>Note</th></tr></thead><tbody>
{{range .Borrowed}}<tr data-rec="{{.Qty}}"><td class="pn">{{.NeedPartNum}}</td><td>{{.ColorName}}</td><td class="q">{{.Qty}}×</td><td>{{.FromSetNum}}{{if .FromSetName}} <small>{{.FromSetName}}</small>{{end}}</td><td>{{.FromPartNum}}</td><td>{{.Note}}{{if .Status}} <small>{{.Status}}</small>{{end}}</td></tr>
{{end}}{{range .Blank}}<tr><td><input class="bi" aria-label="Part needed"></td><td><input class="bi" aria-label="Colour"></td><td><input class="bi" inputmode="numeric" aria-label="Quantity"></td><td><input class="bi" aria-label="Borrowed from set"></td><td><input class="bi" aria-label="From part"></td><td><input class="bi" aria-label="Note"></td></tr>
{{end}}</tbody></table>
<label class="ack"><input type="checkbox" id="ack"><span>Borrowed parts have been added to the system straight away after this check. This is a priority and must be done now.</span></label></section>
<section class="notes"><div class="bar"><span>Notes</span><span class="n">anything the admin or the next person should know</span></div><textarea id="notes" rows="6" aria-label="Notes" placeholder="Damaged parts, wrong colours, things to ask..."></textarea></section>
<div class="foot"><span>KC-Parts · Sorting sheet · {{.Title}}</span><span>Print at 100% on A4 landscape (two columns)</span></div>
<script>
(function(){
 var rows=[].slice.call(document.querySelectorAll("tbody tr")).filter(function(r){return r.querySelector(".mk")}),
  key="sort2:"+document.title+":"+rows.length,tally=document.getElementById("tally"),saved={},hs=document.getElementById("hs"),hc=document.getElementById("hc"),hd=document.getElementById("hd"),
  ack=document.getElementById("ack"),bis=[].slice.call(document.querySelectorAll(".bi")),page=document.querySelector(".page");
 var notes=document.getElementById("notes");
 try{saved=JSON.parse(localStorage.getItem(key)||"{}")}catch(e){}
 var emb=document.getElementById("kc-state");if(emb){try{saved=JSON.parse(emb.textContent)}catch(e){}}
 function digits(el,n){var v=el.value.replace(/\D/g,"").slice(0,n);if(v!==el.value)el.value=v}
 function short(iso){var m=/^(\d{4})-(\d{2})-(\d{2})$/.exec(iso||"");return m?m[3]+"/"+m[2]+"/"+m[1].slice(2):(iso||"")}
 function cells(r){var i=r.querySelectorAll(".mi"),k=r.querySelectorAll(".si");return{b:r.querySelector(".mk"),m:i[0],x:i[1],g:r.querySelector(".bg"),d:k[0]}}
 var BN={"":"bag","1":"1 big","2":"2 small"};function setBag(c,v){v=v||"";if(v)c.g.dataset.b=v;else c.g.removeAttribute("data-b");c.g.textContent=BN[v]||"bag"}
 function stamp(c){if(!c.d.value)c.d.value=short(hd.value)}
 function collect(){var rec=0,br=[];[].slice.call(document.querySelectorAll("tr[data-rec]")).forEach(function(r){rec+=+r.dataset.rec||0});
  for(var i=0;i<bis.length;i+=6)br.push(bis.slice(i,i+6).map(function(e){return e.value}));
  return{set:page.dataset.set||"",h:{s:hs.value,c:hc.value,d:hd.value},ack:ack.checked?1:0,rec:rec,borrowed:br,notes:notes.value,
   lines:rows.map(function(r){var c=cells(r);return{p:r.dataset.p,c:+r.dataset.c,s:+(c.b.dataset.s||0),m:c.m.value,x:c.x.value,i:c.g.dataset.b||"",d:c.d.value}})}}
 function qty(r){var q=r.querySelector(".q");return q?(parseInt(q.textContent,10)||0):0}
 function setState(c,v){if(v)c.b.dataset.s=v;else delete c.b.dataset.s}
 // Counts for some rows: lines by mark, pieces counted (a tick counts the whole line, a cross all but what is missing,
 // an unmarked or ? line none), missing only from crosses (a cross with no number is 1 short), and what is still to do.
 function tl(rs){var c=[0,0,0,0],pc=0,tot=0,ms=0,ex=0;rs.forEach(function(r){var q=cells(r),st=+(q.b.dataset.s||0),n=qty(r);c[st]++;tot+=n;
   if(st===1)pc+=n;else if(st===2){var m=Math.min(n,+q.m.value||1);ms+=m;pc+=n-m}ex+=+q.x.value||0});
  return{c:c,pc:pc,tot:tot,ms:ms,ex:ex,todo:c[0]+c[3]}}
 function txt(t){return"\u2713 "+t.c[1]+"  \u2717 "+t.c[2]+"  ? "+t.c[3]+"  \u00b7  "+"got "+t.pc+"/"+t.tot+"  \u00b7  missing "+t.ms+"  \u00b7  extra "+t.ex}
 function sum(){
  [].slice.call(document.querySelectorAll("section.cs")).forEach(function(sec){var t=tl([].slice.call(sec.querySelectorAll("tbody tr")));
   sec.querySelector(".ct").textContent=txt(t);sec.querySelector(".left").textContent=t.todo+" to do"});
  var t=tl(rows);
  try{localStorage.setItem(key,JSON.stringify(collect()))}catch(e){}
  tally.textContent=txt(t)+"  \u00b7  "+t.todo+" to do"}
 var h=saved.h;if(h){if(h.s)hs.value=h.s;if(h.c)hc.value=h.c;if(h.d)hd.value=h.d}
 if(saved.notes)notes.value=saved.notes;
 if(saved.lines||saved.ack||saved.borrowed){ack.checked=!!saved.ack;(saved.borrowed||[]).forEach(function(row,i){row.forEach(function(v,j){if(bis[i*6+j])bis[i*6+j].value=v})})}
 rows.forEach(function(r,i){var c=cells(r),v=(saved.lines||[])[i];
  if(v&&v.p===r.dataset.p){if(v.s)c.b.dataset.s=v.s;c.m.value=v.m||"";c.x.value=v.x||"";setBag(c,v.i);c.d.value=v.d||""}
  c.b.addEventListener("click",function(){var was=+(c.b.dataset.s||0),v=(was+1)%4;
   if(was===2&&c.m.dataset.auto&&c.m.value==="1")c.m.value="";delete c.m.dataset.auto;
   setState(c,v);if(v===0){c.m.value="";c.x.value=""}if(v===2&&!c.m.value){c.m.value="1";c.m.dataset.auto="1"}if(v)stamp(c);sum()});
  c.m.addEventListener("input",function(){digits(c.m,4);delete c.m.dataset.auto;var v=+(c.b.dataset.s||0);if(c.m.value&&v<2){setState(c,2);stamp(c)}sum()});
  c.x.addEventListener("input",function(){digits(c.x,4);if(c.x.value&&!(+(c.b.dataset.s||0))){setState(c,1);stamp(c)}sum()});
  c.g.addEventListener("click",function(){setBag(c,{"":"1","1":"2","2":""}[c.g.dataset.b||""]);sum()});
  c.d.addEventListener("input",sum);c.d.addEventListener("focus",function(){if(!c.d.value){c.d.value=short(hd.value);sum()}})});
 [hs,hc].forEach(function(e){e.addEventListener("input",function(){digits(e,6);sum()})});hd.addEventListener("input",sum);ack.addEventListener("change",sum);notes.addEventListener("input",sum);bis.forEach(function(e){e.addEventListener("input",sum)});
 var cb=document.getElementById("clear"),timer;
 function disarm(){clearTimeout(timer);cb.textContent="Clear marks";cb.removeAttribute("data-arm")}
 cb.addEventListener("click",function(){
  if(!cb.hasAttribute("data-arm")){cb.setAttribute("data-arm","1");cb.textContent="Tap again to clear";timer=setTimeout(disarm,4000);return}
  disarm();rows.forEach(function(r){var c=cells(r);c.b.removeAttribute("data-s");delete c.m.dataset.auto;c.m.value="";c.x.value="";setBag(c,"");c.d.value=""});sum()});
 document.getElementById("save").addEventListener("click",function(){
  sum();var out=document.documentElement.cloneNode(true),old=out.querySelector("#kc-state");if(old)old.remove();
  var sc=document.createElement("script");sc.type="application/json";sc.id="kc-state";sc.textContent=JSON.stringify(collect()).replace(/</g,"\\u003c");
  out.querySelector("body script").before(sc);
  var a=document.createElement("a");a.href=URL.createObjectURL(new Blob(["<!doctype html>\n"+out.outerHTML],{type:"text/html"}));
  a.download="sorting-sheet-"+(page.dataset.set||"set")+"-filled.html";document.body.appendChild(a);a.click();a.remove()});
 sum();
})();
</script>
</div></body></html>`))
