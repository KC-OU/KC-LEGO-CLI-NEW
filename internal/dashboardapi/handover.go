// Shift Handover Note -- the single free-text note left for whoever's on
// next, same as internal/uiapp/handover_screen.go.
package dashboardapi

import (
	"encoding/json"
	"net/http"
	"strings"
)

// handleGetHandover returns the current note, or an empty one if nobody's
// left one yet -- GetHandoverNote returns (nil, nil) for that case, so this
// normalizes it to a zero-value note rather than null.
func (s *Server) handleGetHandover(w http.ResponseWriter, r *http.Request) {
	note, err := s.legoDB.GetHandoverNote()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if note == nil {
		writeJSON(w, http.StatusOK, map[string]string{"body": "", "created_by": ""})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"body": note.Body, "created_by": note.CreatedBy, "created_at": note.CreatedAt})
}

type setHandoverRequest struct {
	Body string `json:"body"`
	By   string `json:"by"`
}

// handleSetHandover replaces the note -- an empty body clears it, same as
// the TUI's own screen (SetHandoverNote has no separate "clear" call).
func (s *Server) handleSetHandover(w http.ResponseWriter, r *http.Request) {
	var req setHandoverRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	req.By = strings.TrimSpace(req.By)
	if req.By == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "by is required"})
		return
	}
	if err := s.legoDB.SetHandoverNote(req.Body, req.By); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(req.By, "DASHBOARD_HANDOVER_SET", "SUCCESS", "")
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}
