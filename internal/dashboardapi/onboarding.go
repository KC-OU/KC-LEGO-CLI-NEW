// Set-check onboarding: an admin-editable checklist a user acknowledges
// before starting a check on the dashboard (see the "Start check" button
// on the Sets page), plus the submission record an admin can later review
// -- directly from the user's own hand-drawn onboarding-page design this
// batch was built from. Deliberately its own small store rather than
// reusing internal/lego.Proof: a Proof is hard-tied to a set_checks row
// and a byte-sniffed local image/PDF file, neither of which fits an
// onboarding acknowledgement (see this batch's plan for the full
// reasoning). The admin-review shape (lookup by set -> table -> an
// action form) is deliberately cloned from the existing Paper-copy
// verification panel (proofs.go) instead.
package dashboardapi

import (
	"crypto/rand"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/teedy"
)

type onboardingItem struct {
	ID       string `json:"id"`
	Text     string `json:"text"`
	Required bool   `json:"required"`
}

type onboardingAnswer struct {
	ItemID string `json:"item_id"`
	Value  string `json:"value"` // "yes" | "no" | "na"
}

type onboardingSubmission struct {
	ID             string
	SetNum         string
	By             string
	SubmittedAt    time.Time
	Answers        []onboardingAnswer
	TeedyFilename  string
	UploadVerified bool
	Status         string // "submitted" | "reviewed" | "flagged"
	ReviewNote     string
}

// onboardingSubmissionView is the JSON shape actually sent over the wire
// -- SubmittedAt formatted as a plain display string, same convention as
// proofs.go's proofRow.UploadedAt, rather than time.Time's own JSON
// marshaling.
type onboardingSubmissionView struct {
	ID             string             `json:"id"`
	SetNum         string             `json:"set_num"`
	By             string             `json:"by"`
	SubmittedAt    string             `json:"submitted_at"`
	Answers        []onboardingAnswer `json:"answers"`
	TeedyFilename  string             `json:"teedy_filename,omitempty"`
	UploadVerified bool               `json:"upload_verified"`
	Status         string             `json:"status"`
	ReviewNote     string             `json:"review_note,omitempty"`
}

func (s onboardingSubmission) toView() onboardingSubmissionView {
	return onboardingSubmissionView{
		ID: s.ID, SetNum: s.SetNum, By: s.By, SubmittedAt: s.SubmittedAt.Format("2006-01-02 15:04"),
		Answers: s.Answers, TeedyFilename: s.TeedyFilename, UploadVerified: s.UploadVerified,
		Status: s.Status, ReviewNote: s.ReviewNote,
	}
}

type onboardingStore struct {
	path string
	mu   sync.Mutex
}

func newOnboardingStore(path string) *onboardingStore {
	if path == "" {
		path = onboardingFile()
	}
	return &onboardingStore{path: path}
}

func onboardingFile() string {
	if p := config.Get(config.OnboardingFile); p != "" {
		return p
	}
	return filepath.Join(filepath.Dir(config.Get(config.LegoDBPath)), "onboarding.json")
}

type onboardingData struct {
	Items       []onboardingItem       `json:"items"`
	Submissions []onboardingSubmission `json:"submissions"`
	// UsedGuestTokens is every guest-upload-link nonce that's already
	// been consumed (see guest_upload.go) -- the token itself is
	// self-verifying for signature/expiry, this is the only part that
	// needs server-side state.
	UsedGuestTokens []string `json:"used_guest_tokens,omitempty"`
}

// defaultOnboardingItems seeds a brand-new store with the checklist from
// the user's own hand-drawn design -- an admin can edit/remove/add to
// these afterward, they're just a reasonable starting point rather than
// an empty list on first deploy.
func defaultOnboardingItems() []onboardingItem {
	return []onboardingItem{
		{ID: "paper-check-done", Text: "Have you completed the paper check?", Required: true},
		{ID: "set-info-filled", Text: "Set information (number, ID, date, pieces, theme, storage/box ID, condition) filled in correctly", Required: true},
		{ID: "cost-filled", Text: "Cost of the set when bought (new/used) filled in", Required: true},
		{ID: "counts-filled", Text: "Counts (parts missing, parts sold, minifigs got, minifigs missing) filled in", Required: true},
		{ID: "extra-parts", Text: "Extra parts recorded, or marked N/A if none", Required: true},
		{ID: "missing-parts", Text: "Missing parts recorded, or marked N/A if the set is complete", Required: true},
		{ID: "shipping-total", Text: "Shipping cost added if parts need ordering, or marked N/A", Required: true},
		{ID: "grand-total", Text: "Grand total added, or marked N/A", Required: true},
		{ID: "notes", Text: "Notes added if you have any", Required: false},
		{ID: "signed-off", Text: "Signed, printed name and dated", Required: true},
	}
}

func (s *onboardingStore) load() (onboardingData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return onboardingData{Items: defaultOnboardingItems()}, nil
	}
	if err != nil {
		return onboardingData{}, err
	}
	var out onboardingData
	if err := json.Unmarshal(data, &out); err != nil {
		return onboardingData{}, err
	}
	return out, nil
}

func (s *onboardingStore) save(d onboardingData) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0644)
}

// handleListOnboardingItems is GET /dashboard/onboarding/items -- open to
// every signed-in user, they need the checklist to fill it out.
func (s *Server) handleListOnboardingItems(w http.ResponseWriter, r *http.Request) {
	d, err := s.onboarding.load()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": d.Items})
}

type onboardingItemRequest struct {
	ID       string `json:"id"`
	Text     string `json:"text"`
	Required bool   `json:"required"`
	By       string `json:"by"`
}

// handleSetOnboardingItem is POST /dashboard/onboarding/items/set --
// upserts by id (admin-only, enforced on the Rust side).
func (s *Server) handleSetOnboardingItem(w http.ResponseWriter, r *http.Request) {
	var req onboardingItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	req.ID = strings.TrimSpace(req.ID)
	req.Text = strings.TrimSpace(req.Text)
	req.By = strings.TrimSpace(req.By)
	if req.ID == "" || req.Text == "" || req.By == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id, text and by are all required"})
		return
	}
	d, err := s.onboarding.load()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	found := false
	for i := range d.Items {
		if d.Items[i].ID == req.ID {
			d.Items[i].Text = req.Text
			d.Items[i].Required = req.Required
			found = true
			break
		}
	}
	if !found {
		d.Items = append(d.Items, onboardingItem{ID: req.ID, Text: req.Text, Required: req.Required})
	}
	if err := s.onboarding.save(d); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(req.By, "DASHBOARD_ONBOARDING_ITEM_SET", "SUCCESS", req.ID)
	writeJSON(w, http.StatusOK, map[string]any{"items": d.Items})
}

type removeOnboardingItemRequest struct {
	ID string `json:"id"`
	By string `json:"by"`
}

// handleRemoveOnboardingItem is POST /dashboard/onboarding/items/remove.
func (s *Server) handleRemoveOnboardingItem(w http.ResponseWriter, r *http.Request) {
	var req removeOnboardingItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	req.ID = strings.TrimSpace(req.ID)
	req.By = strings.TrimSpace(req.By)
	if req.ID == "" || req.By == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id and by are required"})
		return
	}
	d, err := s.onboarding.load()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	kept := make([]onboardingItem, 0, len(d.Items))
	found := false
	for _, it := range d.Items {
		if it.ID == req.ID {
			found = true
			continue
		}
		kept = append(kept, it)
	}
	if !found {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no such checklist item"})
		return
	}
	d.Items = kept
	if err := s.onboarding.save(d); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(req.By, "DASHBOARD_ONBOARDING_ITEM_REMOVED", "SUCCESS", req.ID)
	writeJSON(w, http.StatusOK, map[string]any{"items": d.Items})
}

type submitOnboardingRequest struct {
	SetNum        string             `json:"set_num"`
	By            string             `json:"by"`
	Answers       []onboardingAnswer `json:"answers"`
	TeedyFilename string             `json:"teedy_filename"`
}

// handleSubmitOnboarding is POST /dashboard/onboarding/submit -- any
// signed-in user. Re-validates every required item server-side (defense
// in depth past the client-side block -- a raw POST could otherwise skip
// it entirely) before recording the submission.
func (s *Server) handleSubmitOnboarding(w http.ResponseWriter, r *http.Request) {
	var req submitOnboardingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	req.SetNum = strings.TrimSpace(req.SetNum)
	req.By = strings.TrimSpace(req.By)
	if req.SetNum == "" || req.By == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "set_num and by are required"})
		return
	}
	d, err := s.onboarding.load()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	answered := make(map[string]string, len(req.Answers))
	for _, a := range req.Answers {
		answered[a.ItemID] = a.Value
	}
	for _, item := range d.Items {
		if item.Required && answered[item.ID] == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "required item not answered: " + item.Text})
			return
		}
	}
	verified := false
	if req.TeedyFilename != "" {
		if tc := teedy.New(); tc != nil {
			if ok, terr := tc.FindByTitleSubstring(req.TeedyFilename); terr == nil {
				verified = ok
			}
		}
	}
	sub := onboardingSubmission{
		ID:             rand.Text(),
		SetNum:         req.SetNum,
		By:             req.By,
		SubmittedAt:    time.Now().UTC(),
		Answers:        req.Answers,
		TeedyFilename:  req.TeedyFilename,
		UploadVerified: verified,
		Status:         "submitted",
	}
	d.Submissions = append(d.Submissions, sub)
	if err := s.onboarding.save(d); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(req.By, "DASHBOARD_ONBOARDING_SUBMITTED", "SUCCESS", req.SetNum)
	writeJSON(w, http.StatusOK, map[string]any{"submission": sub.toView()})
}

// handleLookupOnboardingSubmissions is GET
// /dashboard/onboarding/submissions?set=... -- admin-only, mirrors
// handleProofsForSet's lookup-by-set shape.
func (s *Server) handleLookupOnboardingSubmissions(w http.ResponseWriter, r *http.Request) {
	setNum := strings.TrimSpace(r.URL.Query().Get("set"))
	if setNum == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "set is required"})
		return
	}
	d, err := s.onboarding.load()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	matched := make([]onboardingSubmissionView, 0)
	for _, sub := range d.Submissions {
		if sub.SetNum == setNum {
			matched = append(matched, sub.toView())
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"submissions": matched})
}

type reviewOnboardingRequest struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Note   string `json:"note"`
	By     string `json:"by"`
}

// handleReviewOnboardingSubmission is POST
// /dashboard/onboarding/submissions/review -- admin-only.
func (s *Server) handleReviewOnboardingSubmission(w http.ResponseWriter, r *http.Request) {
	var req reviewOnboardingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	req.ID = strings.TrimSpace(req.ID)
	req.Status = strings.TrimSpace(req.Status)
	req.By = strings.TrimSpace(req.By)
	if req.ID == "" || req.By == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id and by are required"})
		return
	}
	if req.Status != "reviewed" && req.Status != "flagged" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "status must be reviewed or flagged"})
		return
	}
	d, err := s.onboarding.load()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	found := false
	for i := range d.Submissions {
		if d.Submissions[i].ID == req.ID {
			d.Submissions[i].Status = req.Status
			d.Submissions[i].ReviewNote = req.Note
			found = true
			break
		}
	}
	if !found {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no such submission"})
		return
	}
	if err := s.onboarding.save(d); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(req.By, "DASHBOARD_ONBOARDING_REVIEWED", "SUCCESS", req.ID+" -> "+req.Status)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleVerifyOnboardingUpload is GET
// /dashboard/onboarding/verify-upload?filename=... -- any signed-in user,
// called by the onboarding popup's "Verify upload" step.
func (s *Server) handleVerifyOnboardingUpload(w http.ResponseWriter, r *http.Request) {
	filename := strings.TrimSpace(r.URL.Query().Get("filename"))
	if filename == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "filename is required"})
		return
	}
	tc := teedy.New()
	if tc == nil {
		writeJSON(w, http.StatusOK, map[string]any{"configured": false, "found": false})
		return
	}
	found, err := tc.FindByTitleSubstring(filename)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "could not reach Teedy: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"configured": true, "found": found})
}
