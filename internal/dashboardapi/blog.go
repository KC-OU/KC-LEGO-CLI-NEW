// Admin side of the public sets blog: list and edit posts, and move them through
// draft -> review -> published. Publishing is the only way a post reaches the public site, so
// the dashboard restricts approve/unpublish to admins; this API only records who did it.
package dashboardapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

type blogPostView struct {
	Slug        string `json:"slug"`
	SetNum      string `json:"set_num"`
	Theme       string `json:"theme"`
	Title       string `json:"title"`
	Summary     string `json:"summary"`
	Body        string `json:"body,omitempty"`
	Audio       string `json:"audio"`
	BuildStatus string `json:"build_status"`
	State       string `json:"state"`
	Token       string `json:"preview_token"`
	Source      string `json:"source"`
	UpdatedBy   string `json:"updated_by"`
	UpdatedAt   string `json:"updated_at"`
	PublishedAt string `json:"published_at"`
	Photos      int    `json:"public_photos"`
	Writer      string `json:"writer,omitempty"`
}

func (s *Server) postView(p lego.Post, withBody bool) blogPostView {
	v := blogPostView{Slug: p.Slug, SetNum: p.SetNum, Theme: p.Theme, Title: p.Title, Summary: p.Summary, Audio: p.Audio,
		BuildStatus: p.BuildStatus, State: p.State, Token: p.PreviewToken, Source: p.Source, UpdatedBy: p.UpdatedBy,
		UpdatedAt: p.UpdatedAt, PublishedAt: p.PublishedAt}
	if withBody {
		v.Body = p.Body
	}
	if sec, err := s.legoDB.Sections(p.Slug); err == nil {
		v.Writer = sec.Writer
	}
	if p.SetNum != "" {
		if photos, err := s.legoDB.SetPhotos(p.SetNum); err == nil {
			for _, ph := range photos {
				if strings.HasPrefix(ph.Note, lego.PublicPhotoPrefix) {
					v.Photos++
				}
			}
		}
	}
	return v
}

// handleBlogList is GET /dashboard/blog?state=review (state optional).
func (s *Server) handleBlogList(w http.ResponseWriter, r *http.Request) {
	posts, err := s.legoDB.ListPosts(r.URL.Query().Get("state"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	out := make([]blogPostView, 0, len(posts))
	for _, p := range posts {
		out = append(out, s.postView(p, false))
	}
	writeJSON(w, http.StatusOK, map[string]any{"posts": out})
}

// handleBlogGet is GET /dashboard/blog/{slug}.
func (s *Server) handleBlogGet(w http.ResponseWriter, r *http.Request) {
	p, err := s.legoDB.GetPost(r.PathValue("slug"))
	if err != nil || p == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such post"})
		return
	}
	writeJSON(w, http.StatusOK, s.postView(*p, true))
}

type blogSaveRequest struct {
	Slug        string `json:"slug"`
	SetNum      string `json:"set_num"`
	Theme       string `json:"theme"`
	Title       string `json:"title"`
	Summary     string `json:"summary"`
	Body        string `json:"body"`
	Audio       string `json:"audio"`
	BuildStatus string `json:"build_status"`
	By          string `json:"by"`
}

// handleBlogSave is POST /dashboard/blog -- creates or updates (an edit of a live post takes it offline).
func (s *Server) handleBlogSave(w http.ResponseWriter, r *http.Request) {
	var req blogSaveRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 400<<10)).Decode(&req); err != nil || strings.TrimSpace(req.By) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "a post and by are required"})
		return
	}
	switch req.BuildStatus {
	case "", "waiting", "building", "counting", "checked":
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "build status must be waiting, building, counting or checked"})
		return
	}
	p, err := s.legoDB.SavePost(lego.Post{Slug: req.Slug, SetNum: req.SetNum, Theme: req.Theme, Title: req.Title, Summary: req.Summary,
		Body: req.Body, Audio: req.Audio, BuildStatus: req.BuildStatus}, req.By, "web", "")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(req.By, "BLOG_POST_SAVED", "SUCCESS", p.Slug)
	writeJSON(w, http.StatusOK, s.postView(*p, true))
}

type blogStateRequest struct {
	State string `json:"state"`
	By    string `json:"by"`
	Note  string `json:"note"`
}

// handleBlogState is POST /dashboard/blog/{slug}/state {state, by}.
func (s *Server) handleBlogState(w http.ResponseWriter, r *http.Request) {
	var req blogStateRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil || strings.TrimSpace(req.By) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "state and by are required"})
		return
	}
	slug := r.PathValue("slug")
	note := strings.TrimSpace(req.Note)
	sec, _ := s.legoDB.Sections(slug)
	if req.State == lego.PostReview || req.State == lego.PostPublished {
		if p, _ := s.legoDB.GetPost(slug); p != nil && p.SetNum != "" {
			if why := s.legoDB.BlogBlock(p.SetNum); why != "" {
				writeJSON(w, http.StatusConflict, map[string]string{"error": "the blog can't go ahead: " + why})
				return
			}
		}
	}
	switch req.State {
	case lego.PostReview:
		// a post written through the dashboard's steps goes for review only when every blocking check passes
		if sec != nil && sec.Exists {
			if missing := blogCheckNames(s.legoDB.ReadyChecks(slug, "")); missing != "" {
				writeJSON(w, http.StatusConflict, map[string]string{"error": "not ready to send: " + missing})
				return
			}
		}
	case lego.PostChanges, lego.PostRejected:
		if note == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "a note is required: tell the writer what to fix"})
			return
		}
	}
	if err := s.legoDB.SetPostState(slug, req.State, req.By); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	kind := map[string]string{lego.PostChanges: "changes", lego.PostRejected: "reject", lego.PostPublished: "approve", lego.PostReview: "sent"}[req.State]
	if kind != "" && (note != "" || kind == "approve") {
		if note == "" {
			note = "Approved."
		}
		_ = s.legoDB.AddBlogNote(slug, req.By, kind, note)
	}
	if msg := blogStateOutcome(slug, req.State, note); msg != "" && sec != nil && sec.Writer != "" && !strings.EqualFold(sec.Writer, req.By) {
		s.notifyUser(req.By, sec.Writer, msg)
	}
	if req.State == lego.PostReview {
		s.notifyWithPerm("sets.publish", req.By, fmt.Sprintf("Blog post %s is waiting for review.", slug))
	}
	s.logAudit(req.By, "BLOG_POST_"+strings.ToUpper(req.State), "SUCCESS", slug)
	writeJSON(w, http.StatusOK, map[string]string{"slug": slug, "state": req.State})
}
