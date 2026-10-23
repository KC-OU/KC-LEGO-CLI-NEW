// The dashboard's own per-user permission mirror: GET /dashboard/me/
// permissions reuses internal/access.Policy.Effective directly, so a
// dashboard-linked account (see internal/dashboardlink) is restricted by
// exactly the same groups/perms the TUI itself enforces -- not a second,
// parallel permission system.
//
// An ungoverned username (no access.Policy entry at all) gets every
// permission denied here. This is a deliberate simplification, not an
// oversight: internal/uiapp's own legacyCan has a second fallback for an
// ungoverned TUI session (derived from that account's real ModernWMS/
// Part-DB role), but replicating that a second time on a different
// surface risks exactly the kind of subtle mismatch dashboard-linking
// itself already ran into once (see internal/dashboardlink's own
// comment). An admin who wants a dashboard-linked account to see
// anything beyond its own profile sets up its groups on the Access
// Control page -- the same system every other permission already goes
// through.
package dashboardapi

import (
	"net/http"
	"strings"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
)

// handleMyPermissions is GET /dashboard/me/permissions?username=&source=.
func (s *Server) handleMyPermissions(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimSpace(r.URL.Query().Get("username"))
	if username == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username is required"})
		return
	}
	source := r.URL.Query().Get("source")
	p, err := access.Load()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	key := resolveUserKey(p, source, username)
	src, name, _ := strings.Cut(key, ":")
	eff := p.Effective(src, name)
	perms := make(map[string]bool, len(access.All()))
	for _, perm := range access.All() {
		perms[perm] = eff.Can(perm)
	}
	writeJSON(w, http.StatusOK, map[string]any{"permissions": perms, "governed": p.Has(src, name)})
}
