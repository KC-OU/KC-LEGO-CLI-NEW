// Per-account language preference, shared between the dashboard and the
// TUI/CLI (not the CLI's own output -- it has no identity to key a
// preference off, see cmd/wms's $SUDO_USER/$USER-only attribution).
package dashboardapi

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

type setLangRequest struct {
	Username string `json:"username"`
	Lang     string `json:"lang"`
}

// handleSetLang is POST /dashboard/lang -- writes this username's TUI/CLI
// language preference into WMS_USER_PREFS_FILE (internal/uiapp/
// theme_picker.go's userPref.Lang), so picking a language in the dashboard
// also updates the linked TUI account. Merges one key into the existing
// per-user JSON object rather than importing internal/uiapp directly --
// every other dashboardapi file mirrors uiapp's logic instead of depending
// on it (see e.g. ops.go, rota.go), and this follows the same convention.
func (s *Server) handleSetLang(w http.ResponseWriter, r *http.Request) {
	var req setLangRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username is required"})
		return
	}
	path := config.Get(config.UserPrefsFile)
	m := map[string]map[string]json.RawMessage{}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &m) // a damaged file is no preferences, not an error
	}
	if m[req.Username] == nil {
		m[req.Username] = map[string]json.RawMessage{}
	}
	langJSON, _ := json.Marshal(req.Lang)
	m[req.Username]["lang"] = langJSON
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"username": req.Username, "lang": req.Lang})
}
