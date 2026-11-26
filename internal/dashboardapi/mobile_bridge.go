// Bridge for the KC app: lets the Rust service (the only holder of the shared token) open a
// phone-API session for a Clerk-linked account, then call the same /mobile/ check, message and
// history routes the phone app uses. Those routes are mounted here behind the shared token (sent as
// X-KC-Token, since the phone API's own session token travels in Authorization), so nothing new is
// reachable without the Rust service.
package dashboardapi

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/mobileapi"
)

// SetMobile attaches the phone API so its routes are served under this server's shared token.
func (s *Server) SetMobile(m *mobileapi.Server) { s.mobile = m }

// mountMobile registers /mobile/ behind X-KC-Token. No-op when SetMobile was not called.
func (s *Server) mountMobile(mux *http.ServeMux) {
	if s.mobile == nil {
		return
	}
	inner := http.NewServeMux()
	s.mobile.Mount(inner)
	mux.HandleFunc("/mobile/", func(w http.ResponseWriter, r *http.Request) {
		if s.token != "" && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-KC-Token")), []byte(s.token)) != 1 {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		inner.ServeHTTP(w, r)
	})
}

type mobileSessionRequest struct {
	Username string `json:"username"`
	Source   string `json:"source"`
}

// handleMobileSession is POST /dashboard/mobile-session {username, source}: a phone-API session
// token for a governed account (one with an Access Control entry). The caller has already proved the
// user's identity (Clerk JWT), so there is no password or 2FA step here; ungoverned names are refused.
func (s *Server) handleMobileSession(w http.ResponseWriter, r *http.Request) {
	var req mobileSessionRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil || strings.TrimSpace(req.Username) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username is required"})
		return
	}
	p, err := access.Load()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	key := resolveUserKey(p, req.Source, strings.TrimSpace(req.Username))
	src, name, _ := strings.Cut(key, ":")
	if !p.Has(src, name) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "this account has no access set up"})
		return
	}
	role := "User"
	if p.Effective(src, name).Can("access.manage") {
		role = "Admin"
	}
	tok, err := s.legoDB.StartMobileLogin(name, src, role, false)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(name, "KC_APP_SESSION", "SUCCESS", "app session opened")
	writeJSON(w, http.StatusOK, map[string]string{"token": tok, "username": name, "source": src, "role": role})
}
