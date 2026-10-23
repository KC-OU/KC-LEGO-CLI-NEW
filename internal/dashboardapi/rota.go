// Weekly Rota -- view/set/override/clear per-user shifts, mirroring
// internal/uiapp/rota_screen.go exactly (same four lego.DB calls, no new
// scheduling logic).
package dashboardapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// handleRotaWeek returns every rota entry for each of 7 days starting
// ?start= (YYYY-MM-DD, required) -- the same per-date loop the TUI's
// get-week view does, since RotaForDate only takes one date at a time.
func (s *Server) handleRotaWeek(w http.ResponseWriter, r *http.Request) {
	start := r.URL.Query().Get("start")
	startDate, err := time.Parse("2006-01-02", start)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "start must be YYYY-MM-DD"})
		return
	}
	days := make(map[string]any, 7)
	for i := 0; i < 7; i++ {
		date := startDate.AddDate(0, 0, i).Format("2006-01-02")
		entries, err := s.legoDB.RotaForDate(date)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		days[date] = entries
	}
	writeJSON(w, http.StatusOK, map[string]any{"days": days})
}

type setRotaRequest struct {
	Username string `json:"username"`
	Date     string `json:"date"`
	Start    string `json:"start"`
	End      string `json:"end"`
	Note     string `json:"note"`
	By       string `json:"by"`
}

func (s *Server) handleSetRota(w http.ResponseWriter, r *http.Request) {
	var req setRotaRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	req.Username, req.Date, req.By = strings.TrimSpace(req.Username), strings.TrimSpace(req.Date), strings.TrimSpace(req.By)
	if req.Username == "" || req.Date == "" || req.By == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username, date and by are required"})
		return
	}
	if err := s.legoDB.SetRota(req.Username, req.Date, req.Start, req.End, req.Note, req.By); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(req.By, "DASHBOARD_ROTA_SET", "SUCCESS", req.Username+" "+req.Date)
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

type rotaDateRequest struct {
	Username string `json:"username"`
	Date     string `json:"date"`
	By       string `json:"by"`
}

// handleRotaOverride is emergency cover: username works date even though
// they're not on the rota for it, flagged EmergencyOverride=true.
func (s *Server) handleRotaOverride(w http.ResponseWriter, r *http.Request) {
	var req rotaDateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	req.Username, req.Date, req.By = strings.TrimSpace(req.Username), strings.TrimSpace(req.Date), strings.TrimSpace(req.By)
	if req.Username == "" || req.Date == "" || req.By == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username, date and by are required"})
		return
	}
	if err := s.legoDB.QuickNSOverride(req.Username, req.Date, req.By); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(req.By, "DASHBOARD_ROTA_OVERRIDE", "SUCCESS", req.Username+" "+req.Date)
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

func (s *Server) handleRotaClear(w http.ResponseWriter, r *http.Request) {
	var req rotaDateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	req.Username, req.Date, req.By = strings.TrimSpace(req.Username), strings.TrimSpace(req.Date), strings.TrimSpace(req.By)
	if req.Username == "" || req.Date == "" || req.By == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username, date and by are required"})
		return
	}
	if err := s.legoDB.ClearRota(req.Username, req.Date); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(req.By, "DASHBOARD_ROTA_CLEARED", "SUCCESS", req.Username+" "+req.Date)
	writeJSON(w, http.StatusOK, map[string]bool{"cleared": true})
}
