// Settings & API Keys: Rebrickable/BrickLink/Part-DB credentials, the
// Sync Dashboard admin login, and the TUI display theme. All five are
// plain key/value pairs in config's override file (internal/config.Get/
// SetOverride) except the sync-admin login, which lives in its own
// credentials store (internal/api). Mirrors internal/uiapp/
// settings_screens.go and bricklink_screens.go.
package dashboardapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/api"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

// maskSecret shows only the last 4 characters -- same convention as
// internal/uiapp/settings_screens.go's own helper (not reused directly:
// that one lives in a package this one shouldn't need to import).
func maskSecret(v string) string {
	if v == "" {
		return ""
	}
	if len(v) <= 4 {
		return strings.Repeat("*", len(v))
	}
	return strings.Repeat("*", len(v)-4) + v[len(v)-4:]
}

// handleGetSettings returns every secret masked and every plain value as
// given -- never the sync-admin password (write-only, see
// handleSetSyncAdmin), only its username.
func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	adminUsername, _ := api.AdminUsername()
	writeJSON(w, http.StatusOK, map[string]any{
		"rebrickable_api_key":       maskSecret(config.Get(config.RebrickableAPIKey)),
		"bricklink_consumer_key":    maskSecret(config.Get(config.BricklinkConsumerKey)),
		"bricklink_consumer_secret": maskSecret(config.Get(config.BricklinkConsumerSecret)),
		"bricklink_token":           maskSecret(config.Get(config.BricklinkToken)),
		"bricklink_token_secret":    maskSecret(config.Get(config.BricklinkTokenSecret)),
		"bricklink_currency":        config.Get(config.BricklinkCurrency),
		"bricklink_region":          config.Get(config.BricklinkRegion),
		"partdb_api_url":            config.Get(config.PartDBAPIURL),
		"partdb_api_token":          maskSecret(config.Get(config.PartDBAPIToken)),
		"tui_theme":                 config.Get(config.TUITheme),
		"sync_admin_username":       adminUsername,
	})
}

// settingsFields maps the request's JSON keys to the config key each one
// overrides -- every field here is optional; only keys present and
// non-empty in the request body are changed, so re-GETting and re-POSTing
// the (masked) response back can never clobber a secret with "****1234".
var settingsFields = map[string]string{
	"rebrickable_api_key":       config.RebrickableAPIKey,
	"bricklink_consumer_key":    config.BricklinkConsumerKey,
	"bricklink_consumer_secret": config.BricklinkConsumerSecret,
	"bricklink_token":           config.BricklinkToken,
	"bricklink_token_secret":    config.BricklinkTokenSecret,
	"bricklink_currency":        config.BricklinkCurrency,
	"bricklink_region":          config.BricklinkRegion,
	"partdb_api_url":            config.PartDBAPIURL,
	"partdb_api_token":          config.PartDBAPIToken,
	"tui_theme":                 config.TUITheme,
}

type setSettingsRequest struct {
	Values map[string]string `json:"values"`
	By     string            `json:"by"`
}

func (s *Server) handleSetSettings(w http.ResponseWriter, r *http.Request) {
	var req setSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	req.By = strings.TrimSpace(req.By)
	if req.By == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "by is required"})
		return
	}
	var changed []string
	for field, value := range req.Values {
		key, ok := settingsFields[field]
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown setting: " + field})
			return
		}
		if value == "" {
			continue
		}
		if err := config.SetOverride(key, value); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		changed = append(changed, field)
	}
	s.logAudit(req.By, "DASHBOARD_SETTINGS_CHANGED", "SUCCESS", strings.Join(changed, ","))
	writeJSON(w, http.StatusOK, map[string]any{"changed": changed})
}

type setSyncAdminRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	By       string `json:"by"`
}

// handleSetSyncAdmin changes the Sync Dashboard's own admin login -- a
// separate credentials store from everything else here, and the password
// is never read back (api.AdminUsername only ever returns the username).
func (s *Server) handleSetSyncAdmin(w http.ResponseWriter, r *http.Request) {
	var req setSyncAdminRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	req.Username, req.By = strings.TrimSpace(req.Username), strings.TrimSpace(req.By)
	if req.Username == "" || req.Password == "" || req.By == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username, password and by are required"})
		return
	}
	if err := api.SetAdminCredentials(req.Username, req.Password); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(req.By, "DASHBOARD_SYNC_ADMIN_CHANGED", "SUCCESS", req.Username)
	writeJSON(w, http.StatusOK, map[string]string{"username": req.Username})
}
