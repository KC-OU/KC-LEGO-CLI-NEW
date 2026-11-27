package lego

import (
	"bytes"
	"html/template"
)

// CountHTML is the count sheet: the same set, grouped the same way, but laid out to be counted rather than ticked. One
// part per row on a portrait page; you write how many you GOT and the sheet works out missing and extra itself
// (got below the need is missing, above it is extra). Totals say how many parts are needed, got, missing and extra.
// It saves its state in the same form as the sorting sheet, so a saved copy can be uploaded as the paper check.
func CountHTML(d *ExportData) ([]byte, error) {
	groups, total := sheetGroups(d)
	var b bytes.Buffer
	err := countTmpl.Execute(&b, map[string]any{"Title": d.Title, "Groups": groups, "Total": total, "Lines": len(d.Rows), "Colours": len(groups), "Date": d.When.Format("2006-01-02"),
		"CheckerID": idDigits(d.CheckerID), "Borrowed": d.Borrowed, "Blank": make([]int, 8), "Logo": logoSrc, "SetNum": d.SetNum})
	return b.Bytes(), err
}

var countTmpl = template.Must(template.New("count").Funcs(template.FuncMap{"imgsrc": imgSrc, "short": shortName}).Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Count sheet — {{.Title}}</title>
<style>
 :root{--navy:#1c2745;--ink:#1a2233;--muted:#5d6678;--line:#d5dae4;--band:#e8ebf2;--zebra:#f5f7fa;--amber:#f0a202}
 *{box-sizing:border-box}
 html{-webkit-print-color-adjust:exact;print-color-adjust:exact}
 body{margin:0;font:12px/1.3 Inter,"Segoe UI",system-ui,-apple-system,Arial,sans-serif;color:var(--ink);background:#fff}
 .page{max-width:900px;margin:0 auto;padding:0 0 1rem}
 .band{display:flex;align-items:center;gap:.8rem;background:var(--navy);color:#fff;padding:.6rem 1rem;border-bottom:4px solid var(--amber)}
 .logo{flex:none;width:44px;height:44px;border-radius:7px;background:#fff;padding:2px;object-fit:contain}
 .band h1{margin:0;font-size:17px;font-weight:600;line-height:1.15}
 .band p{margin:.15rem 0 0;font-size:10.5px;color:#c4cbe0}
 .band .pill{margin-left:auto;background:#fff;color:var(--navy);border:0;border-radius:999px;padding:.25rem .7rem;font:inherit;font-size:11px;font-weight:600;text-align:center;width:7.6rem}
 .sign{display:flex;align-items:center;gap:1.2rem;margin:.5rem 1rem .2rem;font-size:10.5px;color:var(--muted)}
 .hid{width:5.2rem;border:0;border-bottom:1px solid #8b93a5;background:transparent;font:inherit;font-weight:600;color:var(--ink);padding:0 .2rem;border-radius:0}
 .np{font:inherit;color:var(--muted);background:none;border:1px solid var(--line);border-radius:3px;padding:.1rem .6rem;cursor:pointer}
 .np[data-arm]{background:#fdecea;border-color:#c62828;color:#c62828;font-weight:600}
 .sum{display:flex;flex-wrap:wrap;gap:.4rem;margin:.4rem 1rem .2rem}
 .sum div{flex:1 1 90px;border:1.5px solid var(--navy);border-radius:6px;padding:.3rem .5rem;text-align:center}
 .sum b{display:block;font-size:19px;line-height:1.1;font-variant-numeric:tabular-nums}
 .sum span{font-size:9.5px;color:var(--muted);text-transform:uppercase;letter-spacing:.05em}
 .sum .m b{color:#c62828}.sum .x b{color:#1d6fb8}.sum .g b{color:#1a7f37}
 .help{margin:.2rem 1rem .4rem;font-size:10px;color:var(--muted)}
 .help b{color:var(--ink)}
 .cols{padding:0 1rem}
 section.cs{margin:.5rem 0 0}
 .bar{display:flex;align-items:center;gap:.5rem;background:var(--navy);color:#fff;padding:.25rem .6rem;font-size:12px;break-after:avoid}
 .sw{flex:none;width:12px;height:12px;border-radius:50%;border:1.5px solid #fff;background:#888}
 .bar .n{margin-left:auto;color:#c4cbe0;font-size:10.5px;white-space:pre;font-variant-numeric:tabular-nums}
 table{border-collapse:collapse;width:100%;table-layout:fixed}
 th{background:var(--band);font-weight:500;font-size:9.5px;color:var(--muted);text-align:center;padding:.2rem .3rem}
 th.l{text-align:left}
 td{padding:.18rem .3rem;border-bottom:1px solid var(--line);vertical-align:middle;overflow-wrap:anywhere}
 tbody tr:nth-child(even) td{background:var(--zebra)}
 tr{break-inside:avoid}
 tr.sp td{padding:0;border:0;height:5mm;background:#fff}
 @media screen{tr.sp{display:none}}
 .ph div{width:40px;height:30px;margin:auto;background:#fff;border:1px solid var(--line);border-radius:2px;display:grid;place-items:center;overflow:hidden}
 .ph img{max-width:38px;max-height:28px;object-fit:contain;display:block}
 .pn{font-weight:700;font-size:12.5px} .nm{font-weight:600;font-size:10.5px;line-height:1.2} .mt{color:var(--muted);font-size:9.5px}
 .qn{width:100%;border:1px solid var(--line);border-radius:4px;background:#fff;font:inherit;font-weight:700;font-size:14px;padding:.15rem 0;cursor:pointer;color:var(--ink)}
 .qn:hover{border-color:var(--navy)}
 .gi{width:100%;border:0;border-bottom:2px solid #8b93a5;background:transparent;font:inherit;font-weight:700;font-size:15px;text-align:center;padding:.1rem 0;border-radius:0;color:var(--ink)}
 .gi:focus{outline:2px solid #8aa0d6;background:#fff}
 .mo,.xo{text-align:center;font-weight:700;font-size:13px;font-variant-numeric:tabular-nums}
 .mo{color:#c62828}.xo{color:#1d6fb8}
 .uq{width:22px;height:22px;border:1.5px solid #b26a00;border-radius:50%;background:#fff;color:#b26a00;font:700 12px/1 Inter,Arial,sans-serif;padding:0;cursor:pointer;opacity:.45}
 .uq[data-on]{opacity:1;background:#fff4dc}
 .bg{width:100%;border:0;border-bottom:1px solid #8b93a5;background:transparent;font:inherit;font-size:10.5px;color:#a9b0c0;padding:0;cursor:pointer;text-align:center}
 .bg[data-b]{color:var(--ink);font-weight:700}
 tr[data-s="1"] .st::after{content:"\2713";color:#1a7f37;font-weight:700}
 tr[data-s="2"] .st::after{content:"\2717";color:#c62828;font-weight:700}
 tr[data-s="2"] td{background:#fdecea} tr[data-s="3"] td{background:#fff4dc}
 .st{text-align:center;width:22px}
 .borrow,.notes{margin:.7rem 1rem 0;break-inside:avoid}
 .borrow td{height:1.6rem} .borrow td.q{text-align:left;font-weight:700}
 .bi{width:100%;border:0;border-bottom:1px solid #8b93a5;background:transparent;font:inherit;padding:0;border-radius:0}
 .borrow small{color:var(--muted);font-size:9px}
 .ack{display:flex;align-items:flex-start;gap:.5rem;margin:.5rem 0 0;padding:.4rem .6rem;border:1.5px solid var(--navy);font-size:11px;font-weight:600}
 .ack input{width:16px;height:16px;margin:0;flex:none;accent-color:var(--navy)}
 .notes textarea{display:block;width:100%;min-height:8rem;border:1px solid var(--line);border-top:0;padding:.2rem .5rem;font:inherit;font-size:11.5px;line-height:1.6rem;resize:vertical;color:var(--ink);background:repeating-linear-gradient(transparent,transparent calc(1.6rem - 1px),#c5cbd9 calc(1.6rem - 1px),#c5cbd9 1.6rem)}
 .foot{margin:.8rem 1rem 0;padding-top:.3rem;border-top:1px solid var(--line);display:flex;justify-content:space-between;font-size:10px;color:var(--muted)}
 @page{size:A4 landscape;margin:5mm 5mm 10mm;@bottom-left{content:"KC-Parts · Count sheet · {{.Title}}";font:9px Inter,Arial,sans-serif;color:#5d6678}@bottom-right{content:"page " counter(page) " of " counter(pages);font:9px Inter,Arial,sans-serif;color:#5d6678}}
 @media print{html,body{width:100%}.page{max-width:none;padding:0 6mm}.foot,.np{display:none}.sign,.sum,.help,.borrow,.notes{margin-left:0;margin-right:0}.cols{column-count:2;column-gap:8mm;column-rule:1px solid #d5dae4}section.cs{margin:0 0 .4rem}.qn{border-color:#c9cfdc}.sum b{font-size:15px}.sum div{padding:.15rem .4rem}.help{margin-top:.1rem;margin-bottom:.2rem}td{padding:.1rem .25rem}.pn{font-size:11.5px}.gi{font-size:13px}.qn{font-size:12.5px}.ph div{height:26px}.ph img{max-height:24px}}
</style></head><body><div class="page" data-set="{{.SetNum}}">
<header class="band"><img class="logo" alt="KC-Parts" src="{{.Logo}}"><div><h1>{{.Title}}</h1><p>Count sheet · {{.Lines}} part lines · {{.Total}} pieces · {{.Colours}} colours · Use only your username ID number, and never your name</p></div><input class="pill" id="hd" value="{{.Date}}" aria-label="Date" size="10"></header>
<div class="sign"><span>Counted by (ID) <input class="hid" id="hs" inputmode="numeric" maxlength="6" value="{{.CheckerID}}"></span><span>Checked by (ID) <input class="hid" id="hc" inputmode="numeric" maxlength="6" value="{{.CheckerID}}"></span><span style="margin-left:auto"></span><button type="button" class="np" id="save" title="Download a copy with every count, ID and note saved in it, to upload as your paper check">Save filled sheet</button><button type="button" class="np" id="clear">Clear counts</button></div>
<div class="sum"><div><b id="s-need">0</b><span>Parts needed</span></div><div class="g"><b id="s-got">0</b><span>Got</span></div><div class="m"><b id="s-miss">0</b><span>Missing</span></div><div class="x"><b id="s-extra">0</b><span>Extra</span></div><div><b id="s-todo">0</b><span>Lines to do</span></div></div>
<p class="help">Write how many you <b>got</b> on each line. Fewer than needed is <b>missing</b>, more is <b>extra</b>; the sheet works both out. Tap the <b>need</b> number to fill in "all there". <b>?</b> = not sure, ask (still to do). <b>bag</b>: tap for 1 big or 2 small.</p>
<div class="cols">
{{range .Groups}}<section class="cs"><table><colgroup><col style="width:42px"><col><col style="width:42px"><col style="width:48px"><col style="width:32px"><col style="width:34px"><col style="width:24px"><col style="width:46px"><col style="width:18px"></colgroup>
<thead><tr class="sp"><td colspan="9"></td></tr><tr><th colspan="9" style="padding:0"><div class="bar">{{if .Swatch}}<span class="sw" style="background:{{.Swatch}}"></span>{{else}}<span class="sw"></span>{{end}}<span>{{.Colour}} — {{.Pieces}} piece(s)</span><span class="n gt"></span></div></th></tr>
<tr><th>Photo</th><th class="l">Part</th><th>Need</th><th>Got</th><th>Miss</th><th>Extra</th><th>?</th><th>Bag</th><th></th></tr></thead><tbody>
{{range .Rows}}<tr data-p="{{.PartNum}}" data-c="{{.ColorID}}" data-n="{{.Qty}}"><td class="ph"><div>{{with .Image}}<img alt="" src="{{imgsrc .}}" onerror="this.style.visibility='hidden'">{{end}}</div></td><td><span class="pn">{{.PartNum}}</span> <span class="nm">{{short .Name}}</span><div class="mt">{{if .Category}}{{.Category}} · {{end}}element {{if .Element}}{{.Element}}{{else}}—{{end}}</div></td><td><button type="button" class="qn" title="Tap: all {{.Qty}} are here">{{.Qty}}</button></td><td><input class="gi" inputmode="numeric" maxlength="4" aria-label="Got"></td><td class="mo"></td><td class="xo"></td><td style="text-align:center"><button type="button" class="uq" aria-label="Not sure, ask">?</button></td><td><button type="button" class="bg" aria-label="Bag: tap for 1 big or 2 small">bag</button></td><td class="st"></td></tr>
{{end}}</tbody><tfoot><tr class="sp"><td colspan="9"></td></tr></tfoot></table></section>{{end}}
</div>
<section class="borrow"><div class="bar"><span>Borrowed parts — taken from another set to finish building this one</span><span class="n">{{len .Borrowed}} recorded</span></div>
<table><thead><tr><th class="l">Part needed</th><th class="l">Colour</th><th class="l">Qty</th><th class="l">Borrowed from set</th><th class="l">From part (if different)</th><th class="l">Note</th></tr></thead><tbody>
{{range .Borrowed}}<tr data-rec="{{.Qty}}"><td class="pn">{{.NeedPartNum}}</td><td>{{.ColorName}}</td><td class="q">{{.Qty}}×</td><td>{{.FromSetNum}}{{if .FromSetName}} <small>{{.FromSetName}}</small>{{end}}</td><td>{{.FromPartNum}}</td><td>{{.Note}}{{if .Status}} <small>{{.Status}}</small>{{end}}</td></tr>
{{end}}{{range .Blank}}<tr><td><input class="bi" aria-label="Part needed"></td><td><input class="bi" aria-label="Colour"></td><td><input class="bi" inputmode="numeric" aria-label="Quantity"></td><td><input class="bi" aria-label="Borrowed from set"></td><td><input class="bi" aria-label="From part"></td><td><input class="bi" aria-label="Note"></td></tr>
{{end}}</tbody></table>
<label class="ack"><input type="checkbox" id="ack"><span>Borrowed parts have been added to the system straight away after this check. This is a priority and must be done now.</span></label></section>
<section class="notes"><div class="bar"><span>Notes</span><span class="n">anything the admin or the next person should know</span></div><textarea id="notes" rows="6" aria-label="Notes" placeholder="Damaged parts, wrong colours, things to ask..."></textarea></section>
<div class="foot"><span>KC-Parts · Count sheet · {{.Title}}</span><span>Print at 100% on A4 landscape (two columns)</span></div>
<script>
(function(){
 var rows=[].slice.call(document.querySelectorAll("tr[data-p]")),key="count1:"+document.title+":"+rows.length,saved={},
  hs=document.getElementById("hs"),hc=document.getElementById("hc"),hd=document.getElementById("hd"),ack=document.getElementById("ack"),
  notes=document.getElementById("notes"),bis=[].slice.call(document.querySelectorAll(".bi")),page=document.querySelector(".page");
 try{saved=JSON.parse(localStorage.getItem(key)||"{}")}catch(e){}
 var emb=document.getElementById("kc-state");if(emb){try{saved=JSON.parse(emb.textContent)}catch(e){}}
 function digits(el,n){var v=el.value.replace(/\D/g,"").slice(0,n);if(v!==el.value)el.value=v}
 function cells(r){return{g:r.querySelector(".gi"),q:r.querySelector(".qn"),u:r.querySelector(".uq"),b:r.querySelector(".bg"),m:r.querySelector(".mo"),x:r.querySelector(".xo")}}
 function need(r){return parseInt(r.dataset.n,10)||0}
 // One line: s = 0 to do, 1 all there (or more), 2 short, 3 not sure. got is capped at the need; the rest is extra.
 function calc(r){var c=cells(r),n=need(r);if(c.g.value==="")return{s:c.u.hasAttribute("data-on")?3:0,got:0,miss:0,extra:0};
  var g=+c.g.value;return{s:g>=n?1:2,got:Math.min(g,n),miss:Math.max(0,n-g),extra:Math.max(0,g-n)}}
 function tl(rs){var o={need:0,got:0,miss:0,extra:0,todo:0};rs.forEach(function(r){var k=calc(r);o.need+=need(r);o.got+=k.got;o.miss+=k.miss;o.extra+=k.extra;if(k.s===0||k.s===3)o.todo++});return o}
 function paint(r){var c=cells(r),k=calc(r);c.m.textContent=k.miss||"";c.x.textContent=k.extra||"";if(k.s)r.dataset.s=k.s;else delete r.dataset.s}
 function setBag(c,v){v=v||"";var N={"":"bag","1":"1 big","2":"2 small"};if(v)c.b.dataset.b=v;else c.b.removeAttribute("data-b");c.b.textContent=N[v]||"bag"}
 function collect(){var rec=0,br=[];[].slice.call(document.querySelectorAll("tr[data-rec]")).forEach(function(r){rec+=+r.dataset.rec||0});
  for(var i=0;i<bis.length;i+=6)br.push(bis.slice(i,i+6).map(function(e){return e.value}));
  return{set:page.dataset.set||"",h:{s:hs.value,c:hc.value,d:hd.value},ack:ack.checked?1:0,rec:rec,borrowed:br,notes:notes.value,
   lines:rows.map(function(r){var c=cells(r),k=calc(r);return{p:r.dataset.p,c:+r.dataset.c,s:k.s,m:k.miss?String(k.miss):"",x:k.extra?String(k.extra):"",i:c.b.dataset.b||"",d:"",g:c.g.value,u:c.u.hasAttribute("data-on")?1:0}})}}
 function sum(){
  [].slice.call(document.querySelectorAll("section.cs")).forEach(function(sec){var o=tl([].slice.call(sec.querySelectorAll("tr[data-p]")));
   sec.querySelector(".gt").textContent="got "+o.got+"/"+o.need+"  ·  missing "+o.miss+"  ·  extra "+o.extra+"  ·  "+o.todo+" to do"});
  rows.forEach(paint);var o=tl(rows);
  document.getElementById("s-need").textContent=o.need;document.getElementById("s-got").textContent=o.got;document.getElementById("s-miss").textContent=o.miss;
  document.getElementById("s-extra").textContent=o.extra;document.getElementById("s-todo").textContent=o.todo;
  try{localStorage.setItem(key,JSON.stringify(collect()))}catch(e){}}
 var h=saved.h;if(h){if(h.s)hs.value=h.s;if(h.c)hc.value=h.c;if(h.d)hd.value=h.d}
 if(saved.notes)notes.value=saved.notes;
 if(saved.lines||saved.ack||saved.borrowed){ack.checked=!!saved.ack;(saved.borrowed||[]).forEach(function(row,i){row.forEach(function(v,j){if(bis[i*6+j])bis[i*6+j].value=v})})}
 rows.forEach(function(r,i){var c=cells(r),v=(saved.lines||[])[i];
  if(v&&v.p===r.dataset.p){c.g.value=v.g||"";if(v.u&&c.g.value==="")c.u.setAttribute("data-on","1");setBag(c,v.i)}
  c.g.addEventListener("input",function(){digits(c.g,4);c.u.removeAttribute("data-on");sum()});
  c.q.addEventListener("click",function(){c.g.value=String(need(r));c.u.removeAttribute("data-on");sum()});
  c.u.addEventListener("click",function(){if(c.u.hasAttribute("data-on")){c.u.removeAttribute("data-on")}else{c.g.value="";c.u.setAttribute("data-on","1")}sum()});
  c.b.addEventListener("click",function(){setBag(c,{"":"1","1":"2","2":""}[c.b.dataset.b||""]);sum()})});
 [hs,hc].forEach(function(e){e.addEventListener("input",function(){digits(e,6);sum()})});hd.addEventListener("input",sum);ack.addEventListener("change",sum);notes.addEventListener("input",sum);bis.forEach(function(e){e.addEventListener("input",sum)});
 var cb=document.getElementById("clear"),timer;
 function disarm(){clearTimeout(timer);cb.textContent="Clear counts";cb.removeAttribute("data-arm")}
 cb.addEventListener("click",function(){
  if(!cb.hasAttribute("data-arm")){cb.setAttribute("data-arm","1");cb.textContent="Tap again to clear";timer=setTimeout(disarm,4000);return}
  disarm();rows.forEach(function(r){var c=cells(r);c.g.value="";c.u.removeAttribute("data-on");setBag(c,"")});sum()});
 document.getElementById("save").addEventListener("click",function(){
  sum();var out=document.documentElement.cloneNode(true),old=out.querySelector("#kc-state");if(old)old.remove();
  var sc=document.createElement("script");sc.type="application/json";sc.id="kc-state";sc.textContent=JSON.stringify(collect()).replace(/</g,"\\u003c");
  out.querySelector("body script").before(sc);
  var a=document.createElement("a");a.href=URL.createObjectURL(new Blob(["<!doctype html>\n"+out.outerHTML],{type:"text/html"}));
  a.download="count-sheet-"+(page.dataset.set||"set")+"-filled.html";document.body.appendChild(a);a.click();a.remove()});
 sum();
})();
</script>
</div></body></html>`))
