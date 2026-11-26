// Named download slots for the dashboard's Downloads page: a display name
// (e.g. "Lego Set Checklist Front Sheet") mapped to a Teedy document URL an
// admin pasted in. The actual file lives in Teedy (already deployed
// separately for exactly this) -- this store only remembers which Teedy
// document is "the current one" for each slot, same JSON-file pattern as
// currencies.go/i18n.go.
//
// Each slot also carries a Section: swapping a PDF for an updated version
// doesn't need either -- upload the new version onto the SAME Teedy
// document and this slot's url keeps pointing at it. Section exists for
// grouping instead: "current" is the default and always rendered first,
// as an open list (see render_downloads_page on the Rust side); any other
// name is free text an admin invents (e.g. "Guides", "Labels", "Archive")
// and renders as its own named, collapsed group. Removing a slot here
// never touches the file in Teedy.
package dashboardapi

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

// downloadSectionCurrent is the one section name treated specially: the
// default for a blank Section field, and always rendered first as an open
// list rather than a collapsed named group.
const downloadSectionCurrent = "current"

type downloadSlot struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	Section   string `json:"section"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// normalizeDownloadSection trims and lowercases a requested section
// (case-folded so "Guides"/"guides"/"GUIDES" land in the same group
// instead of splitting into three), and defaults a blank one to
// "current" -- both for new requests and for slots saved by a version of
// this file before Section existed. Any non-empty name is accepted: an
// admin can invent as many sections as they like.
func normalizeDownloadSection(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return downloadSectionCurrent
	}
	return s
}

type downloadsStore struct {
	path string
	mu   sync.Mutex
}

func newDownloadsStore(path string) *downloadsStore {
	if path == "" {
		path = downloadsFile()
	}
	return &downloadsStore{path: path}
}

func downloadsFile() string {
	if p := config.Get(config.DownloadsFile); p != "" {
		return p
	}
	return filepath.Join(filepath.Dir(config.Get(config.LegoDBPath)), "downloads.json")
}

func (s *downloadsStore) load() ([]downloadSlot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return []downloadSlot{}, nil
	}
	if err != nil {
		return nil, err
	}
	var out []downloadSlot
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Section = normalizeDownloadSection(out[i].Section)
	}
	return out, nil
}

func (s *downloadsStore) save(slots []downloadSlot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(slots, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0644)
}

// handleListDownloads is GET /dashboard/downloads -- open to every signed-in
// user, same as the dashboard's Downloads page itself.
func (s *Server) handleListDownloads(w http.ResponseWriter, r *http.Request) {
	slots, err := s.downloads.load()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"slots": slots})
}

type downloadSlotRequest struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	Section string `json:"section"`
	By      string `json:"by"`
}

// handleSetDownload is POST /dashboard/downloads/set -- upserts by name
// (admin-only, enforced on the Rust side like every other admin action).
func (s *Server) handleSetDownload(w http.ResponseWriter, r *http.Request) {
	var req downloadSlotRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.URL = strings.TrimSpace(req.URL)
	req.By = strings.TrimSpace(req.By)
	if req.Name == "" || req.URL == "" || req.By == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name, url and by are all required"})
		return
	}
	section := normalizeDownloadSection(req.Section)
	slots, err := s.downloads.load()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	now := time.Now().UTC().Format("2006-01-02 15:04")
	found := false
	for i := range slots {
		if slots[i].Name == req.Name {
			slots[i].URL = req.URL
			slots[i].Section = section
			slots[i].UpdatedAt = now
			found = true
			break
		}
	}
	if !found {
		slots = append(slots, downloadSlot{Name: req.Name, URL: req.URL, Section: section, UpdatedAt: now})
	}
	if err := s.downloads.save(slots); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(req.By, "DASHBOARD_DOWNLOAD_SET", "SUCCESS", req.Name+" ["+section+"]")
	writeJSON(w, http.StatusOK, map[string]any{"slots": slots})
}

type removeDownloadRequest struct {
	Name string `json:"name"`
	By   string `json:"by"`
}

// handleRemoveDownload is POST /dashboard/downloads/remove.
func (s *Server) handleRemoveDownload(w http.ResponseWriter, r *http.Request) {
	var req removeDownloadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.By = strings.TrimSpace(req.By)
	if req.Name == "" || req.By == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name and by are required"})
		return
	}
	slots, err := s.downloads.load()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	kept := make([]downloadSlot, 0, len(slots))
	found := false
	for _, sl := range slots {
		if sl.Name == req.Name {
			found = true
			continue
		}
		kept = append(kept, sl)
	}
	if !found {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no such download slot"})
		return
	}
	if err := s.downloads.save(kept); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(req.By, "DASHBOARD_DOWNLOAD_REMOVED", "SUCCESS", req.Name)
	writeJSON(w, http.StatusOK, map[string]any{"slots": kept})
}
