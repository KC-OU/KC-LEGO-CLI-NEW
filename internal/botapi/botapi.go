// Package botapi is the WMS side of the Discord/Slack remote-admin bot: a
// small, loopback-only JSON webhook an n8n workflow calls after someone asks
// a linked admin's bot to do something. WMS never talks to Discord/Slack
// itself — n8n receives the chat message, decides what action and
// parameters it means, and POSTs that decision here as plain JSON. Every
// action still goes through the exact permission check its TUI equivalent
// already requires, so this door can never do more than the admin could
// already do by signing in directly.
package botapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/audit"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/auth"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/notify"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/twofa"
)

// botActionPerm is the permission every action below needs — the same
// "users.view" gate the TUI's whole Admin submenu (Assign Work, Message a
// User, Live Sessions, ...) already requires, so the bot can never reach
// further than that admin already could by signing in.
const botActionPerm = "users.view"

// Lockout after repeated bad PINs — ponytail: a flat 5-minute lockout, not
// internal/twofa's doubling backoff; add a Locks counter on access.User if
// repeat offenders ever need harsher escalation than "wait 5 minutes."
const (
	maxPINFailures = 5
	pinLockFor     = 5 * time.Minute
	sessionMaxAge  = 20 * time.Minute // matches uiapp's own live-session window
)

type server struct {
	legoDB *lego.DB
	audit  *audit.Logger
}

// Serve runs the webhook until ctx is cancelled. addr must be loopback
// (127.0.0.1:<port>) — refused otherwise, not just defaulted, since this is
// a brand-new credentialed surface and the n8n workflow calling it is
// expected to run on the same box.
func Serve(ctx context.Context, addr string, legoDB *lego.DB, auditLog *audit.Logger) error {
	if !strings.HasPrefix(addr, "127.0.0.1:") {
		return fmt.Errorf("botapi: addr must be loopback (127.0.0.1:<port>), got %q", addr)
	}
	s := &server{legoDB: legoDB, audit: auditLog}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /bot/action", s.handleAction)
	mux.HandleFunc("POST /bot/reset-pin", s.handleResetPIN)
	srv := &http.Server{Addr: addr, Handler: mux}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// checkPIN verifies pin for the admin stored at key, recording the attempt
// (and locking after maxPINFailures) in the same access.Update call so the
// result is atomic and survives a gateway restart — unlike an in-memory
// counter, which a redeploy would silently reset.
func checkPIN(key, pin string) (ok, locked bool, err error) {
	_, err = access.Update(func(p *access.Policy) error {
		u := p.Users[key]
		if u == nil {
			return nil
		}
		if u.BotPINLockUntil != "" {
			if t, perr := time.Parse(time.RFC3339, u.BotPINLockUntil); perr == nil {
				if time.Now().Before(t) {
					locked = true
					return nil
				}
				u.BotPINLockUntil = "" // the lock has expired
			}
		}
		if auth.VerifyPartDB(pin, u.BotPINHash) {
			ok = true
			u.BotPINFails = 0
			return nil
		}
		u.BotPINFails++
		if u.BotPINFails >= maxPINFailures {
			u.BotPINLockUntil = time.Now().Add(pinLockFor).Format(time.RFC3339)
			u.BotPINFails = 0
		}
		return nil
	})
	return ok, locked, err
}

type actionRequest struct {
	Platform string          `json:"platform"`
	ID       string          `json:"id"`
	PIN      string          `json:"pin"`
	Action   string          `json:"action"`
	Params   json.RawMessage `json:"params"`
}

// handleAction resolves platform+id to a linked admin, verifies the PIN
// (collapsing "unknown id" and "wrong PIN" into the same 401 so a caller
// can't enumerate which IDs are linked), checks that admin's real
// permission, then dispatches to one of the explicit actions below.
func (s *server) handleAction(w http.ResponseWriter, r *http.Request) {
	var req actionRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed JSON"})
		return
	}
	p, err := access.Load()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "policy unreadable"})
		return
	}
	key, ok := p.FindByBotID(req.Platform, req.ID)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	good, locked, err := checkPIN(key, req.PIN)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if locked {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "locked"})
		return
	}
	if !good {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	p, err = access.Load() // fresh: checkPIN's Update may have just cleared BotPINFails
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "policy unreadable"})
		return
	}
	src, name, _ := strings.Cut(key, ":")
	if !p.Effective(src, name).Can(botActionPerm) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	switch req.Action {
	case "reassign_ticket":
		s.reassignTicket(w, name, req.Params)
	case "message_user":
		s.messageUser(w, name, req.Params)
	case "kick_session":
		s.kickSession(w, name, req.Params)
	case "list_open_tickets":
		s.listOpenTickets(w)
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown action"})
	}
}

type reassignParams struct {
	TicketID int64  `json:"ticket_id"`
	To       string `json:"to"`
}

// reassignTicket mirrors internal/uiapp/tickets_screens.go's finishForceOff:
// the same DB call, activity-feed event, notification, audit line, and
// reassurance message to whoever was taken off it.
func (s *server) reassignTicket(w http.ResponseWriter, actor string, raw json.RawMessage) {
	var p reassignParams
	if err := json.Unmarshal(raw, &p); err != nil || p.TicketID == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "ticket_id is required"})
		return
	}
	tickets, err := s.legoDB.AllOpenTickets()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	var from, label string
	found := false
	for _, t := range tickets {
		if t.ID == p.TicketID {
			from, label, found = t.AssignedTo, t.Label, true
			break
		}
	}
	if !found {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no such open ticket"})
		return
	}
	if err := s.legoDB.ForceOffTicket(p.TicketID, p.To); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	kind, dest := lego.EventForcedOff, "the open queue"
	if p.To != "" {
		kind, dest = lego.EventReassigned, p.To
	}
	detail := fmt.Sprintf("%s: %s -> %s", label, orDash(from), orDash(p.To))
	_ = s.legoDB.LogEvent(kind, actor, from, detail)
	notify.Dispatch(kind, fmt.Sprintf("%s taken off %s by %s (bot) — sent to %s", orDash(from), label, actor, dest), "")
	s.audit.Log(actor, "", "TICKET_FORCED_OFF", "SUCCESS", detail)
	if from != "" {
		_ = s.legoDB.SendMessage(actor, from, "We've assigned you another task — don't worry, your accuracy won't be affected.")
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "detail": detail})
}

type messageParams struct {
	Username string `json:"username"`
	Body     string `json:"body"`
}

// messageUser mirrors internal/uiapp/messages.go's sendAdminMessage.
func (s *server) messageUser(w http.ResponseWriter, actor string, raw json.RawMessage) {
	var p messageParams
	if err := json.Unmarshal(raw, &p); err != nil || p.Username == "" || p.Body == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username and body are required"})
		return
	}
	if err := s.legoDB.SendMessage(actor, p.Username, p.Body); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	_ = s.legoDB.LogEvent(lego.EventMessage, actor, p.Username, p.Body)
	notify.Dispatch(lego.EventMessage, fmt.Sprintf("%s -> %s (bot): %s", actor, p.Username, p.Body), "")
	s.audit.Log(actor, "", "MESSAGE_SENT", "SUCCESS", "to="+p.Username)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

type kickParams struct {
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
}

// kickSession mirrors internal/uiapp/sessions_screen.go's finishKick.
func (s *server) kickSession(w http.ResponseWriter, actor string, raw json.RawMessage) {
	var p kickParams
	if err := json.Unmarshal(raw, &p); err != nil || p.SessionID == "" || p.Message == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "session_id and message are required"})
		return
	}
	username := ""
	if sessions, err := s.legoDB.LiveSessions(sessionMaxAge); err == nil {
		for _, sess := range sessions {
			if sess.SessionID == p.SessionID {
				username = sess.Username
				break
			}
		}
	}
	if err := s.legoDB.ForceLogoff(p.SessionID, p.Message); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	_ = s.legoDB.LogEvent(lego.EventForcedOff, actor, username, "live session kicked (bot): "+p.Message)
	notify.Dispatch(lego.EventForcedOff, fmt.Sprintf("%s's session kicked by %s (bot): %s", orDash(username), actor, p.Message), "")
	s.audit.Log(actor, "", "SESSION_KICKED", "SUCCESS", orDash(username))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// listOpenTickets is the one read-only action — no DB mutation, so nothing
// to log to the activity feed or notify about, just the current queue.
func (s *server) listOpenTickets(w http.ResponseWriter) {
	tickets, err := s.legoDB.AllOpenTickets()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "tickets": tickets})
}

type resetRequest struct {
	Platform string `json:"platform"`
	ID       string `json:"id"`
	Code     string `json:"code"`
	Answer   string `json:"answer"`
	NewPIN   string `json:"new_pin"`
}

// handleResetPIN requires a live 2FA code AND the security answer together —
// either alone is refused, same rule as the TUI's own reset screen
// (internal/uiapp/access_screens.go's accessBotResetScreen).
func (s *server) handleResetPIN(w http.ResponseWriter, r *http.Request) {
	var req resetRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil || req.NewPIN == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	p, err := access.Load()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "policy unreadable"})
		return
	}
	key, ok := p.FindByBotID(req.Platform, req.ID)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	u := p.Users[key]
	if u == nil || u.SecurityAnswerHash == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	src, name, _ := strings.Cut(key, ":")
	answerOK := auth.VerifyPartDB(strings.ToLower(strings.TrimSpace(req.Answer)), u.SecurityAnswerHash)
	_, verifyErr := twofa.Verify(name, src, req.Code)
	if !answerOK || verifyErr != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	newHash, err := auth.HashPartDB(req.NewPIN)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if _, err := access.Update(func(p *access.Policy) error {
		u := p.Users[key]
		if u == nil {
			return fmt.Errorf("user no longer exists")
		}
		u.BotPINHash, u.BotPINFails, u.BotPINLockUntil = newHash, 0, ""
		return nil
	}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.audit.Log(name, "", "BOT_PIN_RESET", "SUCCESS", "via webhook")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
