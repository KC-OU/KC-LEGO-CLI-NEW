// Admin queue clean-up for the dashboard: list the open work tickets with their age, remove
// one (added by mistake), or sweep out everything still unclaimed after N days. The same
// lego.DB methods back the TUI screens, so the rules are identical on both.
package dashboardapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type queueTicketView struct {
	ID         int64  `json:"id"`
	Kind       string `json:"kind"`
	Target     string `json:"target"`
	Label      string `json:"label"`
	AssignedTo string `json:"assigned_to"`
	Status     string `json:"status"`
	CreatedAt  string `json:"created_at"`
	AgeHours   int    `json:"age_hours"`
}

// handleQueue is GET /dashboard/queue?days=N -- open tickets, plus how many unclaimed ones are older than N days.
func (s *Server) handleQueue(w http.ResponseWriter, r *http.Request) {
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days < 1 {
		days = 7
	}
	open, err := s.legoDB.AllOpenTickets()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	stale, err := s.legoDB.StaleQueuedTickets(time.Duration(days) * 24 * time.Hour)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	out := make([]queueTicketView, 0, len(open))
	for _, t := range open {
		out = append(out, queueTicketView{ID: t.ID, Kind: t.Kind, Target: t.Target, Label: t.Label, AssignedTo: t.AssignedTo,
			Status: t.Status, CreatedAt: t.CreatedAt.Format("2006-01-02 15:04"), AgeHours: int(time.Since(t.CreatedAt).Hours())})
	}
	writeJSON(w, http.StatusOK, map[string]any{"tickets": out, "days": days, "stale": len(stale)})
}

type queueCancelRequest struct {
	ID     int64  `json:"id"`
	Days   int    `json:"days"`
	Reason string `json:"reason"`
	By     string `json:"by"`
}

// handleQueueCancel is POST /dashboard/queue/cancel {id, reason, by}.
func (s *Server) handleQueueCancel(w http.ResponseWriter, r *http.Request) {
	var req queueCancelRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil || req.ID == 0 || strings.TrimSpace(req.By) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id and by are required"})
		return
	}
	t, err := s.legoDB.CancelTicket(req.ID, req.By, req.Reason)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(req.By, "TICKET_REMOVED", "SUCCESS", t.Label+": "+strings.TrimSpace(req.Reason))
	writeJSON(w, http.StatusOK, map[string]any{"removed": 1, "label": t.Label})
}

// handleQueueCancelStale is POST /dashboard/queue/cancel-stale {days, by}.
func (s *Server) handleQueueCancelStale(w http.ResponseWriter, r *http.Request) {
	var req queueCancelRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil || req.Days < 1 || strings.TrimSpace(req.By) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "days (1 or more) and by are required"})
		return
	}
	done, err := s.legoDB.CancelStaleQueuedTickets(time.Duration(req.Days)*24*time.Hour, req.By)
	for _, t := range done {
		s.logAudit(req.By, "TICKET_REMOVED", "SUCCESS", t.Label+": left in the queue too long")
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error(), "removed": len(done)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"removed": len(done)})
}
