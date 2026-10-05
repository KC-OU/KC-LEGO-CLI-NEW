package mobileapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/notify"
)

type mobileMessage struct {
	ID        int64  `json:"id"`
	From      string `json:"from"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
}

// handleMessages is the phone's side of the TUI's own admin-to-user messages
// (internal/uiapp/messages.go's pollMessages) — the same UndeliveredMessages/
// MarkDelivered pair, same "shown once, to every undelivered message at
// once" semantics, just polled by the app instead of the TUI's own tick
// loop. A message already marked delivered to one of the user's other
// sessions (the TUI, say) never comes back here — same as it already works
// between two TUI sessions.
func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request, sess lego.MobileSession) {
	msgs, err := s.legoDB.UndeliveredMessages(sess.Username)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	out := make([]mobileMessage, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, mobileMessage{ID: m.ID, From: m.From, Body: m.Body, CreatedAt: m.CreatedAt.Format(time.RFC3339)})
		_ = s.legoDB.MarkDelivered(m.ID)
	}
	writeJSON(w, http.StatusOK, out)
}

type messageAdminRequest struct {
	Body string `json:"body"`
}

// handleMessageAdmin is the phone's side of My Settings' "Message an Admin"
// (internal/uiapp/my_settings.go's messageAdminScreen) — same Recent
// Activity + notification routing, no per-recipient inbox (admin is a role,
// not a person, so there's nowhere to queue a reply the way handleMessages
// has for an admin-to-user note).
func (s *Server) handleMessageAdmin(w http.ResponseWriter, r *http.Request, sess lego.MobileSession) {
	var req messageAdminRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	body := strings.TrimSpace(req.Body)
	if body == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "message is required"})
		return
	}
	detail := "to admins: " + body
	_ = s.legoDB.LogEvent(lego.EventMessage, sess.Username, "", detail)
	go notify.Dispatch(lego.EventMessage, sess.Username+": "+body, "")
	s.audit.Log(sess.Username, sess.Role, "MESSAGE_TO_ADMIN", "SUCCESS", detail)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type flagLocationRequest struct {
	PartNum   string `json:"part_num"`
	ColorName string `json:"color_name"`
	Location  string `json:"location"`
	Note      string `json:"note"`
}

// handleFlagLocation is "flag this location/barcode as wrong": a picker or
// checker who finds a part not where the system says it is (or a barcode
// that won't scan) tells an admin right away without it blocking the rest
// of their list — same notification path as handleMessageAdmin, just with
// the part/location folded into the message so an admin doesn't have to ask
// what and where. Nothing about the line itself changes: it's still open,
// still countable, same as before the flag.
func (s *Server) handleFlagLocation(w http.ResponseWriter, r *http.Request, sess lego.MobileSession) {
	var req flagLocationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	req.PartNum, req.ColorName, req.Location, req.Note = strings.TrimSpace(req.PartNum), strings.TrimSpace(req.ColorName),
		strings.TrimSpace(req.Location), strings.TrimSpace(req.Note)
	if req.PartNum == "" || req.Location == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "part_num and location are required"})
		return
	}
	body := "FLAGGED: " + req.PartNum
	if req.ColorName != "" {
		body += " (" + req.ColorName + ")"
	}
	body += " — location says \"" + req.Location + "\" but that's wrong"
	if req.Note != "" {
		body += ": " + req.Note
	}
	detail := "to admins: " + body
	_ = s.legoDB.LogEvent(lego.EventMessage, sess.Username, "", detail)
	go notify.Dispatch(lego.EventMessage, sess.Username+": "+body, "")
	s.audit.Log(sess.Username, sess.Role, "FLAG_LOCATION", "SUCCESS", detail)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
