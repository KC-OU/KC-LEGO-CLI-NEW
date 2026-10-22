// Account management for the dashboard's Admin screen: create, reset, and
// lock/unlock a picker/checker/admin account across both ModernWMS and
// Part-DB. Mirrors cmd/wms/users.go's `wms users create`/`reset` and
// internal/users.Service.ToggleBoth exactly -- same temp-password
// generation, same "numeric login for handheld roles" rule, no new
// account-management logic of its own.
//
// This is the first genuinely *new* HTTP-reachable surface for these
// actions -- internal/mobileapi never exposed account creation/lock at
// all, only internal/access's CLI/TUI paths did. Treated with the same
// care as those: every call here requires an explicit "by" (see admin.go's
// package comment on why) and is written to the audit log.
package dashboardapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/auth"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/users"
)

type createAccountRequest struct {
	Username string `json:"username"`
	Role     string `json:"role"`
	Email    string `json:"email"`
	By       string `json:"by"`
}

type createAccountResponse struct {
	Username           string `json:"username"`
	Role               string `json:"role"`
	TemporaryPassword  string `json:"temporary_password"`
	MustChangePassword bool   `json:"must_change_password"`
}

// handleCreateAccount creates a Picker/Checker/Admin account with a
// freshly generated temp password (numeric PIN for Picker/Checker, since
// those roles sign in from a handheld's number keys -- see
// users.WantsNumericLogin), the same as `wms users create`. The password
// is returned exactly once, the same way the CLI prints it once; this
// package has no email-sending of its own, so handing a worker their temp
// credential is still a manual step for whoever creates the account.
func (s *Server) handleCreateAccount(w http.ResponseWriter, r *http.Request) {
	if s.usersSvc == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "account management is not available in this environment"})
		return
	}
	var req createAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	req.Username, req.Role, req.Email, req.By = strings.TrimSpace(req.Username), strings.TrimSpace(req.Role), strings.TrimSpace(req.Email), strings.TrimSpace(req.By)
	if req.Username == "" || req.By == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username and by are required"})
		return
	}
	if req.Role == "" {
		req.Role = "Picker"
	}
	if users.WantsNumericLogin(req.Role) && !users.NumericLoginUsernameRE.MatchString(req.Username) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": req.Role + " accounts need a 4-5 digit numeric user ID (for a handheld scanner's keypad)"})
		return
	}

	password := auth.GenerateTempPassword(10)
	if users.WantsNumericLogin(req.Role) {
		password = auth.GenerateNumericPIN(6)
	}
	if err := s.usersSvc.CreateUnified(r.Context(), req.Username, req.Role, req.Email, password); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(req.By, "DASHBOARD_USER_CREATED", "SUCCESS", req.Username+" ("+req.Role+")")
	writeJSON(w, http.StatusOK, createAccountResponse{Username: req.Username, Role: req.Role, TemporaryPassword: password, MustChangePassword: true})
}

type resetAccountRequest struct {
	Username string `json:"username"`
	By       string `json:"by"`
}

type resetAccountResponse struct {
	Username           string `json:"username"`
	TemporaryPassword  string `json:"temporary_password"`
	MustChangePassword bool   `json:"must_change_password"`
}

// handleResetAccount resets to a freshly generated temp password, the same
// as `wms users reset` -- numeric PIN if the username is itself numeric
// (the signal a numeric-login account was provisioned that way; see
// newUsersResetCmd's own identical comment on this).
func (s *Server) handleResetAccount(w http.ResponseWriter, r *http.Request) {
	if s.usersSvc == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "account management is not available in this environment"})
		return
	}
	var req resetAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	req.Username, req.By = strings.TrimSpace(req.Username), strings.TrimSpace(req.By)
	if req.Username == "" || req.By == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username and by are required"})
		return
	}

	password := auth.GenerateTempPassword(10)
	if users.NumericLoginUsernameRE.MatchString(req.Username) {
		password = auth.GenerateNumericPIN(6)
	}
	if err := s.usersSvc.ResetUnified(r.Context(), req.Username, password, true); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(req.By, "DASHBOARD_PASSWORD_RESET", "SUCCESS", req.Username)
	writeJSON(w, http.StatusOK, resetAccountResponse{Username: req.Username, TemporaryPassword: password, MustChangePassword: true})
}

type deleteAccountRequest struct {
	Username string `json:"username"`
	Confirm  string `json:"confirm"`
	By       string `json:"by"`
}

// handleDeleteAccount removes the account from both systems permanently --
// the TUI's own "Delete User" screen requires the same typed "yes" before
// calling DeleteBoth, which also still guards the protected identities
// (admin/1/kcollins/2) regardless of what's typed here.
func (s *Server) handleDeleteAccount(w http.ResponseWriter, r *http.Request) {
	if s.usersSvc == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "account management is not available in this environment"})
		return
	}
	var req deleteAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	req.Username, req.Confirm, req.By = strings.TrimSpace(req.Username), strings.TrimSpace(req.Confirm), strings.TrimSpace(req.By)
	if req.Username == "" || req.By == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username and by are required"})
		return
	}
	if strings.ToLower(req.Confirm) != "yes" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": `type "yes" to confirm`})
		return
	}
	if err := s.usersSvc.DeleteBoth(r.Context(), req.Username); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(req.By, "DASHBOARD_USER_DELETED", "SUCCESS", req.Username)
	writeJSON(w, http.StatusOK, map[string]string{"username": req.Username})
}

type renameAccountRequest struct {
	Username    string `json:"username"`
	NewUsername string `json:"new_username"`
	Role        string `json:"role"`
	By          string `json:"by"`
}

// handleRenameAccount is "Change ID" from the dashboard -- the same
// RenameUnified the TUI's own screen calls, including its numeric-login
// policy (a Picker/Checker account's new id must still be 4-5 digits) and
// protected-identity guard.
func (s *Server) handleRenameAccount(w http.ResponseWriter, r *http.Request) {
	if s.usersSvc == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "account management is not available in this environment"})
		return
	}
	var req renameAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	req.Username, req.NewUsername, req.Role, req.By = strings.TrimSpace(req.Username), strings.TrimSpace(req.NewUsername), strings.TrimSpace(req.Role), strings.TrimSpace(req.By)
	if req.Username == "" || req.NewUsername == "" || req.By == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username, new_username and by are required"})
		return
	}
	if err := s.usersSvc.RenameUnified(r.Context(), req.Username, req.NewUsername, req.Role); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(req.By, "DASHBOARD_USER_RENAMED", "SUCCESS", req.Username+" -> "+req.NewUsername)
	writeJSON(w, http.StatusOK, map[string]string{"username": req.NewUsername})
}

type toggleAccountRequest struct {
	Username string `json:"username"`
	By       string `json:"by"`
}

// handleLockAccount disables the account in both systems (ToggleBoth,
// isValid=false) -- the account still exists, it just can't sign in,
// mirroring the TUI's own lock action.
func (s *Server) handleLockAccount(w http.ResponseWriter, r *http.Request) {
	s.toggleAccount(w, r, false, "DASHBOARD_USER_LOCKED")
}

// handleUnlockAccount re-enables it.
func (s *Server) handleUnlockAccount(w http.ResponseWriter, r *http.Request) {
	s.toggleAccount(w, r, true, "DASHBOARD_USER_UNLOCKED")
}

func (s *Server) toggleAccount(w http.ResponseWriter, r *http.Request, isValid bool, auditAction string) {
	if s.usersSvc == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "account management is not available in this environment"})
		return
	}
	var req toggleAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	req.Username, req.By = strings.TrimSpace(req.Username), strings.TrimSpace(req.By)
	if req.Username == "" || req.By == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username and by are required"})
		return
	}
	if err := s.usersSvc.ToggleBoth(r.Context(), req.Username, isValid); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(req.By, auditAction, "SUCCESS", req.Username)
	writeJSON(w, http.StatusOK, map[string]any{"username": req.Username, "active": isValid})
}
