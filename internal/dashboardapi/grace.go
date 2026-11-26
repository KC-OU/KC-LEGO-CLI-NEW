// The web dashboard's own 2FA grace window -- see internal/dashboardgrace's
// package doc for the full design. Both handlers here are reachable only
// through the existing shared WMS_DASHBOARD_API_TOKEN bearer that already
// protects every other /dashboard/* route (see withToken) -- the browser
// never calls either of these directly, only the Rust service does, as a
// server-to-server hop.
package dashboardapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/clerkbridge"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/dashboardgrace"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/loginlock"
)

type graceMintRequest struct {
	ClerkUserID string `json:"clerk_user_id"`
}

type graceMintResponse struct {
	Token         string `json:"token"`
	ExpiresAtUnix int64  `json:"expires_at_unix"`
}

// handleGraceMint is POST /dashboard/auth/grace-mint. The Rust side calls
// this exactly once, right after it has independently verified (via the
// same JWKS check every other authenticated request already goes through)
// that a request carries a freshly Clerk-issued, currently-valid session
// token for clerk_user_id -- so reaching this endpoint already proves a
// real, complete Clerk login (password + a genuine second factor, or
// OAuth) just happened for that exact account. This handler trusts that
// claim at face value (it has no way to re-derive it -- Clerk's secret key
// lives only here, in Go, not in Rust, and Rust's JWKS check needs no
// secret at all), the same boundary every other dashboardapi write already
// trusts the Rust service's bearer token for.
func (s *Server) handleGraceMint(w http.ResponseWriter, r *http.Request) {
	var req graceMintRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	req.ClerkUserID = strings.TrimSpace(req.ClerkUserID)
	if req.ClerkUserID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "clerk_user_id is required"})
		return
	}
	window := time.Duration(dashboardgrace.WindowMinutes()) * time.Minute
	token, expiresAt, err := dashboardgrace.Mint(req.ClerkUserID, window)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, graceMintResponse{Token: token, ExpiresAtUnix: expiresAt.Unix()})
}

type graceLoginRequest struct {
	Email      string `json:"email"`
	Password   string `json:"password"`
	GraceToken string `json:"grace_token"`
}

type graceLoginResponse struct {
	ClerkUserID string `json:"clerk_user_id"`
	Email       string `json:"email"`
	Username    string `json:"username,omitempty"`
	Role        string `json:"role,omitempty"`
	// ExpiresAt is the ORIGINAL grace token's own expiry (see
	// dashboardgrace.Check), not a fresh one -- the Rust side mints its
	// follow-up session token with this exact exp, never a new full window,
	// so a grace-login can never extend how long the window lasts.
	ExpiresAtUnix int64 `json:"expires_at_unix"`
}

// graceLoginFailed is the one response every failure path below returns --
// same status, same body, regardless of which check actually failed (no
// account with that email, wrong password, no/expired/mismatched grace
// token, or a lockout already in effect). A distinguishable response for
// each would hand an attacker a user-enumeration and account-probing oracle
// for free; a normal wrong-password reply already looks exactly like this.
func graceLoginFailed(w http.ResponseWriter) {
	writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
}

// handleGraceLogin is POST /dashboard/auth/grace-login -- the password-
// re-check-but-skip-2FA path. Clerk's own hosted sign-in flow cannot
// complete without a fresh second-factor code once MFA is enrolled (a hard
// platform constraint, confirmed this session, not a missing option), so
// this never goes through Clerk's client-side flow at all: it independently
// re-verifies the password against Clerk's Backend API and separately
// checks for a still-valid grace token belonging to the exact same account,
// here, server-side.
//
// Every step shares one lockout bucket (internal/loginlock, keyed by the
// lower-cased email) with the same 5-failures/15-minute shape every other
// login-adjacent surface in this codebase already uses -- this endpoint
// does its own independent password verification against Clerk, so without
// its own throttling it would be a second, unthrottled password-guessing
// surface against the same account that Clerk's own rate limiting on its
// hosted sign-in flow would never see.
func (s *Server) handleGraceLogin(w http.ResponseWriter, r *http.Request) {
	var req graceLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	email := strings.TrimSpace(strings.ToLower(req.Email))
	if email == "" || req.Password == "" || req.GraceToken == "" {
		graceLoginFailed(w)
		return
	}

	if _, locked, err := loginlock.Locked(email); err == nil && locked {
		graceLoginFailed(w)
		return
	}

	fail := func(reason string) {
		_, _, _ = loginlock.RecordFailure(email)
		s.logAudit(email, "DASHBOARD_GRACE_LOGIN", "FAILED", reason)
		graceLoginFailed(w)
	}

	info, err := clerkbridge.FindUserByEmail(email)
	if err != nil {
		// Could not even ask Clerk -- not the caller's fault, but still never
		// distinguishable from a normal failure in the response.
		s.logAudit(email, "DASHBOARD_GRACE_LOGIN", "ERROR", err.Error())
		graceLoginFailed(w)
		return
	}
	if info == nil {
		fail("no matching Clerk account")
		return
	}

	verified, err := clerkbridge.VerifyPassword(info.ID, req.Password)
	if err != nil {
		s.logAudit(email, "DASHBOARD_GRACE_LOGIN", "ERROR", err.Error())
		graceLoginFailed(w)
		return
	}
	if !verified {
		fail("wrong password")
		return
	}

	graceOK, graceExpiresAt := dashboardgrace.Check(req.GraceToken, info.ID)
	if !graceOK {
		fail("no valid grace token for this account")
		return
	}

	_ = loginlock.ClearFailures(email)
	s.logAudit(email, "DASHBOARD_GRACE_LOGIN", "SUCCESS", info.ID)
	writeJSON(w, http.StatusOK, graceLoginResponse{
		ClerkUserID: info.ID, Email: email, Username: info.Username, Role: info.Role,
		ExpiresAtUnix: graceExpiresAt.Unix(),
	})
}
