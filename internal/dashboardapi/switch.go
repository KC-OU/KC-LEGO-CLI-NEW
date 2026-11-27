package dashboardapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/clerkbridge"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/loginlock"
)

// Swapped in tests; in service these are the real Clerk lookups.
var (
	switchFind   = clerkbridge.FindByIdentifier
	switchVerify = clerkbridge.VerifyPassword
	signInTicket = clerkbridge.CreateSignInToken
)

type switchLoginRequest struct {
	Identifier string `json:"identifier"` // an email address or a user ID
	Password   string `json:"password"`
	From       string `json:"from"` // who is already signed in (for the audit line)
}

type switchLoginResponse struct {
	ClerkUserID string `json:"clerk_user_id"`
	Email       string `json:"email,omitempty"`
	Username    string `json:"username"`
	Role        string `json:"role"`
}

// handleSwitchLogin is POST /dashboard/auth/switch-login: someone already signed in on the dashboard (a checker, say)
// steps up to an ADMIN account with that admin's ID/email and password only, no authenticator code -- the same trust
// as the terminal's V key: the session is already verified, so the second account only proves its password. Only an
// admin account can be switched to, never another checker, and every answer to a failure is identical. The Rust
// service calls this after checking the caller's own session and hands back a short-lived session for the admin.
func (s *Server) handleSwitchLogin(w http.ResponseWriter, r *http.Request) {
	var req switchLoginRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	id := strings.ToLower(strings.TrimSpace(req.Identifier))
	if id == "" || req.Password == "" {
		graceLoginFailed(w)
		return
	}
	lockKey := "switch:" + id
	if _, locked, err := loginlock.Locked(lockKey); err == nil && locked {
		graceLoginFailed(w)
		return
	}
	fail := func(reason string) {
		_, _, _ = loginlock.RecordFailure(lockKey)
		s.logAudit(req.From, "DASHBOARD_ADMIN_SWITCH", "FAILED", "to="+id+" "+reason)
		graceLoginFailed(w)
	}
	info, err := switchFind(id)
	if err != nil {
		s.logAudit(req.From, "DASHBOARD_ADMIN_SWITCH", "ERROR", err.Error())
		graceLoginFailed(w)
		return
	}
	if info == nil {
		fail("no matching account")
		return
	}
	ok, err := switchVerify(info.ID, req.Password)
	if err != nil {
		s.logAudit(req.From, "DASHBOARD_ADMIN_SWITCH", "ERROR", err.Error())
		graceLoginFailed(w)
		return
	}
	if !ok {
		fail("wrong password")
		return
	}
	if info.Role != "admin" {
		fail("not an admin account")
		return
	}
	_ = loginlock.ClearFailures(lockKey)
	s.logAudit(req.From, "DASHBOARD_ADMIN_SWITCH", "SUCCESS", "to="+info.Username)
	resp := switchLoginResponse{ClerkUserID: info.ID, Username: info.Username, Role: info.Role}
	if strings.Contains(id, "@") {
		resp.Email = id
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleSignInToken is POST /dashboard/auth/sign-in-token {clerk_user_id}: a one-time browser sign-in ticket for that
// Clerk user. The Rust service calls it only after verifying that the caller holds a genuine Clerk session for exactly
// that user, so the KC-Parts app can open the web dashboard already signed in.
func (s *Server) handleSignInToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ClerkUserID string `json:"clerk_user_id"`
		By          string `json:"by"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&req); err != nil || strings.TrimSpace(req.ClerkUserID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "clerk_user_id is required"})
		return
	}
	ticket, err := signInTicket(strings.TrimSpace(req.ClerkUserID))
	if err != nil {
		s.logAudit(req.By, "DASHBOARD_APP_TICKET", "ERROR", err.Error())
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "could not get a sign-in ticket"})
		return
	}
	s.logAudit(req.By, "DASHBOARD_APP_TICKET", "SUCCESS", "")
	writeJSON(w, http.StatusOK, map[string]string{"ticket": ticket})
}
