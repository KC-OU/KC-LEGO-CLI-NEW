// Recent Activity (the admin event feed shown on the TUI's own Admin >
// Recent Activity screen), the tamper-evident audit log's verify/tail view,
// and Docker container status -- three small, unrelated read-mostly admin
// surfaces grouped in one file because each is a handful of lines wrapping
// a single internal/lego, internal/audit, or internal/docker call.
package dashboardapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/audit"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/docker"
)

// handleActivity lists the most recent admin events (account changes,
// assignments, overrides -- anything LogEvent'd), newest first, the same
// feed internal/uiapp/activity.go shows. ?limit= caps it (default 50).
func (s *Server) handleActivity(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}
	events, err := s.legoDB.AdminEvents(limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

type clearActivityRequest struct {
	By string `json:"by"`
}

// handleClearActivity wipes the feed entirely -- irreversible, same as the
// TUI's own clear action, which is exactly why it's logged to the audit
// trail (a different log) before it deletes the activity one.
func (s *Server) handleClearActivity(w http.ResponseWriter, r *http.Request) {
	var req clearActivityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	req.By = strings.TrimSpace(req.By)
	if req.By == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "by is required"})
		return
	}
	if err := s.legoDB.ClearAdminEvents(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(req.By, "DASHBOARD_ACTIVITY_CLEARED", "SUCCESS", "")
	writeJSON(w, http.StatusOK, map[string]bool{"cleared": true})
}

// handleAuditLog is the tamper-evident chain's own read-only view: the last
// ?n= lines (default 50, hashes stripped, same as internal/audit.Tail) plus
// a chain-integrity verdict (internal/audit.Verify) -- the dashboard's
// equivalent of `wms audit head`/`wms audit verify`.
func (s *Server) handleAuditLog(w http.ResponseWriter, r *http.Request) {
	n := 50
	if raw := r.URL.Query().Get("n"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			n = v
		}
	}
	l := audit.New()
	lines, err := l.Tail(n)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	report, err := l.Verify()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"lines": lines, "report": report, "ok": report.OK()})
}

// handleContainers is the read-only `docker ps` table the TUI's Admin >
// Containers screen shows -- internal/docker.Ps returns pre-formatted text
// (no structured parser exists), so this passes it through as-is rather
// than inventing a schema for it.
func (s *Server) handleContainers(w http.ResponseWriter, r *http.Request) {
	out, err := docker.Ps(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"output": out})
}
