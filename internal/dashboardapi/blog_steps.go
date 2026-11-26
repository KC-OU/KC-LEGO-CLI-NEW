package dashboardapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

// The writer's steps of a blog post (overview, parts check, build and photos, last page) and the reviewers' notes.
// See lego/blog_sections.go. Everything here is staff-only; what the public sees is chosen in internal/publicblog.

type stepsView struct {
	Slug           string              `json:"slug"`
	Title          string              `json:"title"`
	SetNum         string              `json:"set_num"`
	State          string              `json:"state"`
	Writer         string              `json:"writer"`
	Catalog        lego.BlogFacts      `json:"catalog"`
	Facts          lego.BlogFacts      `json:"facts"`
	FactsConfirmed bool                `json:"facts_confirmed"`
	Check          *lego.CheckSnapshot `json:"check"`
	NewerCheck     bool                `json:"newer_check"`
	Sheet          []lego.BlogFile     `json:"sheet_files"`
	Instructions   []lego.BlogFile     `json:"instr_files"`
	Bags           int                 `json:"bags"`
	StampAck       bool                `json:"stamp_ack"`
	SlotsHave      []string            `json:"slots_have"`
	SlotsMissing   []string            `json:"slots_missing"`
	Ready          []lego.ReadyCheck   `json:"ready"`
	CanSend        bool                `json:"can_send"`
	Notes          []noteView          `json:"notes"`
}

type noteView struct {
	By   string `json:"by"`
	Kind string `json:"kind"`
	Body string `json:"body"`
	At   string `json:"at"`
}

func (s *Server) stepsFor(slug string) (*stepsView, int, error) {
	p, err := s.legoDB.GetPost(slug)
	if err != nil || p == nil {
		return nil, http.StatusNotFound, fmt.Errorf("no such post")
	}
	sec, err := s.legoDB.Sections(slug)
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	v := &stepsView{Slug: p.Slug, Title: p.Title, SetNum: p.SetNum, State: p.State, Writer: sec.Writer, Catalog: s.legoDB.CatalogFactsFor(slug),
		Facts: sec.Facts, FactsConfirmed: sec.FactsConfirmed, Check: sec.Snapshot(), NewerCheck: s.legoDB.NewerCheck(slug),
		Sheet: sec.SheetFiles, Instructions: sec.InstrFiles, Bags: sec.Bags, StampAck: sec.StampAck}
	if v.Facts == (lego.BlogFacts{}) {
		v.Facts = v.Catalog // nothing confirmed yet: start from what the catalog says
	}
	if sec.Bags > 0 {
		v.SlotsHave, v.SlotsMissing = s.legoDB.BagSlots(p.SetNum, sec.Bags)
	}
	v.Ready = s.legoDB.ReadyChecks(slug, attachmentsDir())
	v.CanSend = true
	for _, c := range v.Ready {
		if c.Blocking && !c.OK {
			v.CanSend = false
		}
	}
	for _, n := range s.legoDB.BlogNotes(slug) {
		v.Notes = append(v.Notes, noteView{By: n.By, Kind: n.Kind, Body: n.Body, At: n.CreatedAt})
	}
	return v, http.StatusOK, nil
}

// handleBlogSteps is GET /dashboard/blog/{slug}/steps.
func (s *Server) handleBlogSteps(w http.ResponseWriter, r *http.Request) {
	v, code, err := s.stepsFor(r.PathValue("slug"))
	if err != nil {
		writeJSON(w, code, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) decodeBy(w http.ResponseWriter, r *http.Request, v any) (by string, ok bool) {
	var raw map[string]json.RawMessage
	body, _ := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
	if json.Unmarshal(body, &raw) != nil || json.Unmarshal(body, v) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return "", false
	}
	_ = json.Unmarshal(raw["by"], &by)
	if strings.TrimSpace(by) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "by is required"})
		return "", false
	}
	return by, true
}

// claimWriter makes the first person to work on a post's sections its writer.
func (s *Server) claimWriter(slug, by string) { _ = s.legoDB.EnsureSections(slug, by) }

func (s *Server) handleBlogFacts(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	var req struct {
		Name, Theme, Year, Parts, Note string
		Confirmed                      bool
	}
	by, ok := s.decodeBy(w, r, &req)
	if !ok {
		return
	}
	s.claimWriter(slug, by)
	if err := s.legoDB.SaveFacts(slug, lego.BlogFacts{Name: strings.TrimSpace(req.Name), Theme: strings.TrimSpace(req.Theme), Year: strings.TrimSpace(req.Year),
		Parts: strings.TrimSpace(req.Parts), Note: strings.TrimSpace(req.Note)}, req.Confirmed); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(by, "BLOG_FACTS", "SUCCESS", slug)
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

func (s *Server) handleBlogPullCheck(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	var req struct{}
	by, ok := s.decodeBy(w, r, &req)
	if !ok {
		return
	}
	s.claimWriter(slug, by)
	snap, err := s.legoDB.PullCheck(slug)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(by, "BLOG_CHECK_PULLED", "SUCCESS", slug)
	writeJSON(w, http.StatusOK, snap)
}

func (s *Server) handleBlogBuild(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	var req struct {
		Bags     int
		StampAck bool `json:"stamp_ack"`
	}
	by, ok := s.decodeBy(w, r, &req)
	if !ok {
		return
	}
	s.claimWriter(slug, by)
	if err := s.legoDB.SetBuild(slug, req.Bags, req.StampAck); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

// handleBlogFile is POST /dashboard/blog/{slug}/file (multipart: which=sheet|instr, by, file).
func (s *Server) handleBlogFile(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	p, _ := s.legoDB.GetPost(slug)
	if p == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such post"})
		return
	}
	if err := r.ParseMultipartForm(maxFlowUpload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "could not read the upload: " + err.Error()})
		return
	}
	which, by := r.FormValue("which"), strings.TrimSpace(r.FormValue("by"))
	if (which != "sheet" && which != "instr") || by == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "which (sheet or instr) and by are required"})
		return
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "a file is required"})
		return
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maxFlowUpload+1))
	if err != nil || len(raw) > maxFlowUpload {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "the file is too big (25 MB at most)"})
		return
	}
	dir := attachmentsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	var stored, kind, sheet string
	lower := strings.ToLower(hdr.Filename)
	if which == "sheet" && (strings.HasSuffix(lower, ".html") || strings.HasSuffix(lower, ".htm")) {
		if sheet = lego.ParseSheetState(raw); sheet == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "that HTML file is not a saved sorting sheet: use Save filled sheet, then upload that file"})
			return
		}
		kind = "sheet"
		stored = fmt.Sprintf("blog-%s-%s.html", slug, time.Now().UTC().Format("20060102-150405.000"))
		if err := os.WriteFile(filepath.Join(dir, stored), raw, 0o600); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	} else {
		tmp, err := os.CreateTemp("", "blogfile-*")
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		defer os.Remove(tmp.Name())
		_, werr := tmp.Write(raw)
		tmp.Close()
		if werr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": werr.Error()})
			return
		}
		if kind, err = lego.VerifyProofFile(tmp.Name()); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if which == "instr" && kind != "pdf" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "build instructions must be a PDF"})
			return
		}
		path, err := lego.StoreProof(dir, "blog-"+slug, kind, tmp.Name())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		stored = filepath.Base(path)
	}
	s.claimWriter(slug, by)
	if err := s.legoDB.AddSectionFile(slug, which, lego.BlogFile{Name: stored, Kind: kind}); err != nil {
		_ = os.Remove(filepath.Join(dir, stored))
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if sheet != "" {
		_ = s.legoDB.SetSheetPublic(slug, s.legoDB.MarksFromSheet(p.SetNum, sheet))
	}
	s.logAudit(by, "BLOG_FILE_ADDED", "SUCCESS", slug+" "+which)
	writeJSON(w, http.StatusOK, map[string]string{"name": stored, "kind": kind})
}

func (s *Server) handleBlogFileRemove(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	var req struct{ Which, Name string }
	by, ok := s.decodeBy(w, r, &req)
	if !ok {
		return
	}
	name, err := s.legoDB.RemoveSectionFile(slug, req.Which, filepath.Base(req.Name))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	_ = os.Remove(filepath.Join(attachmentsDir(), name))
	s.logAudit(by, "BLOG_FILE_REMOVED", "SUCCESS", slug+" "+req.Which)
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

// handleBlogFileGet is GET /dashboard/blog/{slug}/file?which=&name= : a staff download (never inline HTML).
func (s *Server) handleBlogFileGet(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	sec, _ := s.legoDB.Sections(slug)
	name := filepath.Base(r.URL.Query().Get("name"))
	known := false
	for _, f := range append(append([]lego.BlogFile{}, sec.SheetFiles...), sec.InstrFiles...) {
		known = known || f.Name == name
	}
	if !known {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such file on this post"})
		return
	}
	f, err := os.OpenInRoot(attachmentsDir(), name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "the file is gone"})
		return
	}
	defer f.Close()
	ctype, disp := "application/octet-stream", "attachment"
	switch strings.ToLower(filepath.Ext(name)) {
	case ".pdf":
		ctype, disp = "application/pdf", "inline"
	case ".png":
		ctype, disp = "image/png", "inline"
	case ".jpg", ".jpeg":
		ctype, disp = "image/jpeg", "inline"
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Content-Disposition", disp+`; filename="`+name+`"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.Copy(w, f)
}

// blogStateOutcome is what the writer is told when a reviewer moves their post.
func blogStateOutcome(slug, state, note string) string {
	switch state {
	case lego.PostPublished:
		return fmt.Sprintf("Your blog post %s was approved and is live.", slug)
	case lego.PostChanges:
		return fmt.Sprintf("Your blog post %s needs changes: %s", slug, note)
	case lego.PostRejected:
		return fmt.Sprintf("Your blog post %s was rejected: %s", slug, note)
	}
	return ""
}

func blogCheckNames(checks []lego.ReadyCheck) string {
	var out []string
	for _, c := range checks {
		if c.Blocking && !c.OK {
			out = append(out, c.Name)
		}
	}
	return strings.Join(out, "; ")
}
