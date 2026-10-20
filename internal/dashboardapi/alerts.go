// Admin -> Alerts: review a 21+-missing check/order flagged for manual
// review and file a report against it. Mirrors
// internal/uiapp/alerts_screen.go's accuracyReportScreen exactly (same
// FileAccuracyReport call, same reports.AccuracyReportPDF + exports.Save/
// NewLink round trip for a downloadable copy, same SendMessage-if-talk-
// requested and LogEvent/notify.Dispatch pair) -- a second thin skin over
// the same business logic, not a new reporting system.
package dashboardapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/exports"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/notify"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/reports"
)

// handleAlerts lists every open accuracy escalation with no report filed
// against it yet -- the same feed the TUI's own Admin -> Alerts shows.
func (s *Server) handleAlerts(w http.ResponseWriter, r *http.Request) {
	open, err := s.legoDB.OpenAccuracyEscalations()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"alerts": open})
}

type fileAccuracyReportRequest struct {
	EscalationID  int64  `json:"escalation_id"`
	Summary       string `json:"summary"`
	ActionTaken   string `json:"action_taken"`
	TalkRequested bool   `json:"talk_requested"`
	By            string `json:"by"`
}

// handleFileAccuracyReport reviews one open escalation and files a report
// against it -- the escalation drops off handleAlerts's list as a side
// effect of FileAccuracyReport, same as the TUI.
func (s *Server) handleFileAccuracyReport(w http.ResponseWriter, r *http.Request) {
	var req fileAccuracyReportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	req.Summary, req.ActionTaken, req.By = strings.TrimSpace(req.Summary), strings.TrimSpace(req.ActionTaken), strings.TrimSpace(req.By)
	if req.EscalationID == 0 || req.Summary == "" || req.ActionTaken == "" || req.By == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "escalation_id, summary, action_taken and by are required"})
		return
	}
	e, err := s.legoDB.AccuracyEscalationByID(req.EscalationID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "that alert is no longer open"})
		return
	}
	if _, err := s.legoDB.FileAccuracyReport(e.ID, e.Username, e.Role, req.By, req.Summary, req.ActionTaken, req.TalkRequested); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	downloadURL := savedAccuracyReportURL(req.By, e, req.Summary, req.ActionTaken, req.TalkRequested)
	if req.TalkRequested {
		_ = s.legoDB.SendMessage(req.By, e.Username, "Your manager would like to discuss a recent accuracy report with you.")
	}
	_ = s.legoDB.LogEvent(lego.EventAccuracyReportFiled, req.By, e.Username, req.Summary)
	notify.Dispatch(lego.EventAccuracyReportFiled, fmt.Sprintf("Accuracy report filed for %s by %s", e.Username, req.By), "")
	s.logAudit(req.By, "ACCURACY_REPORT_FILED", "SUCCESS", fmt.Sprintf("%s (%s): %s", e.Username, e.Role, req.Summary))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "download_url": downloadURL})
}

// savedAccuracyReportURL mirrors internal/uiapp/alerts_screen.go's own
// saveAccuracyReportPDF -- "" if it couldn't be saved or WMS_PUBLIC_URL
// isn't set; the report is filed either way, the PDF is a convenience
// copy, not the record of truth.
func savedAccuracyReportURL(by string, e lego.AccuracyEscalation, summary, action string, talk bool) string {
	pdf := reports.AccuracyReportPDF(reports.AccuracyReportData{
		Username: e.Username, Role: e.Role, ReviewedBy: by,
		Target: e.Target, Missing: e.Missing,
		Summary: summary, ActionTaken: action, TalkRequested: talk,
		CreatedAt: time.Now(),
	})
	path, err := exports.Save(exports.Dir(), by, "accuracy-report", e.Username, "pdf", pdf)
	if err != nil {
		return ""
	}
	tok, err := exports.NewLink(exports.Dir(), path, by)
	if err != nil {
		return ""
	}
	return exports.URL(tok)
}

// handleAccuracyReportsFor is one person's filed report history -- the
// permanent record any admin can pull up, same as the TUI's own
// Admin -> Alerts -> "view past reports" path.
func (s *Server) handleAccuracyReportsFor(w http.ResponseWriter, r *http.Request) {
	username := r.PathValue("username")
	role := r.URL.Query().Get("role")
	if role == "" {
		role = lego.AccuracyChecker
	}
	filed, err := s.legoDB.AccuracyReportsFor(username, role)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"reports": filed})
}
