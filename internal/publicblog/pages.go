package publicblog

// One template set, light and friendly: white page on a pale-blue ground, amber accent kept from the
// dashboard brand, large photos, readable 68-character post column. Status chips carry a text label
// as well as a colour.
const pages = `
{{define "head"}}<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Title}}</title>{{if .Desc}}<meta name="description" content="{{.Desc}}">{{end}}
<meta property="og:site_name" content="{{.Site}}"><meta property="og:title" content="{{.Title}}">{{if .Desc}}<meta property="og:description" content="{{.Desc}}">{{end}}{{if .Image}}<meta property="og:image" content="{{.Base}}/media/{{.Image}}"><meta name="twitter:card" content="summary_large_image">{{else}}<meta property="og:image" content="{{.Base}}/logo.png">{{end}}
<meta name="theme-color" content="#E39B00">
<link rel="alternate" type="application/rss+xml" href="/feed.xml" title="{{.Site}}">
<link rel="icon" type="image/png" href="/logo.png">
<style>
:root{--bg:#EEF3F8;--card:#fff;--ink:#16202B;--mute:#566474;--line:#D9E2EC;--amber:#E39B00;--amber2:#FFC83D;--blue:#0B5FA5;--green:#1E7B4A;--red:#B3261E;--shadow:0 1px 2px rgba(22,32,43,.06),0 8px 24px rgba(22,32,43,.06);--hero1:#FFF3CC;--hero2:#DCEBFA}
@media(prefers-color-scheme:dark){:root{--bg:#0F1720;--card:#17212C;--ink:#E8EEF4;--mute:#9AA9B8;--line:#273445;--blue:#6DB3F2;--green:#5BD08F;--red:#FF8A80;--shadow:0 1px 2px rgba(0,0,0,.4),0 8px 24px rgba(0,0,0,.3);--hero1:#2A2410;--hero2:#14283D}.waiting{color:#FFD76A!important;background:#3A2E0A!important;border-color:#6B5513!important}.building{color:#8CC7FF!important;background:#10304D!important;border-color:#24527C!important}.counting{color:#C9B3FF!important;background:#2A2044!important;border-color:#4B3A7C!important}.checked{color:#7EE2A8!important;background:#0F3322!important;border-color:#1F5A3C!important}.preview{background:#3A2E0A!important;color:#FFD76A}}
*{box-sizing:border-box}html{scroll-behavior:smooth}body{margin:0;background:var(--bg);color:var(--ink);font:17px/1.65 ui-rounded,"Segoe UI Variable","Segoe UI",system-ui,-apple-system,Roboto,sans-serif;-webkit-font-smoothing:antialiased}
a{color:var(--blue)}a:focus-visible,button:focus-visible,input:focus-visible,summary:focus-visible{outline:3px solid var(--amber);outline-offset:2px}
.skip{position:absolute;left:-999px}.skip:focus{left:8px;top:8px;background:var(--card);padding:8px 12px;z-index:9;border-radius:8px}
.top{background:var(--card);border-bottom:4px solid var(--amber);position:sticky;top:0;z-index:5}
.wrap{max-width:1040px;margin:0 auto;padding:0 16px}
.top .wrap{display:flex;gap:16px;align-items:center;justify-content:space-between;flex-wrap:wrap;padding-top:10px;padding-bottom:10px}
.brand{display:flex;align-items:center;gap:10px;font-weight:800;font-size:21px;color:var(--ink);text-decoration:none;letter-spacing:-.01em}
.logo{width:40px;height:40px;object-fit:contain}
.nav{display:flex;gap:18px;margin-left:auto}.nav a{color:var(--ink);text-decoration:none;font-weight:600;font-size:15px}.nav a:hover{color:var(--blue)}
form.s{display:flex;gap:8px}form.s input{padding:9px 14px;border:1px solid var(--line);border-radius:99px;font:inherit;width:210px;background:var(--bg);color:var(--ink)}
form.s button{padding:9px 16px;border:0;border-radius:99px;background:var(--ink);color:var(--bg);font:inherit;font-weight:600;cursor:pointer}
main{padding:0 0 56px}h1{font-size:clamp(28px,5vw,40px);line-height:1.12;margin:.2em 0 .3em;letter-spacing:-.025em}h2{font-size:23px;margin:1.6em 0 .4em;letter-spacing:-.01em}h3{font-size:18px;margin:.9em 0 .2em}
.lede{color:var(--mute);margin:0 0 24px}
.hero{background:radial-gradient(circle at 12px 12px,rgba(227,155,0,.18) 5px,transparent 6px) 0 0/48px 48px,linear-gradient(135deg,var(--hero1),var(--hero2));border-bottom:1px solid var(--line);padding:44px 0 30px;margin-bottom:28px}
.hero h1{font-size:clamp(32px,6vw,52px);margin:0 0 .15em}.hero p{max-width:56ch;color:var(--mute);font-size:19px;margin:0 0 22px}
.stats{display:flex;gap:12px;flex-wrap:wrap}.stat{background:var(--card);border:1px solid var(--line);border-radius:14px;padding:10px 18px;box-shadow:var(--shadow);min-width:120px}.stat b{display:block;font-size:26px;line-height:1.1;letter-spacing:-.02em}.stat span{color:var(--mute);font-size:14px}
.pad{padding-top:28px}
.grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(270px,1fr));gap:18px}
.card{background:var(--card);border:1px solid var(--line);border-radius:16px;overflow:hidden;text-decoration:none;color:var(--ink);display:flex;flex-direction:column;box-shadow:var(--shadow);transition:transform .15s,border-color .15s}
.card:hover{border-color:var(--amber);transform:translateY(-3px)}
.thumb{position:relative;aspect-ratio:16/10;background:radial-gradient(circle at 14px 14px,rgba(227,155,0,.35) 6px,transparent 7px) 0 0/40px 40px,linear-gradient(135deg,var(--hero1),var(--hero2))}.thumb img{width:100%;height:100%;object-fit:cover;display:block}
.thumb .chip{position:absolute;left:10px;top:10px;backdrop-filter:blur(6px)}
.body{padding:14px 16px 16px;display:flex;flex-direction:column;gap:2px}.body small{color:var(--mute);font-size:14px}.body b{font-size:19px;line-height:1.25}.body span{color:var(--mute);font-size:15px}
.themes{display:flex;flex-wrap:wrap;gap:8px;margin:0 0 26px}.themes a{background:var(--card);border:1px solid var(--line);border-radius:99px;padding:5px 15px;text-decoration:none;color:var(--ink);font-size:15px;font-weight:500}.themes a:hover{border-color:var(--amber);background:var(--hero1)}
.chip{display:inline-block;border-radius:99px;padding:2px 12px;font-size:14px;font-weight:600;border:1px solid;text-transform:capitalize}
.waiting{color:#7A5200;border-color:#E6C46B;background:#FFF6DC}.building{color:var(--blue);border-color:#9CC4E8;background:#E8F2FB}
.counting{color:#5A3A9C;border-color:#C6B5EA;background:#F1ECFB}.checked{color:var(--green);border-color:#9AD2B3;background:#E7F6EE}
.crumb{padding-top:22px;margin:0;color:var(--mute);font-size:15px}
.steps{display:flex;gap:0;list-style:none;margin:6px 0 22px;padding:0}.steps li{flex:1;text-align:center;font-size:14px;color:var(--mute);position:relative;padding-top:26px;text-transform:capitalize}
.steps li:before{content:"";position:absolute;left:50%;top:6px;width:14px;height:14px;margin-left:-7px;border-radius:50%;background:var(--line);z-index:1}
.steps li:after{content:"";position:absolute;left:-50%;top:12px;width:100%;height:3px;background:var(--line)}.steps li:first-child:after{display:none}
.steps li.done:before,.steps li.done:after,.steps li.on:after{background:var(--green)}.steps li.on:before{background:var(--amber);box-shadow:0 0 0 5px rgba(227,155,0,.25)}.steps li.on{color:var(--ink);font-weight:700}.steps li.done{color:var(--green)}
.layout{display:grid;grid-template-columns:minmax(0,1fr) 300px;gap:28px;align-items:start}
@media(max-width:820px){.layout{grid-template-columns:1fr}}
article{background:var(--card);border:1px solid var(--line);border-radius:16px;padding:8px 30px 30px;max-width:760px;box-shadow:var(--shadow)}
article p,article li{max-width:68ch}article img{max-width:100%;border-radius:10px}article blockquote{margin:1em 0;padding:4px 16px;border-left:4px solid var(--amber);color:var(--mute)}
article code{background:var(--bg);padding:1px 5px;border-radius:4px}
aside{position:sticky;top:76px}aside .box{background:var(--card);border:1px solid var(--line);border-radius:16px;padding:6px 18px 14px;margin-bottom:16px;box-shadow:var(--shadow)}
dl{margin:0}dl div{display:flex;justify-content:space-between;gap:12px;padding:9px 0;border-bottom:1px solid var(--line)}dl div:last-child{border:0}dt{color:var(--mute)}dd{margin:0;font-weight:600;text-align:right}
table.check{border-collapse:collapse;margin:8px 0}table.check th,table.check td{border:1px solid var(--line,#d5dae4);padding:6px 12px;text-align:left}table.check th{font-weight:600}.muted{color:#6b7690;font-size:.9em}
.verdict{display:flex;gap:10px;align-items:center;border-radius:12px;padding:10px 14px;margin:12px 0 4px;font-weight:700}.verdict.ok{background:#E7F6EE;color:var(--green)}.verdict.bad{background:#FDECEA;color:var(--red)}
@media(prefers-color-scheme:dark){.verdict.ok{background:#0F3322}.verdict.bad{background:#3A1512}}
.photos{display:grid;grid-template-columns:repeat(auto-fill,minmax(180px,1fr));gap:12px}.photos figure{margin:0}.photos img{width:100%;aspect-ratio:4/3;object-fit:cover;border-radius:10px;display:block;transition:transform .15s}.photos a:hover img{transform:scale(1.02)}.photos figcaption{font-size:14px;color:var(--mute);padding-top:4px}
.preview{background:#FFF1C7;border:1px solid var(--amber);border-radius:10px;padding:10px 14px;margin:18px 0 0;font-weight:600}
.ok{color:var(--green)}.bad{color:var(--red)}audio{width:100%}iframe{width:100%;aspect-ratio:16/9;border:0;border-radius:10px}
.video{display:block;text-align:center;border:2px dashed var(--line);border-radius:12px;padding:26px 16px;text-decoration:none;color:var(--ink)}.video small{display:block;color:var(--mute)}
.btn{display:inline-block;background:var(--amber);color:#1B1300;font-weight:700;border-radius:99px;padding:8px 18px;margin-top:8px}
footer{color:var(--mute);font-size:14px;padding:24px 0 40px;border-top:1px solid var(--line)}footer a{color:var(--mute)}
@media(prefers-reduced-motion:reduce){*{transition:none!important;scroll-behavior:auto!important}}
@media print{.top,footer,form.s,aside .box:last-child{display:none}body{background:#fff}}
</style></head><body><a class="skip" href="#main">Skip to content</a>
<header class="top"><div class="wrap"><a class="brand" href="/"><img class="logo" src="/logo.png" alt="" width="40" height="40">{{.Site}}</a>
<nav class="nav" aria-label="Site"><a href="/about">How it works</a>{{if .Dash}}<a href="{{.Dash}}/blog" rel="nofollow noopener">Staff sign in</a>{{end}}</nav><form class="s" action="/search" role="search"><input name="q" type="search" placeholder="Set number or name" aria-label="Search sets" maxlength="60"><button>Search</button></form></div></header>
<main id="main">{{end}}
{{define "foot"}}</main><footer><div class="wrap">Photos and notes are our own. Set names and numbers belong to the LEGO Group, which is not involved with this site. <a href="/about">How it works</a> &middot; <a href="/privacy">Privacy</a>{{if .Dash}} &middot; <a href="{{.Dash}}/blog" rel="nofollow noopener">Staff sign in</a>{{end}} &middot; <a href="/feed.xml">RSS</a></div></footer></body></html>{{end}}

{{define "cards"}}<div class="grid">{{range .}}<a class="card" href="/set/{{.Slug}}"><div class="thumb">{{if .Cover}}<img loading="lazy" src="/media/{{.Cover}}" alt="">{{end}}<span class="chip {{.Status}}">{{.Status}}</span></div><div class="body"><small>{{.SetNum}}{{if .Theme}} &middot; {{.Theme}}{{end}}</small><b>{{.Title}}</b>{{if .Check}}<span class="{{if eq .Check "All parts present"}}ok{{else}}bad{{end}}">{{.Check}}</span>{{else}}<span>{{.Summary}}</span>{{end}}</div></a>{{else}}<p class="lede">Nothing here yet.</p>{{end}}</div>{{end}}

{{define "index"}}{{template "head" (dict2 . "Title" (printf "%s" .Site) "Desc" .Tagline)}}
<section class="hero"><div class="wrap"><h1>{{.Site}}</h1><p>{{.Tagline}}</p>
<div class="stats"><div class="stat"><b>{{.Stats.Sets}}</b><span>sets built</span></div>{{if .Stats.Parts}}<div class="stat"><b>{{.Stats.Parts}}</b><span>parts counted</span></div>{{end}}<div class="stat"><b>{{.Stats.Complete}}</b><span>with every part</span></div>{{if .Stats.Themes}}<div class="stat"><b>{{.Stats.Themes}}</b><span>themes</span></div>{{end}}</div></div></section>
<div class="wrap">{{if .Themes}}<nav class="themes" aria-label="Themes">{{range .Themes}}<a href="/theme/{{esc .}}">{{.}}</a>{{end}}</nav>{{end}}
<h2 style="margin-top:0">Latest sets</h2>{{template "cards" .Posts}}</div>{{template "foot"}}{{end}}

{{define "theme"}}{{template "head" (dict2 . "Title" (printf "%s - %s" .Theme .Site) "Desc" "")}}
<div class="wrap pad"><p class="lede"><a href="/">All sets</a></p><h1>{{.Theme}}</h1>{{template "cards" .Posts}}</div>{{template "foot"}}{{end}}

{{define "search"}}{{template "head" (dict2 . "Title" (printf "Search - %s" .Site) "Desc" "")}}
<div class="wrap pad"><h1>Search</h1>{{if .Q}}<p class="lede">Results for "{{.Q}}"</p>{{template "cards" .Posts}}{{else}}<p class="lede">Type a set number or name.</p>{{end}}</div>{{template "foot"}}{{end}}

{{define "notfound"}}{{template "head" (dict2 . "Title" "Not found" "Desc" "")}}<div class="wrap pad"><h1>Page not found</h1><p class="lede">That set is not published. <a href="/">See all sets</a>.</p></div>{{template "foot"}}{{end}}

{{define "about"}}{{template "head" (dict2 . "Title" (printf "How it works - %s" .Site) "Desc" "How each set on this site is built, checked, written up and published.")}}
<div class="wrap pad"><article><h1>How this site works</h1>
<p>{{.Site}} is a write-up of the LEGO sets we receive. Before a set goes on the shelf we build it, count it and photograph it, and this site shows how that went.</p>
<h2>From box to post</h2>
<ol>
<li><strong>Queued.</strong> A set is added for checking and a private draft post is created automatically with its name, theme and year.</li>
<li><strong>Built.</strong> The checker builds the set before counting anything, and photographs each numbered bag and the finished model.</li>
<li><strong>Counted.</strong> Every part is checked against the instructions. The result is recorded as either "All parts present" or the number of parts missing.</li>
<li><strong>Written.</strong> Notes are written in Markdown, in our own words with short quotes from the instructions where useful, and with a link back to the source.</li>
<li><strong>Reviewed.</strong> An admin reads the post first, using a private preview link. Nothing goes live until an admin approves it.</li>
<li><strong>Published.</strong> The post appears here, in the feed and in search. An admin can hide a post again at any time.</li>
</ol>
<h2>What the status means</h2>
<p><span class="chip waiting">waiting</span> queued, not started. <span class="chip building">building</span> being built and photographed. <span class="chip counting">counting</span> built, parts being counted. <span class="chip checked">checked</span> finished.</p>
<h2>The parts check</h2>
<p>We show the totals: parts checked, how many are missing and how many were borrowed from another of our sets to finish this one, plus our paper check sheet. Prices, stock locations, supplier details, notes and the people who did the check are never shown on this site.</p>
<h2>Photos, text and sound</h2>
<p>Photos are ours. Some posts have our own audio notes or a video; a video from another site loads only if you choose to play it. Set names and numbers belong to the LEGO Group, which is not involved with this site, and each post links to LEGO.com, Rebrickable and BrickLink for the official details.</p>
<h2>Following along</h2>
<p>Use the search box for a set number or name, browse by theme, or subscribe to the <a href="/feed.xml">RSS feed</a>. There are no accounts, comments or tracking. See <a href="/privacy">privacy</a>.</p>
<h2>Staff</h2>
<p>Posts are written and approved in our dashboard, not on this site. This site has no login of its own and cannot change anything. {{if .Dash}}Staff sign in at the <a href="{{.Dash}}/blog" rel="nofollow noopener">dashboard</a>, which uses the same protected sign-in as the rest of our systems. Access is granted per person by an admin.{{end}}</p>
</article></div>{{template "foot"}}{{end}}

{{define "privacy"}}{{template "head" (dict2 . "Title" (printf "Privacy - %s" .Site) "Desc" "")}}
<div class="wrap pad"><article><h1>Privacy</h1>
<p>This site does not use cookies, trackers, analytics, adverts or third-party fonts. Nothing you do here is recorded against you.</p>
<h2>What we keep about visitors</h2><p>Nothing. Your address is held in memory for a minute only, to stop one visitor flooding the site, and is never written down. Searches are not stored.</p>
<h2>Other sites</h2><p>Pictures on this site are our own and are loaded from here. A video from YouTube loads only after you choose to play it, and then YouTube sees your address. Links to LEGO.com, Rebrickable and BrickLink leave this site.</p>
<h2>Photos</h2><p>Camera details such as location and time are removed from every photo before it is shown.</p>
<h2>People</h2><p>Posts show no names. Our own system keeps only the name and ID of the staff member who wrote or approved a post, and only for our records.</p>
<h2>Your rights</h2><p>To ask about, correct or remove anything about you, contact the site owner and it will be dealt with promptly.</p></article></div>{{template "foot"}}{{end}}

{{define "post"}}{{with .V}}{{template "head" (dict2 $ "Title" (printf "%s - %s" .P.Title $.Site) "Desc" .P.Summary "Image" .Cover)}}
<div class="wrap">{{if .Preview}}<div class="preview">Preview. This post is {{.P.State}} and is not public yet.</div>{{end}}
<p class="crumb"><a href="/">All sets</a>{{if .P.Theme}} / <a href="/theme/{{esc .P.Theme}}">{{.P.Theme}}</a>{{end}}</p>
<h1>{{.P.Title}}</h1>
<ol class="steps" aria-label="Build progress">{{range .Steps}}<li class="{{if .Done}}done{{end}}{{if .Active}} on{{end}}">{{.Name}}</li>{{end}}</ol>
<div class="layout"><article>{{.Body}}
{{if .Snap}}<h2>The parts check</h2>
<table class="check"><tr><th>Parts checked</th><th>Missing</th><th>Borrowed</th><th>Extra</th></tr><tr><td>{{.Snap.Pieces}}</td><td>{{.Snap.Missing}}</td><td>{{.Snap.BorrowedTotal}}</td><td>{{.Snap.Extra}}</td></tr></table>
{{if .Snap.Date}}<p class="muted">Checked on {{.Snap.Date}}.</p>{{end}}
{{if .Snap.Borrowed}}<h3>Borrowed parts</h3><p>Taken from these sets to finish this one:</p><ul>{{range .Snap.Borrowed}}<li>{{.Qty}} x {{.Part}}{{if .Colour}} ({{.Colour}}){{end}} from set {{.From}}{{if .Name}}, {{.Name}}{{end}}</li>{{end}}</ul>{{end}}
{{$t := .P.Token}}{{$pv := .Preview}}{{$slug := .P.Slug}}{{if .SheetImgs}}<h3>Our paper check</h3><div class="photos">{{range .SheetImgs}}<figure><a href="/sheet/{{$slug}}/{{.}}{{if $pv}}?t={{$t}}{{end}}"><img loading="lazy" src="/sheet/{{$slug}}/{{.}}{{if $pv}}?t={{$t}}{{end}}" alt="Paper check sheet, page {{inc .}}"></a></figure>{{end}}</div>{{end}}
{{if .Marks}}<details><summary>Our paper check, line by line</summary><table class="check"><tr><th>Part</th><th>Colour</th><th></th><th>Missing</th><th>Extra</th></tr>{{range .Marks}}<tr><td>{{.Part}}{{if .Name}} <small>{{.Name}}</small>{{end}}</td><td>{{.Colour}}</td><td>{{if eq .Mark "tick"}}&#10003;{{else if eq .Mark "cross"}}&#10007;{{else if eq .Mark "question"}}?{{end}}</td><td>{{.Miss}}</td><td>{{.Extra}}</td></tr>{{end}}</table></details>{{end}}{{end}}
{{if or .Bags .Instr}}<h2>Building</h2>{{if .Bags}}<p>This set has {{.Bags}} numbered bag{{if ne .Bags 1}}s{{end}}; we photographed each one after it was built, and the finished set.</p>{{end}}{{if .Instr}}<p><a href="{{.Instr}}" rel="noopener nofollow">Build instructions from LEGO</a></p>{{end}}{{end}}
{{if .Photos}}<h2>Build photos</h2><div class="photos">{{$t := .P.Token}}{{$pv := .Preview}}{{range .Photos}}<figure><a href="/media/{{.ID}}{{if $pv}}?t={{$t}}{{end}}"><img loading="lazy" src="/media/{{.ID}}{{if $pv}}?t={{$t}}{{end}}" alt="{{.Caption}}"></a><figcaption>{{.Caption}}</figcaption></figure>{{end}}</div>{{end}}
{{if .AudioFile}}<h2>Listen</h2><audio controls preload="none" src="{{if hasPrefix .AudioFile "https://"}}{{.AudioFile}}{{else}}/audio/{{.AudioFile}}{{end}}"></audio>{{end}}
{{if .AudioEmbed}}<h2>Watch</h2>{{if .LoadVideo}}<iframe src="{{.AudioEmbed}}" title="Video" allowfullscreen></iframe>{{else}}<a class="video" href="?video=1{{if .Preview}}#watch{{end}}" rel="nofollow"><span class="btn">Play video</span><small>Loads from YouTube, which will see your address.</small></a>{{end}}{{end}}
</article>
<aside><div class="box"><h3>Set facts</h3>
{{if .Check}}<div class="verdict {{if eq .Check "All parts present"}}ok{{else}}bad{{end}}">{{.Check}}</div>{{end}}<dl>
<div><dt>Set</dt><dd>{{.P.SetNum}}</dd></div>
{{if .Year}}<div><dt>Released</dt><dd>{{.Year}}</dd></div>{{end}}
{{if .Pieces}}<div><dt>Parts</dt><dd>{{.Pieces}}</dd></div>{{end}}
{{if .P.Theme}}<div><dt>Theme</dt><dd>{{.P.Theme}}</dd></div>{{end}}
<div><dt>Status</dt><dd><span class="chip {{.Status}}">{{.Status}}</span></dd></div>
</dl></div>
{{if .Links}}<div class="box"><h3>Elsewhere</h3>{{range .Links}}<div><a href="{{index . 1}}" rel="noopener nofollow">{{index . 0}}</a></div>{{end}}</div>{{end}}</aside></div></div>
{{template "foot"}}{{end}}{{end}}
`
