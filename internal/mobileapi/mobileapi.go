// Package mobileapi is the Go side of the sentry-wms-inspired mobile pick/
// check app: real staff accounts (same password + 2FA as the TUI), a
// per-shift bearer session instead of re-authenticating on every request,
// and — unlike internal/botapi, which stays loopback-only — reachable from
// a phone on the network, since a verified staff login is already the same
// trust boundary the telnet/web gateway itself relies on.
//
// Reads (what to check/pick next, where it lives) query internal/lego
// directly. Writes (mark missing, confirm a pick) are a deliberately small,
// separate surface here rather than reimplemented against this project's
// business rules a second time — one source of truth for what a scan
// actually does, same principle as /bot/action and the Discord slash
// command sharing botapi.Server.Dispatch.
package mobileapi

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/audit"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/auth"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/gateway"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/partdb"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/twofa"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/users"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/wmsdb"
)

// Server holds every dependency a handler needs. authWMS/authPDB are the
// narrow auth.WMSAuthenticator/PartDBAuthenticator interfaces (not the
// concrete clients) so tests can substitute fakes for login instead of a
// live container or Part-DB file, the same reasoning internal/auth's own
// interfaces exist for; pdb is the concrete client the read endpoints need
// for shelf-location lookups (lego.DB.LocationNames), which that narrow
// interface doesn't expose.
type Server struct {
	authWMS auth.WMSAuthenticator
	authPDB auth.PartDBAuthenticator
	pdb     *partdb.DB
	rebrick *lego.Client
	legoDB  *lego.DB
	audit   *audit.Logger
	// throttle blocks an address after too many failed /mobile/login
	// attempts — the same gateway.Throttle the telnet gateway already uses
	// for the exact same reason, just not previously wired in here. nil in
	// tests that build a Server literal directly (checkWalkEnv and
	// friends): handleLogin skips throttling entirely when it's nil, rather
	// than every such test needing to set up one more field.
	throttle *gateway.Throttle
	// pdbw is the same Part-DB writer the TUI's finishCheck/pushReceived use
	// (see finish.go) — built straight from pdb, so /mobile/finish can push
	// a completed check/order the same way F does in the TUI, instead of
	// leaving a mobile-only check/order permanently un-synced.
	pdbw *partdb.Writer
	// users backs the admin endpoints' "who can I assign this to" list (see
	// admin.go) — built the same way uiapp's own app.users is.
	users *users.Service
}

func NewServer(wms *wmsdb.Client, pdb *partdb.DB, legoDB *lego.DB, auditLog *audit.Logger) *Server {
	return &Server{
		authWMS: wms, authPDB: pdb, pdb: pdb, legoDB: legoDB, audit: auditLog,
		rebrick:  lego.NewClientFor(legoDB),
		throttle: gateway.NewThrottle(),
		pdbw:     partdb.NewWriter(pdb),
		users:    users.New(wms, pdb),
	}
}

// remoteHost is r.RemoteAddr with the port stripped, the same shape
// gateway.Throttle expects (and the telnet gateway already passes it) —
// net.ParseIP inside Throttle.exempt rejects anything with a port attached.
func remoteHost(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// Serve runs the mobile API until ctx is cancelled. Unlike botapi.Serve,
// addr is not restricted to loopback — a phone has to reach this over the
// network (LAN, or whatever's in front of it, same as the web gateway).
func Serve(ctx context.Context, addr string, s *Server) error {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /mobile/login", s.handleLogin)
	mux.HandleFunc("POST /mobile/login/2fa", s.handleLogin2FA)
	mux.HandleFunc("POST /mobile/logout", s.withSession(s.handleLogout))
	mux.HandleFunc("GET /mobile/me", s.withSession(s.handleMe))
	mux.HandleFunc("GET /mobile/next", s.withSession(s.handleNext))
	mux.HandleFunc("POST /mobile/confirm", s.withSession(s.handleConfirm))
	mux.HandleFunc("POST /mobile/finish", s.withSession(s.handleFinish))
	mux.HandleFunc("GET /mobile/messages", s.withSession(s.handleMessages))
	mux.HandleFunc("POST /mobile/message-admin", s.withSession(s.handleMessageAdmin))
	mux.HandleFunc("POST /mobile/flag-location", s.withSession(s.handleFlagLocation))
	mux.HandleFunc("GET /mobile/history", s.withSession(s.handleHistory))
	mux.HandleFunc("GET /mobile/attendance", s.withSession(s.handleAttendanceStatus))
	mux.HandleFunc("POST /mobile/attendance/clock-in", s.withSession(s.handleClockIn))
	mux.HandleFunc("POST /mobile/attendance/clock-out", s.withSession(s.handleClockOut))
	mux.HandleFunc("POST /mobile/attendance/break-start", s.withSession(s.handleBreakStart))
	mux.HandleFunc("POST /mobile/attendance/break-end", s.withSession(s.handleBreakEnd))
	mux.HandleFunc("GET /mobile/admin/users", s.withSession(s.handleAdminUsers))
	mux.HandleFunc("GET /mobile/admin/tickets", s.withSession(s.handleAdminTickets))
	mux.HandleFunc("POST /mobile/admin/assign-ticket", s.withSession(s.handleAdminAssignTicket))
	mux.HandleFunc("POST /mobile/admin/reopen-ticket", s.withSession(s.handleAdminReopenTicket))
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

type loginRequest struct {
	Username, Password string
}

type loginResponse struct {
	Token    string `json:"token"`
	Needs2FA bool   `json:"needs_2fa"`
}

// handleLogin is step one: username + password, exactly what the TUI's own
// login screen checks (auth.AuthenticateUser against ModernWMS then Part-DB)
// — a wrong password fails here identically either way. If the account has
// 2FA enabled, the returned token is only good for POST /mobile/login/2fa
// next; MobileSessionFor refuses it for anything else until that completes.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var host string
	if s.throttle != nil {
		host = remoteHost(r)
		release, why := s.throttle.Admit(host)
		if why != "" {
			s.audit.Log("-", "", "MOBILE_LOGIN", "DENIED", why+" ("+host+")")
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": why})
			return
		}
		defer release()
	}
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Username == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username and password are required"})
		return
	}
	session, err := auth.AuthenticateUser(r.Context(), s.authWMS, s.authPDB, req.Username, req.Password)
	if err != nil {
		if s.throttle != nil {
			s.throttle.Strike(host)
		}
		status := "FAILED_INVALID_CREDENTIALS"
		if errors.Is(err, auth.ErrAccountDisabled) {
			status = "FAILED_ACCOUNT_DISABLED"
		}
		s.audit.Log(req.Username, "", "MOBILE_LOGIN", status, err.Error())
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid username or password"})
		return
	}
	// A real credential check only confirms the password — expiry is a
	// separate, access-policy-level gate the TUI already enforces
	// (continueSignOn, internal/uiapp/login.go) that this path was missing
	// entirely: an expired account could still get a mobile session even
	// though the exact same account is correctly refused at the terminal.
	if p, err := access.Load(); err == nil {
		if u := p.Users[access.Key(session.Source, session.Username)]; u.Expired(time.Now()) {
			s.audit.Log(session.Username, session.Role, "MOBILE_LOGIN", "DENIED_EXPIRED", "access ended "+u.Expires)
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "This account's access has ended. Ask an admin."})
			return
		}
	}
	needs2FA := twofa.IsEnabled(session.Username, session.Source)
	token, err := s.legoDB.StartMobileLogin(session.Username, session.Source, session.Role, needs2FA)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.audit.Log(session.Username, session.Role, "MOBILE_LOGIN", "SUCCESS", "step 1 of "+stepCount(needs2FA))
	writeJSON(w, http.StatusOK, loginResponse{Token: token, Needs2FA: needs2FA})
}

func stepCount(needs2FA bool) string {
	if needs2FA {
		return "2 (2FA required)"
	}
	return "1 (no 2FA on this account)"
}

type login2FARequest struct {
	Token, Code string
}

// handleLogin2FA is step two, only reached when step one said Needs2FA —
// verifies the same way the TUI's own 2FA screen does (twofa.Verify), then
// promotes the pending token to a full session.
func (s *Server) handleLogin2FA(w http.ResponseWriter, r *http.Request) {
	var req login2FARequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Token == "" || req.Code == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "token and code are required"})
		return
	}
	pending, err := s.legoDB.PendingMobile2FA(req.Token)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if _, err := twofa.Verify(pending.Username, pending.Source, req.Code); err != nil {
		s.audit.Log(pending.Username, pending.Role, "MOBILE_LOGIN", "FAILED_2FA", err.Error())
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "wrong or expired code"})
		return
	}
	if err := s.legoDB.ConfirmMobile2FA(req.Token); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	s.audit.Log(pending.Username, pending.Role, "MOBILE_LOGIN", "SUCCESS", "step 2 of 2")
	writeJSON(w, http.StatusOK, loginResponse{Token: req.Token})
}

// withSession resolves the Authorization: Bearer <token> header into a
// lego.MobileSession and hands it to next — every route past login goes
// through this, the same "one gate" shape botapi's permission check is.
func (s *Server) withSession(next func(w http.ResponseWriter, r *http.Request, sess lego.MobileSession)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		sess, err := s.legoDB.MobileSessionFor(token)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next(w, r, sess)
	}
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request, sess lego.MobileSession) {
	writeJSON(w, http.StatusOK, map[string]any{"username": sess.Username, "source": sess.Source, "role": sess.Role})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request, sess lego.MobileSession) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if err := s.legoDB.EndMobileSession(token); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.audit.Log(sess.Username, sess.Role, "MOBILE_LOGOUT", "SUCCESS", "")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
