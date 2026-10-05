package mobileapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

// isAdmin is the mobile equivalent of the TUI's "user_mgmt" module gate
// (internal/uiapp/access.go's modulePerm — the same permission "Assign Work"
// needs there). A governed user (one with a policy entry) is checked the
// same way the TUI checks them; an ungoverned one falls back to their role
// label, same spirit as access.go's legacyCan(IsAdmin) but without a
// MobileSession carrying a full auth.Permissions struct to ask.
func (s *Server) isAdmin(sess lego.MobileSession) bool {
	if p, err := access.Load(); err == nil {
		if _, governed := p.Users[access.Key(sess.Source, sess.Username)]; governed {
			return p.Effective(sess.Source, sess.Username).Can("users.view")
		}
	}
	return strings.EqualFold(sess.Role, "Admin")
}

func (s *Server) requireAdmin(w http.ResponseWriter, sess lego.MobileSession, action string) bool {
	if s.isAdmin(sess) {
		return true
	}
	s.audit.Log(sess.Username, sess.Role, "DENIED_PERMISSION", "DENIED", action+" needs an admin")
	writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin only"})
	return false
}

type adminUserRow struct {
	Username, Role string
}

// handleAdminUsers backs the "assign to" picker — the same combined
// ModernWMS+Part-DB user list internal/uiapp/tickets_screens.go's
// assignWhoPick builds, trimmed to what a picker UI needs.
func (s *Server) handleAdminUsers(w http.ResponseWriter, r *http.Request, sess lego.MobileSession) {
	if !s.requireAdmin(w, sess, "ADMIN_LIST_USERS") {
		return
	}
	if s.users == nil { // tests that build a Server literal directly; see throttle's own nil check above
		writeJSON(w, http.StatusOK, []adminUserRow{})
		return
	}
	rows, _ := s.users.ListAll(r.Context())
	seen := map[string]bool{}
	out := make([]adminUserRow, 0, len(rows))
	for _, row := range rows {
		if row.Username == "" || seen[row.Username] {
			continue
		}
		seen[row.Username] = true
		out = append(out, adminUserRow{Username: row.Username, Role: row.RoleOrGroup})
	}
	writeJSON(w, http.StatusOK, out)
}

type adminTicketRow struct {
	ID         int64  `json:"id"`
	Kind       string `json:"kind"`
	Target     string `json:"target"`
	Label      string `json:"label"`
	AssignedTo string `json:"assigned_to"`
	Status     string `json:"status"`
	ClaimedAt  string `json:"claimed_at,omitempty"`
	// ClaimedForSeconds is how long the ticket has been claimed, for a
	// supervisor's "who's doing what, and for how long" view — computed
	// here rather than left to the client so every client agrees on "now".
	ClaimedForSeconds int64 `json:"claimed_for_seconds,omitempty"`
}

// handleAdminTickets lists every open or claimed ticket — the admin queue
// screen's AllOpenTickets, exposed to the phone. Doubles as the supervisor
// live-status view's data source ("who's on what, right now"): the client
// polls this on an interval rather than the server pushing, same as every
// other mobile screen in this API.
func (s *Server) handleAdminTickets(w http.ResponseWriter, r *http.Request, sess lego.MobileSession) {
	if !s.requireAdmin(w, sess, "ADMIN_LIST_TICKETS") {
		return
	}
	tickets, err := s.legoDB.AllOpenTickets()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	now := time.Now()
	out := make([]adminTicketRow, 0, len(tickets))
	for _, t := range tickets {
		row := adminTicketRow{ID: t.ID, Kind: t.Kind, Target: t.Target, Label: t.Label, AssignedTo: t.AssignedTo, Status: t.Status}
		if t.Status == lego.TicketClaimed && !t.ClaimedAt.IsZero() {
			row.ClaimedAt = t.ClaimedAt.Format(time.RFC3339)
			row.ClaimedForSeconds = int64(now.Sub(t.ClaimedAt).Seconds())
		}
		out = append(out, row)
	}
	writeJSON(w, http.StatusOK, out)
}

type assignTicketRequest struct {
	Kind       string `json:"kind"`
	Target     string `json:"target"`
	Label      string `json:"label"`
	AssignedTo string `json:"assigned_to"`
}

// handleAdminAssignTicket is "Admin: assign jobs from the phone" — the same
// AssignTicket the TUI's assignWhoPick screen calls, plus the same
// you've-been-assigned message, just reached from a phone instead of a
// terminal. No new business logic: this is wiring, not a new way to assign
// work.
func (s *Server) handleAdminAssignTicket(w http.ResponseWriter, r *http.Request, sess lego.MobileSession) {
	if !s.requireAdmin(w, sess, "ADMIN_ASSIGN_TICKET") {
		return
	}
	var req assignTicketRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	req.Kind, req.Target, req.AssignedTo = strings.TrimSpace(req.Kind), strings.TrimSpace(req.Target), strings.TrimSpace(req.AssignedTo)
	if req.Kind != lego.TicketCheck && req.Kind != lego.TicketOrder || req.Target == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "kind (check or order) and target are required"})
		return
	}
	label := strings.TrimSpace(req.Label)
	if label == "" {
		label = s.ticketLabel(req.Kind, req.Target)
	}
	tk, err := s.legoDB.AssignTicket(req.Kind, req.Target, label, req.AssignedTo, "", "", sess.Username)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.AssignedTo != "" {
		_ = s.legoDB.SendMessage(sess.Username, req.AssignedTo, "You've been assigned: "+tk.Label)
	}
	s.audit.Log(sess.Username, sess.Role, "TICKET_ASSIGNED", "SUCCESS", tk.Label+" -> "+orDash(req.AssignedTo)+" ("+tk.Kind+")")
	writeJSON(w, http.StatusOK, adminTicketRow{ID: tk.ID, Kind: tk.Kind, Target: tk.Target, Label: tk.Label, AssignedTo: tk.AssignedTo, Status: tk.Status})
}

type reopenTicketRequest struct {
	Kind   string `json:"kind"`
	Target string `json:"target"`
	Reason string `json:"reason"`
}

// handleAdminReopenTicket is "Admin: reopen a wrong check/order" — a fresh,
// open ticket against the same set/order (lego.DB.ReopenTicket), reached
// from the phone the same way assign-ticket is. The finished ticket it
// points at is untouched; claiming the new one is what makes a check a
// recount (see loadSortedCheck), so there's nothing else to wire up here.
func (s *Server) handleAdminReopenTicket(w http.ResponseWriter, r *http.Request, sess lego.MobileSession) {
	if !s.requireAdmin(w, sess, "ADMIN_REOPEN_TICKET") {
		return
	}
	var req reopenTicketRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	req.Kind, req.Target, req.Reason = strings.TrimSpace(req.Kind), strings.TrimSpace(req.Target), strings.TrimSpace(req.Reason)
	if req.Kind != lego.TicketCheck && req.Kind != lego.TicketOrder || req.Target == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "kind (check or order) and target are required"})
		return
	}
	tk, err := s.legoDB.ReopenTicket(req.Kind, req.Target, s.ticketLabel(req.Kind, req.Target), req.Reason, sess.Username)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.audit.Log(sess.Username, sess.Role, "TICKET_REOPENED", "SUCCESS", tk.Label+" ("+tk.Kind+"): "+req.Reason)
	writeJSON(w, http.StatusOK, adminTicketRow{ID: tk.ID, Kind: tk.Kind, Target: tk.Target, Label: tk.Label, AssignedTo: tk.AssignedTo, Status: tk.Status})
}

// ticketLabel mirrors assignTargetScreen's own label lookup (internal/
// uiapp/tickets_screens.go) so a phone-assigned ticket reads the same as a
// TUI-assigned one — the set's name, or the order's supplier.
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

func orDash(s string) string {
	if s == "" {
		return "the open queue"
	}
	return s
}
