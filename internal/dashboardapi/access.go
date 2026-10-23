// Access Control: a read-only view of groups/permissions and one user's
// effective permissions, plus the one write path (toggle a group's
// permission). internal/access.Policy also carries per-user secrets
// (password/PIN hashes, security answers) in its Users map, so this
// deliberately never serializes a *access.User -- only Group.Perms (safe)
// and Policy.Effective's already-sanitized Perms type.
//
// Scope cut: full user-level overrides (2FA/sign-in-limits/timeouts/menu
// customization/remote-bot linking) stay CLI/TUI-only for now -- those
// touch the same User struct that holds the secrets above, and need their
// own careful read/write shape rather than reusing this file's. Group
// create/delete is also out of scope here; only toggling an existing
// group's existing permissions is wired up.
package dashboardapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
)

var errUnknownGroup = errors.New("unknown group")

type accessGroupView struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Perms       map[string]string `json:"perms"`
	Restricted  bool              `json:"restricted"`
}

// handleAccessGroups lists every group's permission grid plus the full
// permission catalog (access.All()) so the dashboard can render a
// checkbox/select grid without hardcoding the permission list.
func (s *Server) handleAccessGroups(w http.ResponseWriter, r *http.Request) {
	p, err := access.Load()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	groups := make([]accessGroupView, 0, len(p.Groups))
	for name, g := range p.Groups {
		groups = append(groups, accessGroupView{Name: name, Description: g.Description, Perms: g.Perms, Restricted: g.Restricted})
	}
	writeJSON(w, http.StatusOK, map[string]any{"groups": groups, "permissions": access.All(), "areas": access.Areas})
}

// handleAccessEffective is "check a user's effective permissions" -- for
// every known permission, whether source:username can use it and why
// (group/user override/not granted), from access.Policy.Effective.
func (s *Server) handleAccessEffective(w http.ResponseWriter, r *http.Request) {
	source := r.URL.Query().Get("source")
	username := r.URL.Query().Get("username")
	if strings.TrimSpace(username) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username is required"})
		return
	}
	if source == "" {
		source = "wms"
	}
	p, err := access.Load()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	eff := p.Effective(source, username)
	perms := make(map[string]map[string]any, len(access.All()))
	for _, perm := range access.All() {
		perms[perm] = map[string]any{"allowed": eff.Can(perm), "why": eff.Why(perm)}
	}
	writeJSON(w, http.StatusOK, map[string]any{"source": source, "username": username, "permissions": perms})
}

type setGroupPermRequest struct {
	Group string `json:"group"`
	Perm  string `json:"perm"`
	// Value is "allow", "deny", or "" to clear the override back to inherit.
	Value string `json:"value"`
	By    string `json:"by"`
}

// handleSetGroupPerm assigns or revokes one permission on an existing
// group -- access.Update's own Validate() rejects an unknown group,
// unknown permission, or bad value, so this only needs its own basic
// field checks before handing the closure off.
//
// Note: the TUI's own saveAccess also refuses a change that would strip
// the acting admin's own access.manage permission (a self-lockout guard).
// That guard lives in internal/uiapp, not internal/access, and has no
// caller identity to check here (dashboardapi trusts only the Rust
// service's shared bearer token, not a per-admin session) -- so this
// route can lock an admin out of Access Control entirely if misused.
func (s *Server) handleSetGroupPerm(w http.ResponseWriter, r *http.Request) {
	var req setGroupPermRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	req.Group, req.Perm, req.By = strings.TrimSpace(req.Group), strings.TrimSpace(req.Perm), strings.TrimSpace(req.By)
	req.Value = strings.TrimSpace(req.Value)
	if req.Group == "" || req.Perm == "" || req.By == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "group, perm and by are required"})
		return
	}
	_, err := access.Update(func(p *access.Policy) error {
		g, ok := p.Groups[req.Group]
		if !ok {
			return errUnknownGroup
		}
		if g.Perms == nil {
			g.Perms = map[string]string{}
		}
		if req.Value == "" {
			delete(g.Perms, req.Perm)
		} else {
			g.Perms[req.Perm] = req.Value
		}
		return nil
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(req.By, "DASHBOARD_ACCESS_PERM_SET", "SUCCESS", req.Group+" "+req.Perm+"="+req.Value)
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}
