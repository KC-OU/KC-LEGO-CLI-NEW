// Dynamic set image updates: a thin skin over internal/lego.SetImageOverride,
// the one place every reader (TUI, CLI, mobile API, this dashboard's own
// GET /dashboard/sets/{setNum}) picks up an admin's replacement picture --
// see lego.DB.CatalogSet. A change here is visible everywhere on the very
// next read, live, with no separate propagation step needed.
package dashboardapi

import (
	"encoding/json"
	"net/http"
	"strings"
)

type setImageRequest struct {
	SetNum   string `json:"set_num"`
	ImageURL string `json:"image_url"`
	By       string `json:"by"`
}

// handleSetImage sets (or, with a blank image_url, clears) a set's image
// override.
func (s *Server) handleSetImage(w http.ResponseWriter, r *http.Request) {
	var req setImageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	req.SetNum, req.By = strings.TrimSpace(req.SetNum), strings.TrimSpace(req.By)
	if req.SetNum == "" || req.By == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "set_num and by are required"})
		return
	}
	if err := s.legoDB.SetImageOverride(req.SetNum, req.ImageURL); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(req.By, "DASHBOARD_SET_IMAGE_UPDATED", "SUCCESS", req.SetNum)
	writeJSON(w, http.StatusOK, map[string]any{"set": req.SetNum, "image_url": req.ImageURL})
}
