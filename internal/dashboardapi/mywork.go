package dashboardapi

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

type myWorkView struct {
	ID         int64  `json:"id"`
	Target     string `json:"target"`
	Label      string `json:"label"`
	Status     string `json:"status"` // queued or claimed
	AssignedTo string `json:"assigned_to"`
	Mine       bool   `json:"mine"` // assigned to this person (false: open to anyone)
}

// handleMyWork is GET /dashboard/my-work?user=: the set checks a checker can work on. The phone walk only sees a ticket
// once it is claimed, and an admin-assigned check is queued until then, so this lists both: the claimed one first, then
// the ones assigned to them, then the open ones nobody was named for.
func (s *Server) handleMyWork(w http.ResponseWriter, r *http.Request) {
	user := strings.TrimSpace(r.URL.Query().Get("user"))
	if user == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "user is required"})
		return
	}
	open, err := s.legoDB.AllOpenTickets()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	out := []myWorkView{}
	for _, t := range open {
		if t.Kind != lego.TicketCheck {
			continue
		}
		mine := strings.EqualFold(t.AssignedTo, user)
		if mine || (t.AssignedTo == "" && t.Status == lego.TicketQueued) {
			out = append(out, myWorkView{ID: t.ID, Target: t.Target, Label: t.Label, Status: t.Status, AssignedTo: t.AssignedTo, Mine: mine})
		}
	}
	rank := func(v myWorkView) int {
		switch {
		case v.Status == lego.TicketClaimed:
			return 0
		case v.Mine:
			return 1
		}
		return 2
	}
	sort.SliceStable(out, func(i, j int) bool { return rank(out[i]) < rank(out[j]) })
	writeJSON(w, http.StatusOK, map[string]any{"tickets": out})
}

// handleMyWorkClaim is POST /dashboard/my-work/claim {id, by}: start a check, the same claim the terminal's Request
// screen makes. Only a ticket assigned to them or open to anyone, and only one at a time.
func (s *Server) handleMyWorkClaim(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID int64  `json:"id"`
		By string `json:"by"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&req); err != nil || req.ID == 0 || strings.TrimSpace(req.By) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id and by are required"})
		return
	}
	open, err := s.legoDB.AllOpenTickets()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	var tk *lego.Ticket
	for i := range open {
		if open[i].ID == req.ID {
			tk = &open[i]
		}
	}
	if tk == nil || tk.Kind != lego.TicketCheck {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "that check is not open any more"})
		return
	}
	if tk.AssignedTo != "" && !strings.EqualFold(tk.AssignedTo, req.By) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "that check is assigned to someone else"})
		return
	}
	if cur, _ := s.legoDB.CurrentTicket(req.By); cur != nil && cur.ID != tk.ID {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "finish your current job first (set " + cur.Target + ")"})
		return
	}
	if tk.Status != lego.TicketClaimed {
		if err := s.legoDB.ClaimTicket(tk.ID, req.By); err != nil {
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
	}
	s.logAudit(req.By, "CHECK_CLAIMED", "SUCCESS", tk.Target+" via dashboard")
	writeJSON(w, http.StatusOK, map[string]string{"target": tk.Target})
}
