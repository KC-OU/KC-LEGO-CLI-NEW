// Live sessions, messaging, and force log-off: mirrors internal/botapi's
// own messageUser/kickSession exactly (same LogEvent + notify.Dispatch
// pair, same lego.DB calls) -- small, deliberate duplication across an
// unrelated package boundary for a few lines, same trade-off this
// package's other handlers already made (see admin.go's own doc comment).
package dashboardapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/notify"
)

// sessionMaxAge matches internal/botapi's own constant -- the same
// "older than this is crashed/abandoned, not shown" live-session window
// the TUI's own Admin -> Live Sessions screen uses.
const sessionMaxAge = 20 * time.Minute

type sessionRow struct {
	SessionID string `json:"session_id"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	Transport string `json:"transport"`
	Screen    string `json:"screen"`
	StartedAt string `json:"started_at"`
	LastSeen  string `json:"last_seen"`
}

// handleSessions lists every live session -- the picker for the force
// log-off action, same data the TUI's own Live Sessions screen shows.
func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.legoDB.LiveSessions(sessionMaxAge)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	rows := make([]sessionRow, len(sessions))
	for i, sess := range sessions {
		rows[i] = sessionRow{
			SessionID: sess.SessionID, Username: sess.Username, Role: sess.Role, Transport: sess.Transport,
			Screen: sess.Screen, StartedAt: sess.StartedAt.Format(time.RFC3339), LastSeen: sess.LastSeen.Format(time.RFC3339),
		}
	}
	writeJSON(w, http.StatusOK, rows)
}

type messageRequest struct {
	Username string `json:"username"`
	Body     string `json:"body"`
	By       string `json:"by"`
}

// handleMessageUser is the dedicated "send a message to a user" action --
// the same corner-toast internal/lego.SendMessage every other surface
// (TUI, Discord bot) already delivers, just reachable from the dashboard
// now instead of only as a side effect of assigning a ticket.
func (s *Server) handleMessageUser(w http.ResponseWriter, r *http.Request) {
	var req messageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	req.Username, req.Body, req.By = strings.TrimSpace(req.Username), strings.TrimSpace(req.Body), strings.TrimSpace(req.By)
	if req.Username == "" || req.Body == "" || req.By == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username, body and by are required"})
		return
	}
	if err := s.legoDB.SendMessage(req.By, req.Username, req.Body); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	_ = s.legoDB.LogEvent(lego.EventMessage, req.By, req.Username, req.Body)
	notify.Dispatch(lego.EventMessage, fmt.Sprintf("%s -> %s (dashboard): %s", req.By, req.Username, req.Body), "")
	s.logAudit(req.By, "DASHBOARD_MESSAGE_SENT", "SUCCESS", "to="+req.Username)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

type kickRequest struct {
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
	By        string `json:"by"`
}

// handleKickSession is force log-off: flags a live session to be signed
// off at its next idle tick, same effect as the TUI's own Live Sessions
// "K" action and the Discord bot's /wms kick.
func (s *Server) handleKickSession(w http.ResponseWriter, r *http.Request) {
	var req kickRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	req.SessionID, req.Message, req.By = strings.TrimSpace(req.SessionID), strings.TrimSpace(req.Message), strings.TrimSpace(req.By)
	if req.SessionID == "" || req.Message == "" || req.By == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "session_id, message and by are required"})
		return
	}
	username := ""
	if sessions, err := s.legoDB.LiveSessions(sessionMaxAge); err == nil {
		for _, sess := range sessions {
			if sess.SessionID == req.SessionID {
				username = sess.Username
				break
			}
		}
	}
	if err := s.legoDB.ForceLogoff(req.SessionID, req.Message); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	_ = s.legoDB.LogEvent(lego.EventForcedOff, req.By, username, "live session kicked (dashboard): "+req.Message)
	notify.Dispatch(lego.EventForcedOff, fmt.Sprintf("%s's session kicked by %s (dashboard): %s", orDash(username), req.By, req.Message), "")
	s.logAudit(req.By, "DASHBOARD_SESSION_KICKED", "SUCCESS", orDash(username))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
