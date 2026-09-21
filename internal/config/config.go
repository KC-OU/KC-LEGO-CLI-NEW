// Package config centralizes environment-variable defaults, mirroring the
// original Python suite's .env.example so every package reads the same
// variable names from one place instead of duplicating os.Getenv fallbacks.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

func Env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

const (
	ModernWMSContainer      = "MODERNWMS_CONTAINER"
	ModernWMSDBPath         = "MODERNWMS_DB_PATH"
	ModernWMSBackupDir      = "MODERNWMS_BACKUP_DIR"
	PartDBContainer         = "PARTDB_CONTAINER"
	PartDBDBPath            = "PARTDB_DB_PATH"
	AuditLogFile            = "AUDIT_LOG_FILE"
	SyncPort                = "PORT"
	SyncAdminUser           = "SYNC_ADMIN_USER"
	SyncAdminPass           = "SYNC_ADMIN_PASS"
	SyncViewOnlyAuth        = "SYNC_VIEWONLY_AUTH"
	SyncViewOnlyEmail       = "SYNC_VIEWONLY_EMAIL"
	CredentialsFile         = "CREDENTIALS_FILE"
	LinkOverridesFile       = "LINK_OVERRIDES_FILE"
	TwoFAFile               = "TWOFA_FILE"
	TUITheme                = "MODERNWMS_TUI_THEME"
	ListenPorts             = "LISTEN_PORTS"
	GatewayPort             = "GATEWAY_PORT"
	TTYDPort                = "TTYD_PORT"
	PartDBURL               = "PARTDB_URL"
	ModernWMSURL            = "MODERNWMS_URL"
	LegoDBPath              = "LEGO_DB_PATH"
	RebrickableAPIKey       = "REBRICKABLE_API_KEY"
	SettingsFile            = "WMS_SETTINGS_FILE"
	TwoFAGraceMinutes       = "TWOFA_GRACE_MINUTES"
	PartDBAPIURL            = "PARTDB_API_URL"
	PartDBAPIToken          = "PARTDB_API_TOKEN"
	IdleLockMinutes         = "TUI_IDLE_LOCK_MINUTES"
	CatalogAutoRefreshHours = "CATALOG_AUTO_REFRESH_HOURS"
	NotifyURL               = "NOTIFY_URL"
	NotifyFormat            = "NOTIFY_FORMAT"
	ImageDir                = "WMS_IMAGE_DIR"
	TUIImages               = "MODERNWMS_TUI_IMAGES"
	Equivalents             = "WMS_EQUIVALENTS"
	PluginDir               = "WMS_PLUGIN_DIR"
	PluginUser              = "WMS_PLUGIN_USER"
	BricklinkConsumerKey    = "BRICKLINK_CONSUMER_KEY"
	BricklinkConsumerSecret = "BRICKLINK_CONSUMER_SECRET"
	BricklinkToken          = "BRICKLINK_TOKEN"
	BricklinkTokenSecret    = "BRICKLINK_TOKEN_SECRET"
	BricklinkCurrency       = "BRICKLINK_CURRENCY"
	BricklinkRegion         = "BRICKLINK_REGION"
	BricklinkCondition      = "BRICKLINK_CONDITION"
	BricklinkDailyBudget    = "BRICKLINK_DAILY_BUDGET"
)

func Defaults() map[string]string {
	return map[string]string{
		ModernWMSContainer:      "modernwms",
		ModernWMSDBPath:         "/app/wms.db",
		ModernWMSBackupDir:      "/root/backups/modernwms",
		PartDBContainer:         "partdb",
		PartDBDBPath:            "/root/docker-server/partdb/db/app.db",
		AuditLogFile:            "/root/tui_audit.log",
		SyncPort:                "8082",
		SyncAdminUser:           "admin",
		ListenPorts:             "2323,23",
		GatewayPort:             "7681",
		TTYDPort:                "7682",
		CredentialsFile:         "/root/docker-server/partdb-sync/config/credentials.json",
		LinkOverridesFile:       "/root/docker-server/partdb-sync/config/link_overrides.json",
		TwoFAFile:               "/root/docker-server/wms/2fa.json",
		PartDBURL:               "", // your Part-DB address, e.g. https://partdb.example.com/ (links are omitted when empty)
		ModernWMSURL:            "", // your ModernWMS address
		LegoDBPath:              "/root/docker-server/wms/lego.db",
		SettingsFile:            "/root/docker-server/wms/settings.json",
		TwoFAGraceMinutes:       "30",
		PartDBAPIURL:            "http://127.0.0.1:8081",
		IdleLockMinutes:         "15",
		CatalogAutoRefreshHours: "0",
		ImageDir:                "/root/docker-server/wms/imgcache",
		PluginDir:               "/root/docker-server/wms/plugins",
		PluginUser:              "nobody",
		BricklinkCurrency:       "GBP",
		BricklinkRegion:         "europe",
		BricklinkCondition:      "U",
		BricklinkDailyBudget:    "4500",
	}
	// SyncAdminPass intentionally has no default here (see credentialStore.get
	// in internal/api/credentials.go): bootstrapping the sync dashboard's
	// admin account from a hardcoded fallback password shipped it live with
	// "admin123" in production. An empty SyncAdminPass must be treated as
	// "not configured", not "use the well-known default".
}

// Get reads key from the environment, falling back to Defaults()[key], but a
// value saved via SetOverride wins over both — this is how the TUI's
// Settings screen changes a value (e.g. REBRICKABLE_API_KEY) that takes
// effect immediately, including for processes started after this one: an
// env var set by systemd can't be changed by a running child process.
func Get(key string) string {
	if v, ok := loadOverrides()[key]; ok && v != "" {
		return v
	}
	return Env(key, Defaults()[key])
}

func loadOverrides() map[string]string {
	data, err := os.ReadFile(Env(SettingsFile, Defaults()[SettingsFile]))
	if err != nil {
		return nil
	}
	var m map[string]string
	if json.Unmarshal(data, &m) != nil {
		return nil
	}
	return m
}

// SetOverride persists key=value into the settings override file (root-only,
// mode 0600, matching internal/api/credentials.go's credentials.json),
// creating it and its parent directory on first use.
func SetOverride(key, value string) error {
	path := Env(SettingsFile, Defaults()[SettingsFile])
	m := loadOverrides()
	if m == nil {
		m = map[string]string{}
	}
	m[key] = value
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}
