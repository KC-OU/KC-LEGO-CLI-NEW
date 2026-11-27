package dashboardapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

// The web inbox and the onboarding flow (see lego/onboarding_flow.go): a checker uploads the paper check of a
// set, an admin verifies it, the checker does the check and submits. Everything here trusts the bearer token and
// the user names the Rust service passes, like every other handler in this package.

type messageView struct {
	ID        int64  `json:"id"`
	From      string `json:"from"`
	To        string `json:"to"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
	Read      bool   `json:"read"`
	ParentID  int64  `json:"parent_id,omitempty"`
}

// notifyUser puts a message in a user's inbox (the TUI shows it once as a toast too). Failures are ignored on
// purpose: a message that can't be stored must never fail the action it reports.
func (s *Server) notifyUser(from, to, body string) {
	if strings.TrimSpace(to) == "" {
		return
	}
	_, _ = s.legoDB.SendMessageRef(from, to, body, 0)
}

// notifyVerifiers tells everyone who can verify a paper check.
func (s *Server) notifyVerifiers(from, body string) { s.notifyWithPerm("sets.verify", from, body) }

// notifyWithPerm tells everyone who holds perm (admins included).
func (s *Server) notifyWithPerm(perm, from, body string) {
	p, err := access.Load()
	if err != nil {
		return
	}
	for _, name := range p.WithPerm(perm) {
		if !strings.EqualFold(name, from) {
			s.notifyUser(from, name, body)
		}
	}
}

func (s *Server) handleMessagesList(w http.ResponseWriter, r *http.Request) {
	user := strings.TrimSpace(r.URL.Query().Get("user"))
	if user == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "user is required"})
		return
	}
	msgs, err := s.legoDB.ListMessages(user, 200)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	out := make([]messageView, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, messageView{ID: m.ID, From: m.From, To: m.To, Body: m.Body, CreatedAt: m.CreatedAt.Format(time.RFC3339), Read: !m.ReadAt.IsZero(), ParentID: m.ParentID})
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": out, "unread": s.legoDB.UnreadCount(user)})
}

func (s *Server) handleMessagesUnread(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]int{"unread": s.legoDB.UnreadCount(strings.TrimSpace(r.URL.Query().Get("user")))})
}

func (s *Server) handleMessagesRead(w http.ResponseWriter, r *http.Request) {
	var req struct {
		User string
		ID   int64
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || strings.TrimSpace(req.User) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "user is required"})
		return
	}
	if err := s.legoDB.MarkRead(req.User, req.ID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"unread": s.legoDB.UnreadCount(req.User)})
}

// handleMessagesSend starts a message: admins only (Admin is the Rust service's own is-admin answer).
func (s *Server) handleMessagesSend(w http.ResponseWriter, r *http.Request) {
	var req struct {
		From, To, Body string
		Admin          bool
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || strings.TrimSpace(req.From) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "from, to and body are required"})
		return
	}
	if !req.Admin {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "only admins can start a message: reply to one you were sent instead"})
		return
	}
	id, err := s.legoDB.SendMessageRef(req.From, req.To, req.Body, 0)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(req.From, "DASHBOARD_MESSAGE", "SUCCESS", "to "+req.To)
	writeJSON(w, http.StatusOK, map[string]int64{"id": id})
}

func (s *Server) handleMessagesReply(w http.ResponseWriter, r *http.Request) {
	var req struct {
		From, Body string
		ParentID   int64 `json:"parent_id"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || strings.TrimSpace(req.From) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "from, parent_id and body are required"})
		return
	}
	id, err := s.legoDB.ReplyMessage(req.From, req.ParentID, req.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"id": id})
}

// ---- onboarding flow ----

type flowView struct {
	ID          int64    `json:"id"`
	SetNum      string   `json:"set_num"`
	User        string   `json:"user"`
	Status      string   `json:"status"`
	PaperKind   string   `json:"paper_kind,omitempty"`
	HasPaper    bool     `json:"has_paper"`
	Digital     bool     `json:"digital"` // a saved sheet: its marks can be compared with the check
	Verifier    string   `json:"verifier,omitempty"`
	VerifyNote  string   `json:"verify_note,omitempty"`
	VerifyTags  string   `json:"verify_tags,omitempty"`
	VerifiedAt  string   `json:"verified_at,omitempty"`
	Discrepancy string   `json:"discrepancy,omitempty"`
	CompletedAt string   `json:"completed_at,omitempty"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
	Diff        []string `json:"diff,omitempty"`
	// Cleared is whether the user may start the check now; CheckDone whether the set has a finished check newer
	// than the verification (what Submit needs).
	Cleared   bool `json:"cleared"`
	CheckDone bool `json:"check_done"`
}

func (s *Server) flowToView(f *lego.OnboardingFlow, detail bool) flowView {
	v := flowView{ID: f.ID, SetNum: f.SetNum, User: f.User, Status: f.Status, PaperKind: f.PaperKind, HasPaper: f.PaperFile != "", Digital: f.SheetJSON != "",
		Verifier: f.Verifier, VerifyNote: f.VerifyNote, VerifyTags: f.VerifyTags, VerifiedAt: f.VerifiedAt, Discrepancy: f.Discrepancy, CompletedAt: f.Completed,
		CreatedAt: f.CreatedAt, UpdatedAt: f.UpdatedAt, Cleared: s.legoDB.OnboardingCleared(f.SetNum, f.User)}
	v.CheckDone = s.legoDB.FlowCheckDone(f)
	if detail {
		v.Diff = s.legoDB.SheetDiff(f)
	}
	return v
}

func (s *Server) flowFromPath(w http.ResponseWriter, r *http.Request) (*lego.OnboardingFlow, bool) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	f, err := s.legoDB.GetFlow(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return nil, false
	}
	return f, true
}

func (s *Server) handleFlowStart(w http.ResponseWriter, r *http.Request) {
	var req struct{ Set, User string }
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "set and user are required"})
		return
	}
	f, err := s.legoDB.StartFlow(req.Set, req.User)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, s.flowToView(f, false))
}

// handleFlowLookup is GET /dashboard/onboarding/flow?set=&user= : the user's newest onboarding of the set, or 404.
func (s *Server) handleFlowLookup(w http.ResponseWriter, r *http.Request) {
	f, err := s.legoDB.FlowFor(strings.TrimSpace(r.URL.Query().Get("set")), strings.TrimSpace(r.URL.Query().Get("user")))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if f == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no onboarding started for this set"})
		return
	}
	writeJSON(w, http.StatusOK, s.flowToView(f, false))
}

func (s *Server) handleFlowList(w http.ResponseWriter, r *http.Request) {
	list, err := s.legoDB.ListFlows(strings.TrimSpace(r.URL.Query().Get("status")))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	out := make([]flowView, 0, len(list))
	for i := range list {
		out = append(out, s.flowToView(&list[i], false))
	}
	writeJSON(w, http.StatusOK, map[string]any{"flows": out, "waived": s.legoDB.Waivers()})
}

func (s *Server) handleFlowDetail(w http.ResponseWriter, r *http.Request) {
	if f, ok := s.flowFromPath(w, r); ok {
		writeJSON(w, http.StatusOK, s.flowToView(f, true))
	}
}

const maxFlowUpload = 25 << 20

// handleFlowPaper is POST .../paper: the checker's paper check, as a scan or photo (JPEG, PNG, PDF) or as a saved
// digital sorting sheet (an .html file that carries its marks).
func (s *Server) handleFlowPaper(w http.ResponseWriter, r *http.Request) {
	f, ok := s.flowFromPath(w, r)
	if !ok {
		return
	}
	if err := r.ParseMultipartForm(maxFlowUpload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "could not read the upload: " + err.Error()})
		return
	}
	user := strings.TrimSpace(r.FormValue("user"))
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
	if code, err := s.storeFlowPaper(f, user, hdr.Filename, raw); err != nil {
		writeJSON(w, code, map[string]string{"error": err.Error()})
		return
	}
	nf, _ := s.legoDB.GetFlow(f.ID)
	writeJSON(w, http.StatusOK, s.flowToView(nf, false))
}

// handleFlowPaperFile serves the uploaded paper check to an admin. A saved sheet is HTML from a user, so it is
// only ever sent as a download, never as a page.
func (s *Server) handleFlowPaperFile(w http.ResponseWriter, r *http.Request) {
	f, ok := s.flowFromPath(w, r)
	if !ok {
		return
	}
	if f.PaperFile == "" || f.PaperFile != filepath.Base(f.PaperFile) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no paper check uploaded"})
		return
	}
	file, err := os.OpenInRoot(attachmentsDir(), f.PaperFile)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "the uploaded file is gone"})
		return
	}
	defer file.Close()
	ctype, disp := "application/octet-stream", "attachment"
	switch strings.ToLower(filepath.Ext(f.PaperFile)) {
	case ".pdf":
		ctype, disp = "application/pdf", "inline"
	case ".png":
		ctype, disp = "image/png", "inline"
	case ".jpg", ".jpeg":
		ctype, disp = "image/jpeg", "inline"
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Content-Disposition", disp+`; filename="`+f.PaperFile+`"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.Copy(w, file)
}

func (s *Server) handleFlowVerify(w http.ResponseWriter, r *http.Request) {
	f, ok := s.flowFromPath(w, r)
	if !ok {
		return
	}
	var req struct {
		By, Note, Tags string
		OK             bool
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || strings.TrimSpace(req.By) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "by is required"})
		return
	}
	nf, err := s.legoDB.VerifyFlow(f.ID, req.By, req.OK, req.Note, req.Tags)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.OK {
		s.logAudit(req.By, "ONBOARDING_VERIFIED", "SUCCESS", f.SetNum+" "+f.User)
		s.notifyUser(req.By, f.User, fmt.Sprintf("Your paper check for set %s is verified. You can start the check now.", f.SetNum))
	} else {
		s.logAudit(req.By, "ONBOARDING_CHANGES_REQUESTED", "SUCCESS", f.SetNum+" "+f.User)
		msg := fmt.Sprintf("Your paper check for set %s needs changes: %s", f.SetNum, strings.TrimSpace(req.Note))
		if t := strings.TrimSpace(req.Tags); t != "" {
			msg += " [" + t + "]"
		}
		s.notifyUser(req.By, f.User, msg+" Redo it and upload it again.")
	}
	writeJSON(w, http.StatusOK, s.flowToView(nf, false))
}

func (s *Server) handleFlowDiscrepancy(w http.ResponseWriter, r *http.Request) {
	f, ok := s.flowFromPath(w, r)
	if !ok {
		return
	}
	var req struct{ By, Note string }
	if json.NewDecoder(r.Body).Decode(&req) != nil || strings.TrimSpace(req.By) == "" || strings.TrimSpace(req.Note) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "by and a note are required"})
		return
	}
	if err := s.legoDB.SetFlowDiscrepancy(f.ID, req.Note); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(req.By, "ONBOARDING_DISCREPANCY", "SUCCESS", f.SetNum+" "+f.User)
	s.notifyUser(req.By, f.User, fmt.Sprintf("Set %s: the admin found a difference between your paper check and the dashboard check: %s", f.SetNum, strings.TrimSpace(req.Note)))
	writeJSON(w, http.StatusOK, map[string]string{"status": "sent"})
}

// handleFlowComplete is the Submit step ("Complete and Generate Blog Template"): the check must be finished.
// It opens the set's blog draft and tells the admins to audit.
func (s *Server) handleFlowComplete(w http.ResponseWriter, r *http.Request) {
	f, ok := s.flowFromPath(w, r)
	if !ok {
		return
	}
	var req struct {
		User    string
		Answers json.RawMessage
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || strings.TrimSpace(req.User) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "user is required"})
		return
	}
	nf, err := s.legoDB.CompleteFlow(f.ID, req.User, string(req.Answers))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	label := f.SetNum
	if cs, _ := s.legoDB.CatalogSet(f.SetNum); cs != nil {
		label = cs.Name
	}
	_ = s.legoDB.EnsureDraftForSet(f.SetNum, label, req.User)
	_ = s.legoDB.SetBlogWriter(strings.ToLower(f.SetNum), req.User)
	s.logAudit(req.User, "ONBOARDING_COMPLETE", "SUCCESS", f.SetNum)
	s.notifyVerifiers(req.User, fmt.Sprintf("%s finished the check of set %s and submitted it: compare it with the paper check.", req.User, f.SetNum))
	s.notifyUser("system", req.User, fmt.Sprintf("Onboarding for set %s is complete. Now write the blog post: download the template from the Onboarding page.", f.SetNum))
	writeJSON(w, http.StatusOK, s.flowToView(nf, false))
}

func (s *Server) handleBlogTemplate(w http.ResponseWriter, r *http.Request) {
	set := strings.TrimSpace(r.URL.Query().Get("set"))
	if set == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "set is required"})
		return
	}
	if why := s.legoDB.BlogBlock(set); why != "" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "the blog template can't be generated: " + why})
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+strings.ToLower(filepath.Base(set))+`.md"`)
	_, _ = io.WriteString(w, s.legoDB.BlogTemplate(set))
}

func (s *Server) handleOnboardingWaive(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Set, By string
		On      bool
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || strings.TrimSpace(req.By) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "set and by are required"})
		return
	}
	if err := s.legoDB.WaiveOnboarding(req.Set, req.By, req.On); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(req.By, "ONBOARDING_WAIVER", "SUCCESS", fmt.Sprintf("%s on=%v", req.Set, req.On))
	writeJSON(w, http.StatusOK, map[string]any{"waived": s.legoDB.Waivers()})
}

// storeFlowPaper saves an uploaded paper check (a saved sheet, or a JPEG, PNG or PDF scan) against the flow and
// queues it for an admin. The int is the HTTP status to answer with when there is an error.
func (s *Server) storeFlowPaper(f *lego.OnboardingFlow, user, filename string, raw []byte) (int, error) {
	dir := attachmentsDir()
	var stored, kind, sheet string
	if strings.HasSuffix(strings.ToLower(filename), ".html") || strings.HasSuffix(strings.ToLower(filename), ".htm") {
		if sheet = lego.ParseSheetState(raw); sheet == "" {
			return http.StatusBadRequest, errors.New("that HTML file is not a saved sorting sheet: use Save filled sheet on the sheet, then upload that file")
		}
		kind = "sheet"
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return http.StatusInternalServerError, err
		}
		stored = fmt.Sprintf("onboarding-%d-%s.html", f.ID, time.Now().UTC().Format("20060102-150405"))
		if err := os.WriteFile(filepath.Join(dir, stored), raw, 0o600); err != nil {
			return http.StatusInternalServerError, err
		}
	} else {
		tmp, err := os.CreateTemp("", "onboarding-*")
		if err != nil {
			return http.StatusInternalServerError, err
		}
		defer os.Remove(tmp.Name())
		_, werr := tmp.Write(raw)
		tmp.Close()
		if werr != nil {
			return http.StatusInternalServerError, werr
		}
		if kind, err = lego.VerifyProofFile(tmp.Name()); err != nil {
			return http.StatusBadRequest, err
		}
		path, err := lego.StoreProof(dir, "onboarding-"+strconv.FormatInt(f.ID, 10), kind, tmp.Name())
		if err != nil {
			return http.StatusInternalServerError, err
		}
		stored = filepath.Base(path)
	}
	if err := s.legoDB.SubmitPaper(f.ID, user, kind, stored, sheet); err != nil {
		return http.StatusBadRequest, err
	}
	s.logAudit(user, "ONBOARDING_PAPER_UPLOADED", "SUCCESS", f.SetNum)
	s.notifyVerifiers(user, fmt.Sprintf("Paper check for set %s from %s is waiting to be verified.", f.SetNum, user))
	return http.StatusOK, nil
}
