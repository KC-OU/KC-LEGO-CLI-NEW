// Full per-user access overrides: everything `wms access user` covers
// beyond access.go's group-level permission grid -- groups, 2FA policy,
// channels, expiry, timeouts, note, full name, badge tokens, a local
// solo-mode password, rename and remove.
//
// internal/access.User carries real secrets (PasswordHash, BotPINHash,
// SecurityAnswerHash) that must NEVER be serialized back out -- every view
// struct below omits all three, always, on every response including the
// ones from a write. Only handleAccessUserBadge's own response, and only
// right after issuing/regenerating, ever carries a live badge token or a
// generated password; nowhere else, ever.
package dashboardapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/auth"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/authbridge"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/loginlock"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/twofa"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/users"
)

// numericIDRE mirrors cmd/wms/access.go's own -- a renamed Picker/Checker
// login must stay digits-only (the MC9090's keypad-only login means the ID
// is the permanent username).
var numericIDRE = regexp.MustCompile(`^[0-9]+$`)

type accessUserView struct {
	Key         string   `json:"key"`
	Groups      []string `json:"groups,omitempty"`
	TwoFA       string   `json:"twofa,omitempty"`
	ExemptCIDRs []string `json:"exempt_cidrs,omitempty"`
	Channels    []string `json:"channels,omitempty"`
	Expires     string   `json:"expires,omitempty"`
	GraceMin    *int     `json:"grace_minutes,omitempty"`
	IdleMin     *int     `json:"idle_minutes,omitempty"`
	MaxHours    *int     `json:"max_session_hours,omitempty"`
	Note        string   `json:"note,omitempty"`
	FullName    string   `json:"full_name,omitempty"`
	HasBadge    bool     `json:"has_badge"`
	HasPassword bool     `json:"has_password"`
	Governed    bool     `json:"governed"`
	Dashboard   bool     `json:"dashboard"`
}

func userToView(key string, u *access.User) accessUserView {
	v := accessUserView{Key: key, Governed: u != nil}
	if u == nil {
		return v
	}
	v.Groups, v.TwoFA, v.ExemptCIDRs = u.Groups, u.TwoFA, u.ExemptCIDRs
	v.Channels, v.Expires, v.Note, v.FullName = u.Channels, u.Expires, u.Note, u.FullName
	v.GraceMin, v.IdleMin, v.MaxHours = u.GraceMin, u.IdleMin, u.MaxHours
	v.HasBadge = u.BadgeToken != ""
	v.HasPassword = u.PasswordHash != ""
	v.Dashboard = u.Dashboard
	return v
}

// resolveUserKey mirrors cmd/wms/access.go's own userKey: an explicit
// "source:name" wins outright, else an existing entry is matched by name
// alone, else a brand new key is built from source (defaulting to "wms",
// matching this package's own handleAccessEffective -- the CLI's default
// is "modernwms", but every other access.* endpoint in this file already
// uses "wms" for an unspecified source, so this stays consistent with
// itself rather than the CLI).
func resolveUserKey(p *access.Policy, source, username string) string {
	if strings.Contains(username, ":") {
		return strings.ToLower(username)
	}
	if k, ok := p.FindKey(username); ok {
		return k
	}
	if source == "" {
		source = "wms"
	}
	return access.Key(source, username)
}

// editUser loads+mutates+saves one user's entry, creating it if it doesn't
// exist yet -- mirrors cmd/wms/access.go's own `edit` closure.
func (s *Server) editUser(source, username string, f func(u *access.User) error) (string, error) {
	var key string
	_, err := access.Update(func(p *access.Policy) error {
		key = resolveUserKey(p, source, username)
		u := p.Users[key]
		if u == nil {
			u = &access.User{}
			p.Users[key] = u
		}
		return f(u)
	})
	return key, err
}

func decodeAccessReq(r *http.Request, v any) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return fmt.Errorf("malformed request")
	}
	return nil
}

func writeAccessErr(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

// handleAccessUserShow is GET /dashboard/admin/access/user/{username}?source=.
func (s *Server) handleAccessUserShow(w http.ResponseWriter, r *http.Request) {
	username := r.PathValue("username")
	source := r.URL.Query().Get("source")
	p, err := access.Load()
	if err != nil {
		writeAccessErr(w, http.StatusInternalServerError, err)
		return
	}
	key := resolveUserKey(p, source, username)
	u := p.Users[key]
	view := userToView(key, u)
	if u == nil {
		writeJSON(w, http.StatusOK, map[string]any{"user": view})
		return
	}
	src, name, _ := strings.Cut(key, ":")
	eff := p.Effective(src, name)
	why := make(map[string]string, len(access.All()))
	for _, perm := range access.All() {
		why[perm] = eff.Why(perm)
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": view, "effective": why})
}

// handleAccessUserList is GET /dashboard/admin/access/users.
func (s *Server) handleAccessUserList(w http.ResponseWriter, r *http.Request) {
	p, err := access.Load()
	if err != nil {
		writeAccessErr(w, http.StatusInternalServerError, err)
		return
	}
	views := make([]accessUserView, 0, len(p.Users))
	for key, u := range p.Users {
		views = append(views, userToView(key, u))
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": views})
}

type accessUserBaseRequest struct {
	Username string `json:"username"`
	Source   string `json:"source"`
	By       string `json:"by"`
}

func (req accessUserBaseRequest) trimmed() accessUserBaseRequest {
	req.Username, req.Source, req.By = strings.TrimSpace(req.Username), strings.TrimSpace(req.Source), strings.TrimSpace(req.By)
	return req
}

func (req accessUserBaseRequest) valid() error {
	if req.Username == "" || req.By == "" {
		return fmt.Errorf("username and by are required")
	}
	return nil
}

type setGroupsRequest struct {
	accessUserBaseRequest
	Groups []string `json:"groups"`
}

// handleAccessUserSetGroups is POST /dashboard/admin/access/user/groups.
func (s *Server) handleAccessUserSetGroups(w http.ResponseWriter, r *http.Request) {
	var req setGroupsRequest
	if err := decodeAccessReq(r, &req); err != nil {
		writeAccessErr(w, http.StatusBadRequest, err)
		return
	}
	req.accessUserBaseRequest = req.trimmed()
	if err := req.valid(); err != nil {
		writeAccessErr(w, http.StatusBadRequest, err)
		return
	}
	key, err := s.editUser(req.Source, req.Username, func(u *access.User) error { u.Groups = req.Groups; return nil })
	if err != nil {
		writeAccessErr(w, http.StatusBadRequest, err)
		return
	}
	s.logAudit(req.By, "DASHBOARD_ACCESS_USER_GROUPS_SET", "SUCCESS", key+" groups="+strings.Join(req.Groups, ","))
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

type setTwoFARequest struct {
	accessUserBaseRequest
	// Mode is "", "required" or "exempt" ("" = default policy).
	Mode        string   `json:"mode"`
	ExemptCIDRs []string `json:"exempt_cidrs"`
}

// handleAccessUserSetTwoFA is POST /dashboard/admin/access/user/twofa.
func (s *Server) handleAccessUserSetTwoFA(w http.ResponseWriter, r *http.Request) {
	var req setTwoFARequest
	if err := decodeAccessReq(r, &req); err != nil {
		writeAccessErr(w, http.StatusBadRequest, err)
		return
	}
	req.accessUserBaseRequest = req.trimmed()
	if err := req.valid(); err != nil {
		writeAccessErr(w, http.StatusBadRequest, err)
		return
	}
	req.Mode = strings.ToLower(strings.TrimSpace(req.Mode))
	if req.Mode != "" && req.Mode != access.TwoFARequired && req.Mode != access.TwoFAExempt {
		writeAccessErr(w, http.StatusBadRequest, fmt.Errorf("mode must be required, exempt or empty (default)"))
		return
	}
	key, err := s.editUser(req.Source, req.Username, func(u *access.User) error {
		u.TwoFA = req.Mode
		if u.TwoFA == access.TwoFAExempt {
			u.ExemptCIDRs = req.ExemptCIDRs
		} else {
			u.ExemptCIDRs = nil
		}
		return nil
	})
	if err != nil {
		writeAccessErr(w, http.StatusBadRequest, err)
		return
	}
	s.logAudit(req.By, "DASHBOARD_ACCESS_USER_2FA_SET", "SUCCESS", key+" mode="+req.Mode)
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

type setChannelsRequest struct {
	accessUserBaseRequest
	// Channels empty or omitted means "all" (the CLI's own `all` -> nil).
	Channels []string `json:"channels"`
}

// handleAccessUserSetChannels is POST /dashboard/admin/access/user/channels.
func (s *Server) handleAccessUserSetChannels(w http.ResponseWriter, r *http.Request) {
	var req setChannelsRequest
	if err := decodeAccessReq(r, &req); err != nil {
		writeAccessErr(w, http.StatusBadRequest, err)
		return
	}
	req.accessUserBaseRequest = req.trimmed()
	if err := req.valid(); err != nil {
		writeAccessErr(w, http.StatusBadRequest, err)
		return
	}
	var channels []string
	if len(req.Channels) > 0 {
		channels = req.Channels
	}
	key, err := s.editUser(req.Source, req.Username, func(u *access.User) error { u.Channels = channels; return nil })
	if err != nil {
		writeAccessErr(w, http.StatusBadRequest, err)
		return
	}
	s.logAudit(req.By, "DASHBOARD_ACCESS_USER_CHANNELS_SET", "SUCCESS", key+" channels="+strings.Join(channels, ","))
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

type setDashboardRequest struct {
	accessUserBaseRequest
	On bool `json:"on"`
	// Email is optional -- kc-parts-auth-node synthesizes a placeholder
	// when it's "", the same precedent already used for a Discord sign-in
	// with no public email.
	Email string `json:"email"`
}

type setDashboardResponse struct {
	Saved bool `json:"saved"`
	// TemporaryPassword/MustChangePassword are only set when turning
	// dashboard access ON -- the same one-time-ever shape every other
	// create/reset response in this package already uses.
	TemporaryPassword  string `json:"temporary_password,omitempty"`
	MustChangePassword bool   `json:"must_change_password,omitempty"`
	// ProvisionNote is set when the Go-side password was reset but the
	// dashboard login itself was NOT updated to match -- e.g. a BetterAuth
	// account for this username already existed from an earlier hand-off,
	// so the returned temp password works for telnet/TUI only.
	ProvisionNote string `json:"provision_note,omitempty"`
}

// handleAccessUserSetDashboard is POST /dashboard/admin/access/user/dashboard
// -- the yes/no toggle that auto-provisions a matching kc-parts-auth-node
// login (see internal/authbridge) the moment it's turned on. Turning off
// only clears the policy flag; it never deprovisions the BetterAuth side,
// the same "no automatic deletion" caution every other toggle in this
// package follows.
func (s *Server) handleAccessUserSetDashboard(w http.ResponseWriter, r *http.Request) {
	var req setDashboardRequest
	if err := decodeAccessReq(r, &req); err != nil {
		writeAccessErr(w, http.StatusBadRequest, err)
		return
	}
	req.accessUserBaseRequest = req.trimmed()
	req.Email = strings.TrimSpace(req.Email)
	if err := req.valid(); err != nil {
		writeAccessErr(w, http.StatusBadRequest, err)
		return
	}

	if !req.On {
		key, err := s.editUser(req.Source, req.Username, func(u *access.User) error { u.Dashboard = false; return nil })
		if err != nil {
			writeAccessErr(w, http.StatusBadRequest, err)
			return
		}
		s.logAudit(req.By, "DASHBOARD_ACCESS_USER_DASHBOARD_SET", "SUCCESS", key+" dashboard=off")
		writeJSON(w, http.StatusOK, setDashboardResponse{Saved: true})
		return
	}

	if s.usersSvc == nil {
		writeAccessErr(w, http.StatusServiceUnavailable, fmt.Errorf("account management is not available in this environment"))
		return
	}
	if !authbridge.Configured() {
		writeAccessErr(w, http.StatusServiceUnavailable, fmt.Errorf("dashboard login provisioning is not configured on this server"))
		return
	}

	// A fresh temp password/PIN, same generation rule as `wms users reset`
	// -- there's no other way to hand kc-parts-auth-node a usable
	// plaintext credential, since the Go side only ever stores a hash.
	password := auth.GenerateTempPassword(10)
	if users.NumericLoginUsernameRE.MatchString(req.Username) {
		password = auth.GenerateNumericPIN(6)
	}
	if err := s.usersSvc.ResetUnified(r.Context(), req.Username, password, true); err != nil {
		writeAccessErr(w, http.StatusBadRequest, err)
		return
	}
	result, err := authbridge.Provision(req.Username, req.Email, password)
	if err != nil {
		writeAccessErr(w, http.StatusBadGateway, fmt.Errorf("account password was reset, but dashboard provisioning failed: %w", err))
		return
	}
	key, err := s.editUser(req.Source, req.Username, func(u *access.User) error { u.Dashboard = true; return nil })
	if err != nil {
		writeAccessErr(w, http.StatusBadRequest, err)
		return
	}
	note := ""
	if !result.Provisioned {
		note = result.Reason
	}
	s.logAudit(req.By, "DASHBOARD_ACCESS_USER_DASHBOARD_SET", "SUCCESS", key+" dashboard=on")
	writeJSON(w, http.StatusOK, setDashboardResponse{Saved: true, TemporaryPassword: password, MustChangePassword: true, ProvisionNote: note})
}

type setExpiresRequest struct {
	accessUserBaseRequest
	// Expires is "YYYY-MM-DD", or "" to clear.
	Expires string `json:"expires"`
}

// handleAccessUserSetExpires is POST /dashboard/admin/access/user/expires.
func (s *Server) handleAccessUserSetExpires(w http.ResponseWriter, r *http.Request) {
	var req setExpiresRequest
	if err := decodeAccessReq(r, &req); err != nil {
		writeAccessErr(w, http.StatusBadRequest, err)
		return
	}
	req.accessUserBaseRequest = req.trimmed()
	if err := req.valid(); err != nil {
		writeAccessErr(w, http.StatusBadRequest, err)
		return
	}
	req.Expires = strings.TrimSpace(req.Expires)
	key, err := s.editUser(req.Source, req.Username, func(u *access.User) error { u.Expires = req.Expires; return nil })
	if err != nil {
		writeAccessErr(w, http.StatusBadRequest, err)
		return
	}
	s.logAudit(req.By, "DASHBOARD_ACCESS_USER_EXPIRES_SET", "SUCCESS", key+" expires="+req.Expires)
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

type setTimeoutsRequest struct {
	accessUserBaseRequest
	Grace, Idle, MaxHours           *int
	ClearGrace, ClearIdle, ClearMax bool
}

// handleAccessUserSetTimeouts is POST /dashboard/admin/access/user/timeouts.
// A Clear* flag wins over its paired value (mirrors the CLI's own
// "global" sentinel, spelled as an explicit bool here for JSON-type
// safety rather than overloading a numeric field with a magic string).
func (s *Server) handleAccessUserSetTimeouts(w http.ResponseWriter, r *http.Request) {
	var req setTimeoutsRequest
	if err := decodeAccessReq(r, &req); err != nil {
		writeAccessErr(w, http.StatusBadRequest, err)
		return
	}
	req.accessUserBaseRequest = req.trimmed()
	if err := req.valid(); err != nil {
		writeAccessErr(w, http.StatusBadRequest, err)
		return
	}
	key, err := s.editUser(req.Source, req.Username, func(u *access.User) error {
		if req.ClearGrace {
			u.GraceMin = nil
		} else if req.Grace != nil {
			u.GraceMin = req.Grace
		}
		if req.ClearIdle {
			u.IdleMin = nil
		} else if req.Idle != nil {
			u.IdleMin = req.Idle
		}
		if req.ClearMax {
			u.MaxHours = nil
		} else if req.MaxHours != nil {
			u.MaxHours = req.MaxHours
		}
		return nil
	})
	if err != nil {
		writeAccessErr(w, http.StatusBadRequest, err)
		return
	}
	s.logAudit(req.By, "DASHBOARD_ACCESS_USER_TIMEOUTS_SET", "SUCCESS", key)
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

type setNoteRequest struct {
	accessUserBaseRequest
	Note string `json:"note"`
}

// handleAccessUserSetNote is POST /dashboard/admin/access/user/note.
func (s *Server) handleAccessUserSetNote(w http.ResponseWriter, r *http.Request) {
	var req setNoteRequest
	if err := decodeAccessReq(r, &req); err != nil {
		writeAccessErr(w, http.StatusBadRequest, err)
		return
	}
	req.accessUserBaseRequest = req.trimmed()
	if err := req.valid(); err != nil {
		writeAccessErr(w, http.StatusBadRequest, err)
		return
	}
	key, err := s.editUser(req.Source, req.Username, func(u *access.User) error { u.Note = req.Note; return nil })
	if err != nil {
		writeAccessErr(w, http.StatusBadRequest, err)
		return
	}
	s.logAudit(req.By, "DASHBOARD_ACCESS_USER_NOTE_SET", "SUCCESS", key)
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

type setNameRequest struct {
	accessUserBaseRequest
	FullName string `json:"full_name"`
}

// handleAccessUserSetName is POST /dashboard/admin/access/user/name.
func (s *Server) handleAccessUserSetName(w http.ResponseWriter, r *http.Request) {
	var req setNameRequest
	if err := decodeAccessReq(r, &req); err != nil {
		writeAccessErr(w, http.StatusBadRequest, err)
		return
	}
	req.accessUserBaseRequest = req.trimmed()
	if err := req.valid(); err != nil {
		writeAccessErr(w, http.StatusBadRequest, err)
		return
	}
	req.FullName = strings.TrimSpace(req.FullName)
	key, err := s.editUser(req.Source, req.Username, func(u *access.User) error { u.FullName = req.FullName; return nil })
	if err != nil {
		writeAccessErr(w, http.StatusBadRequest, err)
		return
	}
	s.logAudit(req.By, "DASHBOARD_ACCESS_USER_NAME_SET", "SUCCESS", key)
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

// --- sensitive: badge, password, rename, remove -- all confirm-gated ---

func requireConfirm(confirm string) error {
	if strings.TrimSpace(confirm) != "yes" {
		return fmt.Errorf(`type "yes" to confirm`)
	}
	return nil
}

type badgeRequest struct {
	accessUserBaseRequest
	// Action is "regenerate" or "clear".
	Action  string `json:"action"`
	Confirm string `json:"confirm"`
}

// handleAccessUserBadge is POST /dashboard/admin/access/user/badge. The new
// token is only ever returned from a "regenerate" response -- nowhere else.
func (s *Server) handleAccessUserBadge(w http.ResponseWriter, r *http.Request) {
	var req badgeRequest
	if err := decodeAccessReq(r, &req); err != nil {
		writeAccessErr(w, http.StatusBadRequest, err)
		return
	}
	req.accessUserBaseRequest = req.trimmed()
	if err := req.valid(); err != nil {
		writeAccessErr(w, http.StatusBadRequest, err)
		return
	}
	if err := requireConfirm(req.Confirm); err != nil {
		writeAccessErr(w, http.StatusBadRequest, err)
		return
	}
	switch strings.ToLower(strings.TrimSpace(req.Action)) {
	case "regenerate":
		var token string
		key, err := s.editUser(req.Source, req.Username, func(u *access.User) error {
			tok, err := access.NewBadgeToken()
			if err != nil {
				return err
			}
			u.BadgeToken, token = tok, tok
			return nil
		})
		if err != nil {
			writeAccessErr(w, http.StatusBadRequest, err)
			return
		}
		s.logAudit(req.By, "DASHBOARD_ACCESS_BADGE_ISSUED", "SUCCESS", key)
		writeJSON(w, http.StatusOK, map[string]any{"saved": true, "badge_token": token})
	case "clear":
		key, err := s.editUser(req.Source, req.Username, func(u *access.User) error { u.BadgeToken = ""; return nil })
		if err != nil {
			writeAccessErr(w, http.StatusBadRequest, err)
			return
		}
		s.logAudit(req.By, "DASHBOARD_ACCESS_BADGE_CLEARED", "SUCCESS", key)
		writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
	default:
		writeAccessErr(w, http.StatusBadRequest, fmt.Errorf("action must be regenerate or clear"))
	}
}

type setPasswordRequest struct {
	accessUserBaseRequest
	Password string `json:"password"`
	Generate bool   `json:"generate"`
	Confirm  string `json:"confirm"`
}

// handleAccessUserSetPassword is POST /dashboard/admin/access/user/password
// -- a local solo-mode sign-in secret, independent of ModernWMS/Part-DB
// (see internal/access.User.PasswordHash's own doc comment). The plaintext
// is only ever returned from this one response, when generated here.
func (s *Server) handleAccessUserSetPassword(w http.ResponseWriter, r *http.Request) {
	var req setPasswordRequest
	if err := decodeAccessReq(r, &req); err != nil {
		writeAccessErr(w, http.StatusBadRequest, err)
		return
	}
	req.accessUserBaseRequest = req.trimmed()
	if err := req.valid(); err != nil {
		writeAccessErr(w, http.StatusBadRequest, err)
		return
	}
	if err := requireConfirm(req.Confirm); err != nil {
		writeAccessErr(w, http.StatusBadRequest, err)
		return
	}
	pw := strings.TrimSpace(req.Password)
	generated := req.Generate || pw == ""
	if generated {
		pw = auth.GenerateTempPassword(12)
	}
	hash, err := auth.HashPartDB(pw)
	if err != nil {
		writeAccessErr(w, http.StatusInternalServerError, err)
		return
	}
	key, err := s.editUser(req.Source, req.Username, func(u *access.User) error { u.PasswordHash = hash; return nil })
	if err != nil {
		writeAccessErr(w, http.StatusBadRequest, err)
		return
	}
	s.logAudit(req.By, "DASHBOARD_ACCESS_USER_PASSWORD_SET", "SUCCESS", key)
	resp := map[string]any{"saved": true}
	if generated {
		resp["password"] = pw
	}
	writeJSON(w, http.StatusOK, resp)
}

type renameUserRequest struct {
	OldID   string `json:"old_id"`
	NewID   string `json:"new_id"`
	Source  string `json:"source"`
	Confirm string `json:"confirm"`
	By      string `json:"by"`
}

// renameWarning is shown verbatim by the dashboard UI -- the same fact the
// CLI's own renameCmd doc comment states, now surfaced where a web admin
// (who never reads the CLI's --help) will actually see it.
const renameWarning = "The audit log is never rewritten: past sign-ins and actions stay attributed to the old ID forever -- search both IDs for a full history."

// handleAccessUserRename is POST /dashboard/admin/access/user/rename --
// moves ticket/accuracy/kanban/attendance history, 2FA enrollment and
// login-lockout state to the new ID, in that order (safest-first: if a
// later step fails, the account still signs in under the old ID).
func (s *Server) handleAccessUserRename(w http.ResponseWriter, r *http.Request) {
	var req renameUserRequest
	if err := decodeAccessReq(r, &req); err != nil {
		writeAccessErr(w, http.StatusBadRequest, err)
		return
	}
	req.OldID, req.NewID = strings.TrimSpace(req.OldID), strings.TrimSpace(req.NewID)
	req.Source, req.By = strings.TrimSpace(req.Source), strings.TrimSpace(req.By)
	if req.OldID == "" || req.NewID == "" || req.By == "" {
		writeAccessErr(w, http.StatusBadRequest, fmt.Errorf("old_id, new_id and by are required"))
		return
	}
	if err := requireConfirm(req.Confirm); err != nil {
		writeAccessErr(w, http.StatusBadRequest, err)
		return
	}
	if !numericIDRE.MatchString(req.NewID) {
		writeAccessErr(w, http.StatusBadRequest, fmt.Errorf("the new ID must be digits only, got %q", req.NewID))
		return
	}
	p, err := access.Load()
	if err != nil {
		writeAccessErr(w, http.StatusInternalServerError, err)
		return
	}
	oldKey := resolveUserKey(p, req.Source, req.OldID)
	if p.Users[oldKey] == nil {
		writeAccessErr(w, http.StatusBadRequest, fmt.Errorf("%s has no entry", oldKey))
		return
	}
	source, _, _ := strings.Cut(oldKey, ":")
	newKey := access.Key(source, req.NewID)
	if p.Users[newKey] != nil {
		writeAccessErr(w, http.StatusBadRequest, fmt.Errorf("%s already has an entry", newKey))
		return
	}
	if s.legoDB == nil {
		writeAccessErr(w, http.StatusServiceUnavailable, fmt.Errorf("the collection database is not available"))
		return
	}
	if err := s.legoDB.RenameUser(req.OldID, req.NewID); err != nil {
		writeAccessErr(w, http.StatusBadRequest, fmt.Errorf("ticket/accuracy/kanban history was not moved, nothing changed: %w", err))
		return
	}
	var warnings []string
	if err := twofa.Rename(req.OldID, req.NewID, source); err != nil {
		warnings = append(warnings, "2FA enrollment was not moved: "+err.Error())
	}
	if err := loginlock.Rename(req.OldID, req.NewID); err != nil {
		warnings = append(warnings, "login-lockout state was not moved: "+err.Error())
	}
	if _, err := access.Update(func(p *access.Policy) error {
		key := resolveUserKey(p, req.Source, req.OldID)
		u := p.Users[key]
		if u == nil {
			return fmt.Errorf("%s has no entry", key)
		}
		src, _, _ := strings.Cut(key, ":")
		nk := access.Key(src, req.NewID)
		if p.Users[nk] != nil {
			return fmt.Errorf("%s already has an entry", nk)
		}
		delete(p.Users, key)
		p.Users[nk] = u
		return nil
	}); err != nil {
		writeAccessErr(w, http.StatusBadRequest, err)
		return
	}
	s.logAudit(req.By, "DASHBOARD_ACCESS_USER_RENAMED", "SUCCESS", fmt.Sprintf("old=%s new=%s source=%s", req.OldID, req.NewID, source))
	writeJSON(w, http.StatusOK, map[string]any{"old": req.OldID, "new": req.NewID, "source": source, "warning": renameWarning, "warnings": warnings})
}

type removeUserRequest struct {
	accessUserBaseRequest
	Confirm string `json:"confirm"`
}

// handleAccessUserRemove is POST /dashboard/admin/access/user/remove --
// drops the policy entry; the account falls back to its ModernWMS role's
// default access. Does not touch ticket/accuracy history or the account
// itself (see accounts.go's handleDeleteAccount for that).
func (s *Server) handleAccessUserRemove(w http.ResponseWriter, r *http.Request) {
	var req removeUserRequest
	if err := decodeAccessReq(r, &req); err != nil {
		writeAccessErr(w, http.StatusBadRequest, err)
		return
	}
	req.accessUserBaseRequest = req.trimmed()
	if err := req.valid(); err != nil {
		writeAccessErr(w, http.StatusBadRequest, err)
		return
	}
	if err := requireConfirm(req.Confirm); err != nil {
		writeAccessErr(w, http.StatusBadRequest, err)
		return
	}
	var key string
	_, err := access.Update(func(p *access.Policy) error {
		key = resolveUserKey(p, req.Source, req.Username)
		if p.Users[key] == nil {
			return fmt.Errorf("%s has no entry", key)
		}
		delete(p.Users, key)
		return nil
	})
	if err != nil {
		writeAccessErr(w, http.StatusBadRequest, err)
		return
	}
	s.logAudit(req.By, "DASHBOARD_ACCESS_USER_REMOVED", "SUCCESS", key)
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}
