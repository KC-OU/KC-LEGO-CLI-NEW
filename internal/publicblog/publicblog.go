// Package publicblog is the public sets site (sets.kcparts.xyz): a read-only service that shows
// only posts an admin has published, plus their public photos, the parts-check totals (checked, missing,
// borrowed, and what was borrowed from which set) and the paper sheet as images or a table of its marks.
// Like publicstock it opens lego.db read-only and shares no code path with the dashboard API, so
// exposing it exposes nothing else. Never shown: costs, bin locations, who checked, checker IDs or dates,
// full missing lists, notes, or the build-instruction PDFs (staff only).
package publicblog

import (
	"bytes"
	"database/sql"
	_ "embed"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

func OpenReadOnly(path string) (*sql.DB, error) {
	return sql.Open("sqlite", "file:"+path+"?mode=ro&_pragma=query_only(1)&_pragma=busy_timeout(5000)")
}

type Config struct {
	DB                *sql.DB
	AttachmentsDir    string // where set photos live
	AudioDir          string // the server's own audio files
	BaseURL           string // e.g. https://sets.kcparts.xyz, for feeds and sitemap
	DashboardURL      string // where staff sign in; shown as a link only, this site has no login of its own
	PerMinute         int
	SiteName, Tagline string
}

type post struct {
	Slug, SetNum, Theme, Title, Summary, Body, Audio, Override, State, Token, Published string
}

type photo struct {
	ID      int64
	Caption string
}

// snapView is the parts check as the post shows it: totals and what was borrowed from which set.
type snapView struct {
	Pieces, Have, Missing, Extra, BorrowedTotal int
	Date                                        string
	Borrowed                                    []borRow
}

type borRow struct {
	Part, Colour string
	Qty          int
	From, Name   string
}

// markRow is one marked line of a digital paper sheet (never the IDs or dates written on it).
type markRow struct{ Part, Name, Colour, Mark, Miss, Extra string }

type view struct {
	Snap       *snapView
	SheetImgs  []int
	Marks      []markRow
	Bags       int
	Instr      string // LEGO's own instructions page for the set
	P          post
	Body       template.HTML
	Status     string // waiting / building / counting / checked
	Check      string // public one-line result, "" when not checked yet
	Year       string
	Pieces     string
	Photos     []photo
	AudioFile  string
	AudioEmbed string
	Preview    bool
	Links      [][2]string
	Steps      []step
	Cover      int64
	LoadVideo  bool
}

type step struct {
	Name   string
	Done   bool
	Active bool
}

// card is a post plus what a listing card shows: a cover photo and the status chip.
type card struct {
	post
	Cover  int64
	Status string
	Check  string
}

func (s *site) cards(ps []post) []card {
	out := make([]card, 0, len(ps))
	for _, p := range ps {
		v := s.build(p, false)
		out = append(out, card{post: p, Cover: v.Cover, Status: v.Status, Check: v.Check})
	}
	return out
}

//go:embed logo.png
var logoPNG []byte

const cols = `slug, set_num, theme, title, summary, body, audio, build_status, review_state, preview_token, published_at`

func scan(sc interface{ Scan(...any) error }) (post, error) {
	var p post
	err := sc.Scan(&p.Slug, &p.SetNum, &p.Theme, &p.Title, &p.Summary, &p.Body, &p.Audio, &p.Override, &p.State, &p.Token, &p.Published)
	return p, err
}

var ytRE = regexp.MustCompile(`^https://(?:www\.)?(?:youtube\.com/watch\?v=|youtu\.be/)([A-Za-z0-9_-]{6,20})`)
var audioNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,80}\.(mp3|m4a|ogg|wav)$`)

type site struct {
	Config
	lim *limiter
	t   *template.Template
}

func (s *site) one(q string, args ...any) (post, bool) {
	p, err := scan(s.DB.QueryRow(`SELECT `+cols+` FROM blog_posts WHERE `+q, args...))
	return p, err == nil
}

func (s *site) published(order string, limit int, where string, args ...any) []post {
	q := `SELECT ` + cols + ` FROM blog_posts WHERE review_state='published'`
	if where != "" {
		q += ` AND ` + where
	}
	rows, err := s.DB.Query(q+` ORDER BY `+order+` LIMIT `+strconv.Itoa(limit), args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []post
	for rows.Next() {
		if p, err := scan(rows); err == nil {
			out = append(out, p)
		}
	}
	return out
}

// addSections adds the structured parts of a post (see lego/blog_sections.go): confirmed facts, the pulled
// check, the paper sheet and the bag count. A database from before they existed simply has none.
func (s *site) addSections(v *view, p post) {
	var factsJSON, checkJSON, sheetFiles, sheetPublic string
	var confirmed, bags int
	if s.DB.QueryRow(`SELECT facts_json, facts_confirmed, bags, check_json, sheet_files, sheet_public FROM blog_sections WHERE slug=?`, p.Slug).
		Scan(&factsJSON, &confirmed, &bags, &checkJSON, &sheetFiles, &sheetPublic) != nil {
		return
	}
	v.Bags = bags
	if confirmed != 0 {
		var f struct{ Name, Theme, Year, Parts string }
		if json.Unmarshal([]byte(factsJSON), &f) == nil {
			if f.Year != "" && f.Year != "0" {
				v.Year = f.Year
			}
			if f.Parts != "" && f.Parts != "0" {
				v.Pieces = f.Parts
			}
		}
	}
	var snap struct {
		FinishedAt                   string `json:"finished_at"`
		Pieces, Have, Missing, Extra int
		Borrowed                     []struct {
			Part, Colour string
			Qty          int
			From         string
			FromName     string `json:"from_name"`
		}
	}
	if checkJSON != "" && json.Unmarshal([]byte(checkJSON), &snap) == nil {
		sv := &snapView{Pieces: snap.Pieces, Have: snap.Have, Missing: snap.Missing, Extra: snap.Extra, Date: snap.FinishedAt}
		for _, b := range snap.Borrowed {
			sv.BorrowedTotal += b.Qty
			sv.Borrowed = append(sv.Borrowed, borRow{Part: b.Part, Colour: b.Colour, Qty: b.Qty, From: b.From, Name: b.FromName})
		}
		v.Snap = sv
	}
	var files []struct{ Name, Kind string }
	_ = json.Unmarshal([]byte(sheetFiles), &files)
	imgs := 0
	for _, f := range files {
		if f.Kind == "jpeg" || f.Kind == "png" {
			v.SheetImgs = append(v.SheetImgs, imgs)
			imgs++
		}
	}
	var marks []markRow
	_ = json.Unmarshal([]byte(sheetPublic), &marks)
	v.Marks = marks
	if base := strings.TrimSuffix(p.SetNum, "-1"); base != "" && !strings.Contains(base, "-") {
		v.Instr = "https://www.lego.com/en-gb/service/buildinginstructions/" + url.PathEscape(base)
	}
}

// build derives everything a post page shows that is not the post text itself.
func (s *site) build(p post, preview bool) view {
	v := view{P: p, Body: Markdown(p.Body), Preview: preview, Status: p.Override}
	if p.SetNum != "" {
		var year int
		var parts int
		var theme string
		if s.DB.QueryRow(`SELECT year, parts_qty, theme FROM sets WHERE set_num=?`, p.SetNum).Scan(&year, &parts, &theme) != nil {
			var ys, ps string
			if s.DB.QueryRow(`SELECT year, total_pieces FROM ref_sets WHERE set_num=?`, p.SetNum).Scan(&ys, &ps) == nil {
				v.Year, v.Pieces = ys, ps
			}
		} else {
			if year > 0 {
				v.Year = strconv.Itoa(year)
			}
			if parts > 0 {
				v.Pieces = strconv.Itoa(parts)
			}
		}
		var missing, pieces int
		if s.DB.QueryRow(`SELECT missing, pieces FROM set_checks WHERE set_num=? AND status='done' ORDER BY id DESC LIMIT 1`, p.SetNum).Scan(&missing, &pieces) == nil {
			if v.Status == "" {
				v.Status = "checked"
			}
			if missing == 0 {
				v.Check = "All parts present"
			} else {
				v.Check = fmt.Sprintf("%d part%s missing", missing, map[bool]string{true: "", false: "s"}[missing == 1])
			}
		}
		if v.Status == "" {
			var st string
			if s.DB.QueryRow(`SELECT status FROM job_tickets WHERE kind='check' AND target=? AND status IN ('queued','claimed','done') ORDER BY id DESC LIMIT 1`, p.SetNum).Scan(&st) == nil {
				v.Status = map[string]string{"queued": "waiting", "claimed": "building", "done": "checked"}[st]
			}
		}
		if rows, err := s.DB.Query(`SELECT id, note FROM set_photos WHERE set_num=? AND note LIKE 'blog:%' ORDER BY id`, p.SetNum); err == nil {
			for rows.Next() {
				var ph photo
				var note string
				if rows.Scan(&ph.ID, &note) == nil {
					ph.Caption = strings.TrimSpace(strings.TrimPrefix(note, "blog:"))
					v.Photos = append(v.Photos, ph)
				}
			}
			rows.Close()
		}
		s.addSections(&v, p)
		n := url.QueryEscape(p.SetNum)
		v.Links = [][2]string{
			{"LEGO.com", "https://www.lego.com/en-gb/search?q=" + n},
			{"Rebrickable", "https://rebrickable.com/sets/" + url.PathEscape(p.SetNum) + "/"},
			{"BrickLink", "https://www.bricklink.com/v2/catalog/catalogitem.page?S=" + n},
		}
	}
	if v.Status == "" {
		v.Status = "waiting"
	}
	if len(v.Photos) > 0 {
		v.Cover = v.Photos[len(v.Photos)-1].ID // the newest photo, usually the finished set
	}
	reached := true
	for _, n := range []string{"waiting", "building", "counting", "checked"} {
		v.Steps = append(v.Steps, step{Name: n, Done: reached && n != v.Status, Active: n == v.Status})
		if n == v.Status {
			reached = false
		}
	}
	switch a := p.Audio; {
	case audioNameRE.MatchString(a):
		v.AudioFile = a
	case ytRE.MatchString(a):
		v.AudioEmbed = "https://www.youtube-nocookie.com/embed/" + ytRE.FindStringSubmatch(a)[1]
	case strings.HasPrefix(a, "https://") && audioNameRE.MatchString(filepath.Base(strings.SplitN(a, "?", 2)[0])):
		v.AudioFile = a
	}
	return v
}

type limiter struct {
	mu   sync.Mutex
	max  int
	hits map[string][]time.Time
}

func (l *limiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cut := now.Add(-time.Minute)
	kept := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.max {
		l.hits[key] = kept
		return false
	}
	l.hits[key] = append(kept, now)
	if len(l.hits) > 10000 {
		for k, v := range l.hits {
			if len(v) == 0 || v[len(v)-1].Before(cut) {
				delete(l.hits, k)
			}
		}
	}
	return true
}

func clientKey(r *http.Request) string {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		if cf := r.Header.Get("CF-Connecting-IP"); net.ParseIP(cf) != nil {
			return cf
		}
	}
	return host
}

func (s *site) render(w http.ResponseWriter, name string, data any, noindex bool) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if noindex {
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		w.Header().Set("Cache-Control", "no-store")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=120")
	}
	if err := s.t.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, "page error", http.StatusInternalServerError)
	}
}

func Handler(c Config) http.Handler {
	if c.PerMinute <= 0 {
		c.PerMinute = 120
	}
	if c.SiteName == "" {
		c.SiteName = "KC Sets"
	}
	if c.Tagline == "" {
		c.Tagline = "LEGO sets we have built and checked, bag by bag."
	}
	s := &site{Config: c, lim: &limiter{max: c.PerMinute, hits: map[string][]time.Time{}}, t: template.Must(template.New("").Funcs(template.FuncMap{
		"day": func(s string) string {
			if len(s) >= 10 {
				return s[:10]
			}
			return s
		},
		"esc":       url.PathEscape,
		"hasPrefix": strings.HasPrefix,
		"inc":       func(i int) int { return i + 1 },
		"dict2": func(m map[string]any, kv ...any) map[string]any { // a copy of m with extra keys
			out := map[string]any{}
			for k, v := range m {
				out[k] = v
			}
			for i := 0; i+1 < len(kv); i += 2 {
				out[kv[i].(string)] = kv[i+1]
			}
			return out
		},
	}).Parse(pages))}

	mux := http.NewServeMux()
	nf := func(w http.ResponseWriter) { w.WriteHeader(404); s.render(w, "notfound", s.base(), true) }

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		d := s.base()
		d["Posts"] = s.cards(s.published("published_at DESC", 24, ""))
		d["Stats"] = s.stats()
		d["Themes"] = s.themes()
		s.render(w, "index", d, false)
	})
	mux.HandleFunc("GET /theme/{name}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		posts := s.cards(s.published("title", 200, "theme = ?", name))
		if len(posts) == 0 {
			nf(w)
			return
		}
		d := s.base()
		d["Theme"], d["Posts"] = name, posts
		s.render(w, "theme", d, false)
	})
	mux.HandleFunc("GET /set/{slug}", func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.one(`slug = ? AND review_state='published'`, r.PathValue("slug"))
		if !ok {
			nf(w)
			return
		}
		d := s.base()
		v := s.build(p, false)
		v.LoadVideo = r.URL.Query().Get("video") == "1"
		d["V"] = v
		s.render(w, "post", d, false)
	})
	mux.HandleFunc("GET /preview/{token}", func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.one(`preview_token = ?`, r.PathValue("token"))
		if !ok || len(r.PathValue("token")) < 8 {
			nf(w)
			return
		}
		d := s.base()
		v := s.build(p, true)
		v.LoadVideo = r.URL.Query().Get("video") == "1"
		d["V"] = v
		s.render(w, "post", d, true)
	})
	mux.HandleFunc("GET /media/{id}", s.media)
	mux.HandleFunc("GET /sheet/{slug}/{n}", s.sheetImage)
	mux.HandleFunc("GET /audio/{name}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if !audioNameRE.MatchString(name) || s.AudioDir == "" {
			http.NotFound(w, r)
			return
		}
		f, err := os.OpenInRoot(s.AudioDir, name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		fi, _ := f.Stat()
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeContent(w, r, name, fi.ModTime(), f)
	})
	mux.HandleFunc("GET /search", func(w http.ResponseWriter, r *http.Request) {
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		d := s.base()
		d["Q"] = q
		if len(q) >= 2 && len(q) <= 60 {
			like := "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(q) + "%"
			d["Posts"] = s.cards(s.published("published_at DESC", 50, `(title LIKE ? ESCAPE '\' OR set_num LIKE ? ESCAPE '\' OR theme LIKE ? ESCAPE '\' OR summary LIKE ? ESCAPE '\')`, like, like, like, like))
		}
		s.render(w, "search", d, true)
	})
	mux.HandleFunc("GET /logo.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		_, _ = w.Write(logoPNG)
	})
	mux.HandleFunc("GET /about", func(w http.ResponseWriter, r *http.Request) { s.render(w, "about", s.base(), false) })
	mux.HandleFunc("GET /privacy", func(w http.ResponseWriter, r *http.Request) { s.render(w, "privacy", s.base(), false) })
	mux.HandleFunc("GET /feed.xml", s.feed)
	mux.HandleFunc("GET /sitemap.xml", s.sitemap)
	mux.HandleFunc("GET /robots.txt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "User-agent: *\nAllow: /\nDisallow: /preview/\nDisallow: /search\nSitemap: %s/sitemap.xml\n", s.BaseURL)
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "ok") })

	limited := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.lim.allow(clientKey(r), time.Now()) {
			w.Header().Set("Retry-After", "60")
			http.Error(w, "too many requests", http.StatusTooManyRequests)
			return
		}
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src 'self' data:; media-src 'self' https:; frame-src https://www.youtube-nocookie.com; form-action 'self'; base-uri 'none'")
		mux.ServeHTTP(w, r)
	})
	return http.MaxBytesHandler(limited, 1<<10)
}

func (s *site) base() map[string]any {
	return map[string]any{"Site": s.SiteName, "Tagline": s.Tagline, "Base": s.BaseURL, "Dash": strings.TrimRight(s.DashboardURL, "/")}
}

// stats are the headline numbers on the home page: aggregate counts only.
func (s *site) stats() map[string]int {
	var sets, themes, complete, parts int
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM blog_posts WHERE review_state='published'`).Scan(&sets)
	_ = s.DB.QueryRow(`SELECT COUNT(DISTINCT theme) FROM blog_posts WHERE review_state='published' AND theme != ''`).Scan(&themes)
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM blog_posts b WHERE review_state='published' AND set_num != '' AND EXISTS (SELECT 1 FROM set_checks c WHERE c.set_num=b.set_num AND c.status='done' AND c.missing=0 AND c.id=(SELECT MAX(id) FROM set_checks WHERE set_num=b.set_num AND status='done'))`).Scan(&complete)
	_ = s.DB.QueryRow(`SELECT COALESCE(SUM(c.pieces),0) FROM set_checks c WHERE c.status='done' AND c.id=(SELECT MAX(id) FROM set_checks WHERE set_num=c.set_num AND status='done') AND c.set_num IN (SELECT set_num FROM blog_posts WHERE review_state='published')`).Scan(&parts)
	return map[string]int{"Sets": sets, "Themes": themes, "Complete": complete, "Parts": parts}
}

func (s *site) themes() []string {
	rows, err := s.DB.Query(`SELECT DISTINCT theme FROM blog_posts WHERE review_state='published' AND theme != '' ORDER BY theme`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if rows.Scan(&t) == nil {
			out = append(out, t)
		}
	}
	return out
}

// media serves one public photo: its note must start "blog:" and its set must have a published
// post (or the request carries that post's preview token).
func (s *site) media(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var setNum, path string
	if s.DB.QueryRow(`SELECT set_num, file_path FROM set_photos WHERE id=? AND note LIKE 'blog:%'`, id).Scan(&setNum, &path) != nil {
		http.NotFound(w, r)
		return
	}
	var n int
	if t := r.URL.Query().Get("t"); len(t) >= 8 {
		_ = s.DB.QueryRow(`SELECT COUNT(*) FROM blog_posts WHERE set_num=? AND preview_token=?`, setNum, t).Scan(&n)
	}
	if n == 0 {
		_ = s.DB.QueryRow(`SELECT COUNT(*) FROM blog_posts WHERE set_num=? AND review_state='published'`, setNum).Scan(&n)
	}
	if n == 0 {
		http.NotFound(w, r)
		return
	}
	f, err := os.OpenInRoot(s.AttachmentsDir, filepath.Base(path))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	fi, _ := f.Stat()
	raw, err := io.ReadAll(io.LimitReader(f, 25<<20))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	// Camera metadata (GPS, device, time) is stripped before anything leaves the server.
	clean, ctype, ok := cleanImage(raw)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Cache-Control", "public, max-age=3600")
	http.ServeContent(w, r, "", fi.ModTime(), bytes.NewReader(clean))
}

// sheetImage serves page n of the post's paper sheet when it is an image: only for a published post (or its
// preview token), with the camera metadata stripped like every other photo.
func (s *site) sheetImage(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil || n < 0 {
		http.NotFound(w, r)
		return
	}
	var visible int
	if t := r.URL.Query().Get("t"); len(t) >= 8 {
		_ = s.DB.QueryRow(`SELECT COUNT(*) FROM blog_posts WHERE slug=? AND preview_token=?`, slug, t).Scan(&visible)
	}
	if visible == 0 {
		_ = s.DB.QueryRow(`SELECT COUNT(*) FROM blog_posts WHERE slug=? AND review_state='published'`, slug).Scan(&visible)
	}
	var sheetFiles string
	if visible == 0 || s.DB.QueryRow(`SELECT sheet_files FROM blog_sections WHERE slug=?`, slug).Scan(&sheetFiles) != nil {
		http.NotFound(w, r)
		return
	}
	var files []struct{ Name, Kind string }
	_ = json.Unmarshal([]byte(sheetFiles), &files)
	i := 0
	for _, f := range files {
		if f.Kind != "jpeg" && f.Kind != "png" {
			continue
		}
		if i == n {
			s.serveStoredImage(w, r, f.Name)
			return
		}
		i++
	}
	http.NotFound(w, r)
}

func (s *site) serveStoredImage(w http.ResponseWriter, r *http.Request, name string) {
	f, err := os.OpenInRoot(s.AttachmentsDir, filepath.Base(name))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	fi, _ := f.Stat()
	raw, err := io.ReadAll(io.LimitReader(f, 25<<20))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	clean, ctype, ok := cleanImage(raw)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Cache-Control", "public, max-age=3600")
	http.ServeContent(w, r, "", fi.ModTime(), bytes.NewReader(clean))
}

func (s *site) feed(w http.ResponseWriter, r *http.Request) {
	type item struct {
		Title string `xml:"title"`
		Link  string `xml:"link"`
		GUID  string `xml:"guid"`
		Desc  string `xml:"description"`
		Date  string `xml:"pubDate,omitempty"`
	}
	var items []item
	for _, p := range s.published("published_at DESC", 30, "") {
		it := item{Title: p.Title, Link: s.BaseURL + "/set/" + p.Slug, GUID: s.BaseURL + "/set/" + p.Slug, Desc: p.Summary}
		if t, err := time.Parse("2006-01-02T15:04:05.999999", p.Published); err == nil {
			it.Date = t.Format(time.RFC1123Z)
		}
		items = append(items, it)
	}
	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	out := struct {
		XMLName xml.Name `xml:"rss"`
		Version string   `xml:"version,attr"`
		Title   string   `xml:"channel>title"`
		Link    string   `xml:"channel>link"`
		Desc    string   `xml:"channel>description"`
		Items   []item   `xml:"channel>item"`
	}{Version: "2.0", Title: s.SiteName, Link: s.BaseURL, Desc: s.Tagline, Items: items}
	fmt.Fprint(w, xml.Header)
	_ = xml.NewEncoder(w).Encode(out)
}

func (s *site) sitemap(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	fmt.Fprint(w, xml.Header+`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`+"<url><loc>"+s.BaseURL+"/</loc></url><url><loc>"+s.BaseURL+"/about</loc></url>")
	for _, p := range s.published("published_at DESC", 5000, "") {
		fmt.Fprintf(w, "<url><loc>%s/set/%s</loc></url>", s.BaseURL, template.HTMLEscapeString(p.Slug))
	}
	fmt.Fprint(w, "</urlset>")
}
