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

// Server holds the dependencies every bot action needs — exported, unlike
// the rest of this package's internals, so internal/gateway can also drive
// Dispatch directly from the Discord Interactions Endpoint (see
// discord_interactions.go), without a second copy of the action logic.
type Server struct {
	legoDB *lego.DB
	audit  *audit.Logger
}

func NewServer(legoDB *lego.DB, auditLog *audit.Logger) *Server {
	return &Server{legoDB: legoDB, audit: auditLog}
}

// Serve runs the webhook until ctx is cancelled. addr must be loopback
// (127.0.0.1:<port>) — refused otherwise, not just defaulted, since this is
// a brand-new credentialed surface and the n8n workflow calling it is
// expected to run on the same box. s is shared with InteractionsHandler when
// Discord slash commands are also configured (see cmd/wms/gateway.go), so
// both paths dispatch through the exact same Server.
func Serve(ctx context.Context, addr string, s *Server) error {
	if !strings.HasPrefix(addr, "127.0.0.1:") {
		return fmt.Errorf("botapi: addr must be loopback (127.0.0.1:<port>), got %q", addr)
	}
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

// ActionRequest is one bot action call — exported so both the HTTP
// /bot/action body (JSON-decoded) and the Discord Interactions handler
// (built from a slash command's options) can construct the same shape.
type ActionRequest struct {
	Platform string          `json:"platform"`
	ID       string          `json:"id"`
	PIN      string          `json:"pin"`
	Action   string          `json:"action"`
	Params   json.RawMessage `json:"params"`
}

// handleAction decodes the request body and reports Dispatch's result as
// this endpoint's own JSON reply.
func (s *Server) handleAction(w http.ResponseWriter, r *http.Request) {
	var req ActionRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed JSON"})
		return
	}
	status, body := s.Dispatch(req)
	writeJSON(w, status, body)
}

// Dispatch resolves platform+id to a linked admin, verifies the PIN
// (collapsing "unknown id" and "wrong PIN" into the same 401 so a caller
// can't enumerate which IDs are linked), checks that admin's real
// permission, then runs one of the explicit actions below — every caller
// (the HTTP /bot/action route and the Discord Interactions handler) goes
// through this exact same check, so neither can reach anything the other
// can't.
func (s *Server) Dispatch(req ActionRequest) (int, map[string]any) {
	p, err := access.Load()
	if err != nil {
		return http.StatusInternalServerError, map[string]any{"error": "policy unreadable"}
	}
	key, ok := p.FindByBotID(req.Platform, req.ID)
	if !ok {
		return http.StatusUnauthorized, map[string]any{"error": "unauthorized"}
	}
	good, locked, err := checkPIN(key, req.PIN)
	if err != nil {
		return http.StatusInternalServerError, map[string]any{"error": "internal error"}
	}
	if locked {
		return http.StatusTooManyRequests, map[string]any{"error": "locked"}
	}
	if !good {
		return http.StatusUnauthorized, map[string]any{"error": "unauthorized"}
	}
	p, err = access.Load() // fresh: checkPIN's Update may have just cleared BotPINFails
	if err != nil {
		return http.StatusInternalServerError, map[string]any{"error": "policy unreadable"}
	}
	src, name, _ := strings.Cut(key, ":")
	if !p.Effective(src, name).Can(botActionPerm) {
		return http.StatusForbidden, map[string]any{"error": "forbidden"}
	}
	switch req.Action {
	case "reassign_ticket":
		return s.reassignTicket(name, req.Params)
	case "message_user":
		return s.messageUser(name, req.Params)
	case "kick_session":
		return s.kickSession(name, req.Params)
	case "list_open_tickets":
		return s.listOpenTickets()
	case "list_alerts":
		return s.listAlerts()
	default:
		return http.StatusBadRequest, map[string]any{"error": "unknown action"}
	}
}

type reassignParams struct {
	TicketID int64  `json:"ticket_id"`
	To       string `json:"to"`
}

// reassignTicket mirrors internal/uiapp/tickets_screens.go's finishForceOff:
// the same DB call, activity-feed event, notification, audit line, and
// reassurance message to whoever was taken off it.
func (s *Server) reassignTicket(actor string, raw json.RawMessage) (int, map[string]any) {
	var p reassignParams
	if err := json.Unmarshal(raw, &p); err != nil || p.TicketID == 0 {
		return http.StatusBadRequest, map[string]any{"error": "ticket_id is required"}
	}
	tickets, err := s.legoDB.AllOpenTickets()
	if err != nil {
		return http.StatusInternalServerError, map[string]any{"error": err.Error()}
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
		return http.StatusBadRequest, map[string]any{"error": "no such open ticket"}
	}
	if err := s.legoDB.ForceOffTicket(p.TicketID, p.To); err != nil {
		return http.StatusInternalServerError, map[string]any{"error": err.Error()}
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
	return http.StatusOK, map[string]any{"ok": true, "detail": detail}
}

type messageParams struct {
	Username string `json:"username"`
	Body     string `json:"body"`
}

// messageUser mirrors internal/uiapp/messages.go's sendAdminMessage.
func (s *Server) messageUser(actor string, raw json.RawMessage) (int, map[string]any) {
	var p messageParams
	if err := json.Unmarshal(raw, &p); err != nil || p.Username == "" || p.Body == "" {
		return http.StatusBadRequest, map[string]any{"error": "username and body are required"}
	}
	if err := s.legoDB.SendMessage(actor, p.Username, p.Body); err != nil {
		return http.StatusInternalServerError, map[string]any{"error": err.Error()}
	}
	_ = s.legoDB.LogEvent(lego.EventMessage, actor, p.Username, p.Body)
	notify.Dispatch(lego.EventMessage, fmt.Sprintf("%s -> %s (bot): %s", actor, p.Username, p.Body), "")
	s.audit.Log(actor, "", "MESSAGE_SENT", "SUCCESS", "to="+p.Username)
	return http.StatusOK, map[string]any{"ok": true}
}

type kickParams struct {
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
}

// kickSession mirrors internal/uiapp/sessions_screen.go's finishKick.
func (s *Server) kickSession(actor string, raw json.RawMessage) (int, map[string]any) {
	var p kickParams
	if err := json.Unmarshal(raw, &p); err != nil || p.SessionID == "" || p.Message == "" {
		return http.StatusBadRequest, map[string]any{"error": "session_id and message are required"}
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
		return http.StatusInternalServerError, map[string]any{"error": err.Error()}
	}
	_ = s.legoDB.LogEvent(lego.EventForcedOff, actor, username, "live session kicked (bot): "+p.Message)
	notify.Dispatch(lego.EventForcedOff, fmt.Sprintf("%s's session kicked by %s (bot): %s", orDash(username), actor, p.Message), "")
	s.audit.Log(actor, "", "SESSION_KICKED", "SUCCESS", orDash(username))
	return http.StatusOK, map[string]any{"ok": true}
}

// listOpenTickets is the one read-only action — no DB mutation, so nothing
// to log to the activity feed or notify about, just the current queue.
func (s *Server) listOpenTickets() (int, map[string]any) {
	tickets, err := s.legoDB.AllOpenTickets()
	if err != nil {
		return http.StatusInternalServerError, map[string]any{"error": err.Error()}
	}
	return http.StatusOK, map[string]any{"ok": true, "tickets": tickets}
}

// listAlerts is the Admin → Alerts feed: open accuracy escalations (21+
// missing in one check, flagged for manual review — see
// RecordCheckOutcome/accKindEscalate in internal/lego/accuracy.go) with no
// report filed against them yet. Also read-only, same reasoning as
// listOpenTickets.
func (s *Server) listAlerts() (int, map[string]any) {
	alerts, err := s.legoDB.OpenAccuracyEscalations()
	if err != nil {
		return http.StatusInternalServerError, map[string]any{"error": err.Error()}
	}
	return http.StatusOK, map[string]any{"ok": true, "alerts": alerts}
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
func (s *Server) handleResetPIN(w http.ResponseWriter, r *http.Request) {
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
