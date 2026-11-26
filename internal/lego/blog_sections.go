package lego

import (
	"bytes"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The structured part of a set's blog post: facts the writer confirms, the parts check pulled from the dashboard,
// the paper sheet, the build instructions, the bag count and photos. They live beside the Markdown (blog_posts) so a
// git sync of the text never touches them. Nothing here is public except what publicblog chooses to show.

// PostChanges is a post an admin sent back for improvements: the writer fixes it and sends it for review again.
const PostChanges = "changes"

type BlogFacts struct {
	Name  string `json:"name"`
	Theme string `json:"theme"`
	Year  string `json:"year"`
	Parts string `json:"parts"`
	Note  string `json:"note"` // why a fact differs from the catalog, shown to the reviewer
}

type BlogFile struct {
	Name string `json:"name"` // the stored file name in the attachments folder
	Kind string `json:"kind"` // jpeg, png, pdf or sheet (a saved digital sorting sheet)
	At   string `json:"at"`
}

type BlogSections struct {
	Slug, Writer   string
	Facts          BlogFacts
	FactsConfirmed bool
	Bags           int
	StampAck       bool
	CheckID        int64
	CheckJSON      string
	SheetFiles     []BlogFile
	InstrFiles     []BlogFile
	SheetPublic    string // the marked lines of a digital sheet, ready for the public page (no IDs or dates)
	Exists         bool
}

// CheckSnapshot is the parts check as the blog shows it: totals and the borrowed list, frozen when pulled.
type CheckSnapshot struct {
	CheckID    int64            `json:"check_id"`
	FinishedAt string           `json:"finished_at"`
	Pieces     int              `json:"pieces"`
	Have       int              `json:"have"`
	Missing    int              `json:"missing"`
	Extra      int              `json:"extra"`
	Borrowed   []SnapshotBorrow `json:"borrowed,omitempty"`
}

type SnapshotBorrow struct {
	Part     string `json:"part"`
	Colour   string `json:"colour"`
	Qty      int    `json:"qty"`
	From     string `json:"from"`
	FromName string `json:"from_name,omitempty"`
}

// SheetMark is one marked line of a digital sheet as the public page shows it.
type SheetMark struct {
	Part   string `json:"part"`
	Name   string `json:"name,omitempty"`
	Colour string `json:"colour,omitempty"`
	Mark   string `json:"mark"` // tick, cross or question
	Miss   string `json:"miss,omitempty"`
	Extra  string `json:"extra,omitempty"`
}

type BlogNote struct {
	ID              int64
	Slug, By, Kind  string
	Body, CreatedAt string
}

func (d *DB) Sections(slug string) (*BlogSections, error) {
	s := &BlogSections{Slug: slug}
	var facts, sheets, instrs string
	var confirmed, stamp int
	err := d.QueryRow(`SELECT writer, facts_json, facts_confirmed, bags, stamp_ack, check_id, check_json, sheet_files, instr_files, sheet_public FROM blog_sections WHERE slug = ?`, slug).
		Scan(&s.Writer, &facts, &confirmed, &s.Bags, &stamp, &s.CheckID, &s.CheckJSON, &sheets, &instrs, &s.SheetPublic)
	if errors.Is(err, sql.ErrNoRows) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	s.Exists, s.FactsConfirmed, s.StampAck = true, confirmed != 0, stamp != 0
	_ = json.Unmarshal([]byte(facts), &s.Facts)
	_ = json.Unmarshal([]byte(sheets), &s.SheetFiles)
	_ = json.Unmarshal([]byte(instrs), &s.InstrFiles)
	return s, nil
}

// EnsureSections creates the structured part of a post (when missing) and records who is writing it.
func (d *DB) EnsureSections(slug, writer string) error {
	_, err := d.Exec(`INSERT INTO blog_sections (slug, writer, updated_at) VALUES (?,?,?) ON CONFLICT(slug) DO UPDATE SET writer = CASE WHEN writer = '' THEN excluded.writer ELSE writer END`,
		slug, writer, flowNow())
	return err
}

// SetBlogWriter makes writer the one who writes the post (the checker who finished the onboarding).
func (d *DB) SetBlogWriter(slug, writer string) error {
	if err := d.EnsureSections(slug, writer); err != nil {
		return err
	}
	_, err := d.Exec(`UPDATE blog_sections SET writer = ? WHERE slug = ?`, writer, slug)
	return err
}

func (d *DB) touchSections(slug, set string, args ...any) error {
	if err := d.EnsureSections(slug, ""); err != nil {
		return err
	}
	_, err := d.Exec(`UPDATE blog_sections SET `+set+`, updated_at = ? WHERE slug = ?`, append(args, flowNow(), slug)...)
	return err
}

// SaveFacts stores the facts the writer confirmed or corrected (a changed fact needs a reason in Note).
func (d *DB) SaveFacts(slug string, f BlogFacts, confirmed bool) error {
	cat := d.CatalogFactsFor(slug)
	if confirmed && (f.Name != cat.Name || f.Theme != cat.Theme || f.Year != cat.Year || f.Parts != cat.Parts) && strings.TrimSpace(f.Note) == "" {
		return errors.New("a fact differs from the catalog: say why in the note")
	}
	b, _ := json.Marshal(f)
	c := 0
	if confirmed {
		c = 1
	}
	return d.touchSections(slug, `facts_json = ?, facts_confirmed = ?`, string(b), c)
}

// CatalogFactsFor is what the catalog says about the post's set (the values the writer confirms or corrects).
func (d *DB) CatalogFactsFor(slug string) BlogFacts {
	p, _ := d.GetPost(slug)
	if p == nil || p.SetNum == "" {
		return BlogFacts{}
	}
	f := BlogFacts{Name: p.Title, Theme: p.Theme}
	if s, _ := d.CatalogSet(p.SetNum); s != nil {
		f.Name, f.Theme, f.Year, f.Parts = s.Name, s.Theme, strconv.Itoa(s.Year), strconv.Itoa(s.Pieces)
	} else if s, _ := d.GetSetByNum(strings.TrimSuffix(p.SetNum, "-1")); s != nil {
		f.Name, f.Theme, f.Year, f.Parts = s.Name, s.Theme, strconv.Itoa(s.Year), strconv.Itoa(s.PartsQty)
	}
	return f
}

// SetBuild stores the bag count (how many photos are needed) and the "no location or speed shows" tick.
func (d *DB) SetBuild(slug string, bags int, stampAck bool) error {
	if bags < 0 || bags > 60 {
		return errors.New("the number of bags must be between 0 and 60")
	}
	a := 0
	if stampAck {
		a = 1
	}
	return d.touchSections(slug, `bags = ?, stamp_ack = ?`, bags, a)
}

// PullCheck freezes the set's last finished check (totals and the borrowed list) into the post.
func (d *DB) PullCheck(slug string) (*CheckSnapshot, error) {
	p, err := d.GetPost(slug)
	if err != nil || p == nil || p.SetNum == "" {
		return nil, errors.New("give the post a set number first")
	}
	c, _ := d.LastCheck(p.SetNum)
	if c == nil || c.Status != StatusDone {
		return nil, fmt.Errorf("set %s has no finished check yet: finish the check first", p.SetNum)
	}
	pieces, have, missing, extra, _ := c.Totals()
	snap := CheckSnapshot{CheckID: c.ID, FinishedAt: c.FinishedAt.UTC().Format("2006-01-02"), Pieces: pieces, Have: have, Missing: missing, Extra: extra}
	rows, _ := d.BorrowedForSet(p.SetNum)
	for _, b := range rows {
		colour := b.ColorName
		snap.Borrowed = append(snap.Borrowed, SnapshotBorrow{Part: b.NeedPartNum, Colour: colour, Qty: b.Qty, From: b.FromSetNum, FromName: b.FromSetName})
	}
	raw, _ := json.Marshal(snap)
	if err := d.touchSections(slug, `check_id = ?, check_json = ?`, c.ID, string(raw)); err != nil {
		return nil, err
	}
	return &snap, nil
}

// Snapshot is the pulled check, nil when none was pulled.
func (s *BlogSections) Snapshot() *CheckSnapshot {
	if s.CheckJSON == "" {
		return nil
	}
	var c CheckSnapshot
	if json.Unmarshal([]byte(s.CheckJSON), &c) != nil {
		return nil
	}
	return &c
}

// NewerCheck reports whether the set has been checked again since the check pulled into the post.
func (d *DB) NewerCheck(slug string) bool {
	p, _ := d.GetPost(slug)
	s, _ := d.Sections(slug)
	if p == nil || s == nil || s.CheckID == 0 {
		return false
	}
	st := d.GetSetState(p.SetNum)
	return st != nil && st.LastCheck != nil && st.LastCheck.ID != s.CheckID
}

// AddSectionFile records an uploaded file ("sheet" or "instr") against the post.
func (d *DB) AddSectionFile(slug, which string, f BlogFile) error {
	col := map[string]string{"sheet": "sheet_files", "instr": "instr_files"}[which]
	if col == "" {
		return errors.New("unknown kind of file")
	}
	s, err := d.Sections(slug)
	if err != nil {
		return err
	}
	list := s.SheetFiles
	if which == "instr" {
		list = s.InstrFiles
	}
	if len(list) >= 20 {
		return errors.New("that is enough files for one post")
	}
	f.At = flowNow()
	list = append(list, f)
	b, _ := json.Marshal(list)
	return d.touchSections(slug, col+` = ?`, string(b))
}

// RemoveSectionFile drops one file record and returns its stored name so the caller can delete the file.
func (d *DB) RemoveSectionFile(slug, which, name string) (string, error) {
	s, err := d.Sections(slug)
	if err != nil {
		return "", err
	}
	list, col := s.SheetFiles, "sheet_files"
	if which == "instr" {
		list, col = s.InstrFiles, "instr_files"
	}
	var keep []BlogFile
	removed := ""
	for _, f := range list {
		if f.Name == name {
			removed = f.Name
			continue
		}
		keep = append(keep, f)
	}
	if removed == "" {
		return "", errors.New("no such file on this post")
	}
	b, _ := json.Marshal(keep)
	return removed, d.touchSections(slug, col+` = ?`, string(b))
}

// SetSheetPublic stores the marked lines of a digital sheet for the public page.
func (d *DB) SetSheetPublic(slug string, marks []SheetMark) error {
	b, _ := json.Marshal(marks)
	return d.touchSections(slug, `sheet_public = ?`, string(b))
}

// MarksFromSheet turns a saved digital sheet's state into the marked lines the public page shows: only lines that
// carry a mark or a number, named from the set's last check, and never the IDs or dates.
func (d *DB) MarksFromSheet(setNum, sheetJSON string) []SheetMark {
	var sh sheetState
	if json.Unmarshal([]byte(sheetJSON), &sh) != nil {
		return nil
	}
	names := map[string]CheckLine{}
	if c, _ := d.LastCheck(setNum); c != nil {
		for _, l := range c.Lines {
			names[strings.ToLower(l.PartNum)+":"+strconv.Itoa(l.ColorID)] = l
		}
	}
	var out []SheetMark
	for _, l := range sh.Lines {
		if l.S == 0 && l.M == "" && l.X == "" {
			continue
		}
		m := SheetMark{Part: l.P, Miss: l.M, Extra: l.X, Mark: map[int]string{1: "tick", 2: "cross", 3: "question"}[l.S]}
		if cl, ok := names[strings.ToLower(l.P)+":"+strconv.Itoa(l.C)]; ok {
			m.Name, m.Colour = cl.PartName, cl.ColorName
		}
		out = append(out, m)
	}
	return out
}

// ---- review notes ----

func (d *DB) AddBlogNote(slug, by, kind, body string) error {
	body = strings.TrimSpace(body)
	if body == "" {
		return errors.New("a note is required")
	}
	_, err := d.Exec(`INSERT INTO blog_notes (slug, by_user, kind, body, created_at) VALUES (?,?,?,?,?)`, slug, by, kind, body, flowNow())
	return err
}

func (d *DB) BlogNotes(slug string) []BlogNote {
	rows, err := d.Query(`SELECT id, slug, by_user, kind, body, created_at FROM blog_notes WHERE slug = ? ORDER BY id DESC LIMIT 100`, slug)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []BlogNote
	for rows.Next() {
		var n BlogNote
		if rows.Scan(&n.ID, &n.Slug, &n.By, &n.Kind, &n.Body, &n.CreatedAt) == nil {
			out = append(out, n)
		}
	}
	return out
}

// ---- text lint (the rules scripts/blog-lint in the blog repo enforces) ----

var (
	lintTODO      = regexp.MustCompile(`TODO`)
	lintWikilink  = regexp.MustCompile(`\[\[[^\]]+\]\]`)
	lintCallout   = regexp.MustCompile(`(?m)^\s*>\s*\[![a-zA-Z]+\]`)
	lintComment   = regexp.MustCompile(`%%`)
	lintOtherImg  = regexp.MustCompile(`!\[[^\]]*\]\((https?:|//)`)
	lintUnsafe    = regexp.MustCompile(`\]\((javascript|data):`)
	lintMDLink    = regexp.MustCompile(`\]\([^)]*\.md([#)])`)
	lintFence     = regexp.MustCompile("(?m)^\\s*```")
	lintTable     = regexp.MustCompile(`(?m)^\s*\|.*\|`)
	lintRawHTML   = regexp.MustCompile(`<[a-zA-Z/][^>]*>`)
	lintEmail     = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[a-z]{2,}`)
	lintHighlight = regexp.MustCompile(`==[^=]+==`)
)

// LintPost checks a post's text the way the blog's importer and renderer will: errors block sending it for review,
// warnings are only shown.
func LintPost(body string) (errs, warns []string) {
	check := func(re *regexp.Regexp, list *[]string, msg string) {
		if re.MatchString(body) {
			*list = append(*list, msg)
		}
	}
	check(lintTODO, &errs, "still has TODO markers")
	check(lintWikilink, &errs, "[[wikilink]] or ![[embed]] is Obsidian-only: use [text](/set/slug); photos are added in the dashboard")
	check(lintCallout, &errs, "an Obsidian callout (> [!note]) is not supported")
	check(lintComment, &errs, "%%comment%% would be published: delete it")
	check(lintOtherImg, &errs, "images must be our own (start with /): add build photos in the dashboard")
	check(lintUnsafe, &errs, "an unsafe link (javascript: or data:)")
	check(lintMDLink, &errs, "a link to a .md file would 404: link to /set/<slug>")
	check(lintFence, &warns, "code fences are not supported and show as plain text")
	check(lintTable, &warns, "tables are not supported: use a list")
	check(lintRawHTML, &warns, "raw HTML is shown as text, not rendered")
	check(lintEmail, &warns, "looks like an email address: no customer details on the blog")
	check(lintHighlight, &warns, "==highlight== is not supported")
	return
}

// ---- photos ----

var bagCaptionRE = regexp.MustCompile(`(?i)^bag\s*(\d+)$`)

// BagSlots lists which of the required photos (Bag 1 … Bag N and Finished set) the set's public photos still lack.
func (d *DB) BagSlots(setNum string, bags int) (have, missing []string) {
	got := map[string]bool{}
	if photos, err := d.SetPhotos(setNum); err == nil {
		for _, p := range photos {
			if !strings.HasPrefix(p.Note, PublicPhotoPrefix) {
				continue
			}
			c := strings.TrimSpace(strings.TrimPrefix(p.Note, PublicPhotoPrefix))
			if m := bagCaptionRE.FindStringSubmatch(c); m != nil {
				got["Bag "+m[1]] = true
			} else if strings.EqualFold(c, "finished set") {
				got["Finished set"] = true
			}
		}
	}
	slots := []string{}
	for i := 1; i <= bags; i++ {
		slots = append(slots, "Bag "+strconv.Itoa(i))
	}
	slots = append(slots, "Finished set")
	for _, s := range slots {
		if got[s] {
			have = append(have, s)
		} else {
			missing = append(missing, s)
		}
	}
	return
}

// jpegTakenAt reads the EXIF capture time (DateTimeOriginal) of a JPEG; ok is false when there is none.
func jpegTakenAt(b []byte) (time.Time, bool) {
	if !bytes.HasPrefix(b, []byte{0xFF, 0xD8}) {
		return time.Time{}, false
	}
	i := 2
	for i+4 <= len(b) && b[i] == 0xFF {
		marker, n := b[i+1], int(binary.BigEndian.Uint16(b[i+2:i+4]))
		if marker == 0xDA || n < 2 {
			break
		}
		if marker == 0xE1 && i+4+n-2 <= len(b) && bytes.HasPrefix(b[i+4:], []byte("Exif\x00\x00")) {
			return exifDateTimeOriginal(b[i+10 : i+2+n])
		}
		i += 2 + n
	}
	return time.Time{}, false
}

func exifDateTimeOriginal(t []byte) (time.Time, bool) {
	if len(t) < 8 {
		return time.Time{}, false
	}
	var bo binary.ByteOrder = binary.BigEndian
	if t[0] == 'I' {
		bo = binary.LittleEndian
	}
	u16 := func(o int) int {
		if o < 0 || o+2 > len(t) {
			return -1
		}
		return int(bo.Uint16(t[o:]))
	}
	u32 := func(o int) int {
		if o < 0 || o+4 > len(t) {
			return -1
		}
		return int(bo.Uint32(t[o:]))
	}
	find := func(ifd, tag int) (valueOffset, count int) {
		n := u16(ifd)
		for k := 0; k < n; k++ {
			e := ifd + 2 + 12*k
			if u16(e) == tag {
				return e + 8, u32(e + 4)
			}
		}
		return -1, 0
	}
	ifd0 := u32(4)
	off, _ := find(ifd0, 0x8769) // the Exif sub-IFD
	if off < 0 {
		return time.Time{}, false
	}
	sub := u32(off)
	vo, cnt := find(sub, 0x9003)
	if vo < 0 || cnt < 19 {
		return time.Time{}, false
	}
	start := u32(vo)
	if start < 0 || start+19 > len(t) {
		return time.Time{}, false
	}
	tm, err := time.ParseInLocation("2006:01:02 15:04:05", string(t[start:start+19]), time.Local)
	return tm, err == nil
}

// EarlyPhotos names the public photos taken before the check finished (judging by the capture time in the file).
// Best effort: a photo without a capture time is not reported.
func (d *DB) EarlyPhotos(setNum, attachDir string, after time.Time) []string {
	var out []string
	photos, err := d.SetPhotos(setNum)
	if err != nil || after.IsZero() {
		return nil
	}
	for _, p := range photos {
		if !strings.HasPrefix(p.Note, PublicPhotoPrefix) {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(attachDir, filepath.Base(p.FilePath)))
		if err != nil {
			continue
		}
		if at, ok := jpegTakenAt(raw); ok && at.Before(after) {
			out = append(out, strings.TrimSpace(strings.TrimPrefix(p.Note, PublicPhotoPrefix)))
		}
	}
	return out
}

// ---- readiness: what has to be true before the writer can send the post for review ----

type ReadyCheck struct {
	Name     string `json:"name"`
	OK       bool   `json:"ok"`
	Blocking bool   `json:"blocking"`
	Detail   string `json:"detail,omitempty"`
}

// ReadyChecks is the last page's list. attachDir is only used for the photo-time warning (pass "" to skip it).
func (d *DB) ReadyChecks(slug, attachDir string) []ReadyCheck {
	p, _ := d.GetPost(slug)
	s, _ := d.Sections(slug)
	if p == nil || s == nil {
		return nil
	}
	var out []ReadyCheck
	add := func(name string, ok, blocking bool, detail string) {
		out = append(out, ReadyCheck{name, ok, blocking, detail})
	}
	add("Overview confirmed", s.FactsConfirmed, true, "Confirm the set's facts, or correct them with a reason.")
	snap := s.Snapshot()
	add("Parts check pulled from the dashboard", snap != nil, true, "Press Pull from dashboard once the check is finished.")
	if snap != nil && d.NewerCheck(slug) {
		add("Parts check is the latest", false, false, "The set has been checked again since you pulled it: pull again.")
	}
	last, _ := d.LastCheck(p.SetNum)
	add("Check finished", last != nil && last.Status == StatusDone, true, "Finish the check of the set first.")
	unrecorded := false
	if last != nil {
		for _, l := range last.Lines {
			if l.Borrowed > 0 {
				recorded := 0
				_ = d.QueryRow(`SELECT COALESCE(SUM(qty),0) FROM borrowed_parts WHERE build_set_num = ? AND need_part_num = ? AND (color_id < 0 OR color_id = ?)`, p.SetNum, l.PartNum, l.ColorID).Scan(&recorded)
				unrecorded = unrecorded || recorded < l.Borrowed
			}
		}
	}
	add("Borrowed parts are on the Borrowed list", !unrecorded, true, "A line says borrowed but the Borrowed list does not have it.")
	add("Paper check sheet uploaded", len(s.SheetFiles) > 0, true, "Upload your sorting sheet.")
	add("Build instructions added", len(s.InstrFiles) > 0, true, "Upload the instructions PDF(s) (staff only).")
	add("Number of bags entered", s.Bags > 0, true, "Say how many bags the set has.")
	if s.Bags > 0 {
		_, missing := d.BagSlots(p.SetNum, s.Bags)
		add("A photo for every bag and the finished set", len(missing) == 0, true, "Missing: "+strings.Join(missing, ", "))
	}
	add("Photos show no location or speed", s.StampAck, true, "Tick the box once you have checked each photo.")
	errs, warns := LintPost(p.Body)
	add("Text is clean", len(errs) == 0, true, strings.Join(errs, "; "))
	if len(warns) > 0 {
		add("Text warnings", false, false, strings.Join(warns, "; "))
	}
	if attachDir != "" && last != nil {
		if early := d.EarlyPhotos(p.SetNum, attachDir, last.FinishedAt); len(early) > 0 {
			add("Photos taken after the check", false, false, "Taken before the check finished: "+strings.Join(early, ", "))
		}
	}
	return out
}

// PostReady is true when no blocking check fails.
func (d *DB) PostReady(slug string) bool {
	for _, c := range d.ReadyChecks(slug, "") {
		if c.Blocking && !c.OK {
			return false
		}
	}
	return true
}
