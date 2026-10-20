// Admin-screen endpoints: open tickets, who's assigned what, status cards,
// assigning work, and reopening a finished check as a recount. All of this
// mirrors internal/mobileapi/admin.go's own handlers field-for-field and
// calls the exact same lego.DB/users.Service methods -- no new business
// logic, just a second thin HTTP skin over it for the dashboard.
//
// mobileapi's admin handlers gate each action on requireAdmin(sess), since
// they have a real logged-in lego.MobileSession to check. This package has
// no per-user session at all -- the shared bearer token (see
// Server.withToken) is the entire trust boundary, same as the Kanban/Sets/
// accuracy endpoints already work. A write here (assign/reopen/status card)
// takes the acting username as a plain "by" field in the request body for
// audit attribution, since there's no verified dashboard-session identity
// to read it from yet (the same BetterAuth-identity-has-no-Go-username gap
// noted on handleAccuracy).
package dashboardapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

type adminUserRow struct {
	Username, Role string
	// Status is "ACTIVE"/"DISABLED" -- added for the account-management
	// table; the "assign to" dropdown (the only other consumer) just
	// ignores it. Same first-system-wins simplification as Role already
	// had: a username registered in both ModernWMS and Part-DB with
	// differing status shows whichever system ListAll returned first.
	Status string
}

// handleUsers is the combined ModernWMS+Part-DB user list -- the same one
// mobileapi's "assign to" picker uses, now also the account-management
// table's data source.
func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request) {
	if s.usersSvc == nil {
		writeJSON(w, http.StatusOK, []adminUserRow{})
		return
	}
	rows, _ := s.usersSvc.ListAll(r.Context())
	seen := map[string]bool{}
	out := make([]adminUserRow, 0, len(rows))
	for _, row := range rows {
		if row.Username == "" || seen[row.Username] {
			continue
		}
		seen[row.Username] = true
		out = append(out, adminUserRow{Username: row.Username, Role: row.RoleOrGroup, Status: row.Status})
	}
	writeJSON(w, http.StatusOK, out)
}

type adminTicketRow struct {
	ID                int64  `json:"id"`
	Kind              string `json:"kind"`
	Target            string `json:"target"`
	Label             string `json:"label"`
	AssignedTo        string `json:"assigned_to"`
	Status            string `json:"status"`
	ClaimedAt         string `json:"claimed_at,omitempty"`
	ClaimedForSeconds int64  `json:"claimed_for_seconds,omitempty"`
	EstimatedSeconds  int64  `json:"estimated_seconds,omitempty"`
}

// handleTickets is every open or claimed ticket -- the admin queue, same
// data mobileapi's supervisor live-status view polls.
func (s *Server) handleTickets(w http.ResponseWriter, r *http.Request) {
	tickets, err := s.legoDB.AllOpenTickets()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	now := time.Now()
	paceByKind := map[string]int64{}
	out := make([]adminTicketRow, 0, len(tickets))
	for _, t := range tickets {
		row := adminTicketRow{ID: t.ID, Kind: t.Kind, Target: t.Target, Label: t.Label, AssignedTo: t.AssignedTo, Status: t.Status}
		if t.Status == lego.TicketClaimed && !t.ClaimedAt.IsZero() {
			row.ClaimedAt = t.ClaimedAt.Format(time.RFC3339)
			row.ClaimedForSeconds = int64(now.Sub(t.ClaimedAt).Seconds())
			if _, ok := paceByKind[t.Kind]; !ok {
				pace, _ := s.legoDB.EstimatePace(t.Kind, 20)
				paceByKind[t.Kind] = pace.AvgSeconds
			}
			row.EstimatedSeconds = paceByKind[t.Kind]
		}
		out = append(out, row)
	}
	writeJSON(w, http.StatusOK, out)
}

type statusCardRow struct {
	Username  string `json:"username"`
	Reason    string `json:"reason"`
	Name      string `json:"name"`
	Note      string `json:"note,omitempty"`
	CreatedBy string `json:"created_by"`
	StartedAt string `json:"started_at"`
}

func statusCardRowOf(c lego.StatusCard) statusCardRow {
	return statusCardRow{
		Username: c.Username, Reason: c.Reason, Name: lego.CardReasonName(c.Reason),
		Note: c.Note, CreatedBy: c.CreatedBy, StartedAt: c.StartedAt.Format(time.RFC3339),
	}
}

// handleStatusCards lists everyone currently on a Delay/No-Work card.
func (s *Server) handleStatusCards(w http.ResponseWriter, r *http.Request) {
	cards, err := s.legoDB.ActiveStatusCards()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	rows := make([]statusCardRow, len(cards))
	for i, c := range cards {
		rows[i] = statusCardRowOf(c)
	}
	writeJSON(w, http.StatusOK, rows)
}

type assignTicketRequest struct {
	Kind       string `json:"kind"`
	Target     string `json:"target"`
	Label      string `json:"label"`
	AssignedTo string `json:"assigned_to"`
	By         string `json:"by"`
}

// handleAssignTicket assigns (or reassigns) a check/order -- the same
// AssignTicket the TUI and mobile admin screens both call.
func (s *Server) handleAssignTicket(w http.ResponseWriter, r *http.Request) {
	var req assignTicketRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	req.Kind, req.Target, req.AssignedTo, req.By = strings.TrimSpace(req.Kind), strings.TrimSpace(req.Target), strings.TrimSpace(req.AssignedTo), strings.TrimSpace(req.By)
	if req.Kind != lego.TicketCheck && req.Kind != lego.TicketOrder || req.Target == "" || req.By == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "kind (check or order), target and by are required"})
		return
	}
	label := strings.TrimSpace(req.Label)
	if label == "" {
		label = s.ticketLabel(req.Kind, req.Target)
	}
	tk, err := s.legoDB.AssignTicket(req.Kind, req.Target, label, req.AssignedTo, "", "", req.By)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.AssignedTo != "" {
		_ = s.legoDB.SendMessage(req.By, req.AssignedTo, "You've been assigned: "+tk.Label)
	}
	s.logAudit(req.By, "DASHBOARD_TICKET_ASSIGNED", "SUCCESS", tk.Label+" -> "+orDash(req.AssignedTo)+" ("+tk.Kind+")")
	writeJSON(w, http.StatusOK, adminTicketRow{ID: tk.ID, Kind: tk.Kind, Target: tk.Target, Label: tk.Label, AssignedTo: tk.AssignedTo, Status: tk.Status})
}

func orDash(s string) string {
	if s == "" {
		return "the open queue"
	}
	return s
}

type reopenTicketRequest struct {
	Kind   string `json:"kind"`
	Target string `json:"target"`
	Reason string `json:"reason"`
	By     string `json:"by"`
}

// handleReopenTicket is the discrepancy-recheck action: a fresh, open
// ticket against the same set/order (lego.DB.ReopenTicket) -- claiming it
// is what makes the next check a recount.
func (s *Server) handleReopenTicket(w http.ResponseWriter, r *http.Request) {
	var req reopenTicketRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	req.Kind, req.Target, req.Reason, req.By = strings.TrimSpace(req.Kind), strings.TrimSpace(req.Target), strings.TrimSpace(req.Reason), strings.TrimSpace(req.By)
	if req.Kind != lego.TicketCheck && req.Kind != lego.TicketOrder || req.Target == "" || req.By == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "kind (check or order), target and by are required"})
		return
	}
	tk, err := s.legoDB.ReopenTicket(req.Kind, req.Target, s.ticketLabel(req.Kind, req.Target), req.Reason, req.By)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(req.By, "DASHBOARD_TICKET_REOPENED", "SUCCESS", tk.Label+" ("+tk.Kind+"): "+req.Reason)
	writeJSON(w, http.StatusOK, adminTicketRow{ID: tk.ID, Kind: tk.Kind, Target: tk.Target, Label: tk.Label, AssignedTo: tk.AssignedTo, Status: tk.Status})
}

// ticketLabel mirrors mobileapi's own helper of the same name.
func (s *Server) ticketLabel(kind, target string) string {
	if kind == lego.TicketCheck {
		if st, err := s.legoDB.GetSetByNum(target); err == nil && st != nil && st.Name != "" {
			return target + " " + st.Name
		}
		return target
	}
	if id, err := strconv.ParseInt(target, 10, 64); err == nil {
		if o, err := s.legoDB.GetOrder(id); err == nil && o != nil {
			return "Order #" + target + " (" + o.SupplierKind + " " + o.Supplier + ")"
		}
	}
	return target
}
