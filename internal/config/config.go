// Package config centralizes environment-variable defaults, mirroring the
// original Python suite's .env.example so every package reads the same
// variable names from one place instead of duplicating os.Getenv fallbacks.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func Env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

const (
	ModernWMSContainer         = "MODERNWMS_CONTAINER"
	ModernWMSDBPath            = "MODERNWMS_DB_PATH"
	ModernWMSBackupDir         = "MODERNWMS_BACKUP_DIR"
	PartDBContainer            = "PARTDB_CONTAINER"
	PartDBDBPath               = "PARTDB_DB_PATH"
	AuditLogFile               = "AUDIT_LOG_FILE"
	SyncPort                   = "PORT"
	SyncAdminUser              = "SYNC_ADMIN_USER"
	SyncAdminPass              = "SYNC_ADMIN_PASS"
	SyncViewOnlyAuth           = "SYNC_VIEWONLY_AUTH"
	SyncViewOnlyEmail          = "SYNC_VIEWONLY_EMAIL"
	CredentialsFile            = "CREDENTIALS_FILE"
	LinkOverridesFile          = "LINK_OVERRIDES_FILE"
	TwoFAFile                  = "TWOFA_FILE"
	TUITheme                   = "MODERNWMS_TUI_THEME"
	ListenPorts                = "LISTEN_PORTS"
	GatewayPort                = "GATEWAY_PORT"
	GatewayListenHost          = "GATEWAY_LISTEN_HOST"
	TTYDPort                   = "TTYD_PORT"
	WebSoloDirectPort          = "WMS_WEB_SOLO_DIRECT_PORT"
	MetricsPort                = "WMS_METRICS_PORT"
	BotAPIPort                 = "WMS_BOT_API_PORT"
	PartDBURL                  = "PARTDB_URL"
	ModernWMSURL               = "MODERNWMS_URL"
	LegoDBPath                 = "LEGO_DB_PATH"
	RebrickableAPIKey          = "REBRICKABLE_API_KEY"
	SettingsFile               = "WMS_SETTINGS_FILE"
	TwoFAGraceMinutes          = "TWOFA_GRACE_MINUTES"
	PartDBAPIURL               = "PARTDB_API_URL"
	PartDBAPIToken             = "PARTDB_API_TOKEN"
	IdleLockMinutes            = "TUI_IDLE_LOCK_MINUTES"
	CatalogAutoRefreshHours    = "CATALOG_AUTO_REFRESH_HOURS"
	RetirementSheetURL         = "LEGO_RETIREMENT_SHEET_URL"
	RetirementAutoRefreshHours = "RETIREMENT_AUTO_REFRESH_HOURS"
	NotifyURL                  = "NOTIFY_URL"
	NotifyFormat               = "NOTIFY_FORMAT"
	ImageDir                   = "WMS_IMAGE_DIR"
	TUIImages                  = "MODERNWMS_TUI_IMAGES"
	Equivalents                = "WMS_EQUIVALENTS"
	PluginDir                  = "WMS_PLUGIN_DIR"
	ExportDir                  = "WMS_EXPORT_DIR"
	PublicURL                  = "WMS_PUBLIC_URL"
	TUISplash                  = "MODERNWMS_TUI_SPLASH"
	UserPrefsFile              = "WMS_USER_PREFS_FILE"
	AccessFile                 = "WMS_ACCESS_FILE"
	BrickOwlAPIKey             = "BRICKOWL_API_KEY"
	BrickOwlCountry            = "BRICKOWL_COUNTRY"
	SignOnPublicStats          = "WMS_SIGNON_PUBLIC_STATS"
	MOTD                       = "WMS_MOTD"
	NotifyProviders            = "WMS_NOTIFY_PROVIDERS"
	PluginUser                 = "WMS_PLUGIN_USER"
	BricklinkConsumerKey       = "BRICKLINK_CONSUMER_KEY"
	BricklinkConsumerSecret    = "BRICKLINK_CONSUMER_SECRET"
	BricklinkToken             = "BRICKLINK_TOKEN"
	BricklinkTokenSecret       = "BRICKLINK_TOKEN_SECRET"
	BricklinkCurrency          = "BRICKLINK_CURRENCY"
	BricklinkRegion            = "BRICKLINK_REGION"
	BricklinkCondition         = "BRICKLINK_CONDITION"
	BricklinkDailyBudget       = "BRICKLINK_DAILY_BUDGET"
	DiscordBotToken            = "WMS_DISCORD_BOT_TOKEN"
	DiscordBotUserID           = "WMS_DISCORD_BOT_USER_ID"
	ArchiveDir                 = "WMS_ARCHIVE_DIR"
	TestBinaryPath             = "WMS_TEST_BINARY_PATH"
	TestEnvFile                = "WMS_TEST_ENV_FILE"
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
		GatewayListenHost:       "127.0.0.1", // loopback: reach it through the HTTPS tunnel; 0.0.0.0 exposes raw telnet
		TTYDPort:                "7682",
		MetricsPort:             "", // empty: /metrics is off until set (listens on every interface for the dockerised Prometheus, never reverse-proxied)
		BotAPIPort:              "", // empty: the remote-bot webhook (internal/botapi) is off until set, and always loopback-only when it is
		WebSoloDirectPort:       "", // empty: off; set to put /solo's terminal on its own port with no base path, for a reverse proxy/tunnel that can't rewrite paths to reach it at its own root
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
		// The "Brick Tap" community retirement-date sheet's default (public) CSV export — a
		// third-party document, not an official API; override or use `wms lego retirement import`
		// if it ever moves, is restructured, or you'd rather point at a different tab/copy. The
		// gid picks the actual retirement-data tab: the sheet's default tab (no gid) is its
		// Changelog, which has no Set # column at all.
		RetirementSheetURL:         "https://docs.google.com/spreadsheets/d/1rlYfEXtNKxUOZt2Mfv0H17DvK7bj6Pe0CuYwq6ay8WA/export?format=csv&gid=1742130415",
		RetirementAutoRefreshHours: "0", // off by default; `wms lego retirement refresh` still works on demand
		ImageDir:                   "/root/docker-server/wms/imgcache",
		PluginDir:                  "/root/docker-server/wms/plugins",
		ExportDir:                  "/root/docker-server/wms/exports",
		PublicURL:                  "", // the web terminal's https address, e.g. https://lego-tui.example.com; empty = no download links
		TUISplash:                  "1",
		UserPrefsFile:              "/root/docker-server/wms/user-prefs.json",
		AccessFile:                 "/root/docker-server/wms/access.json",
		BrickOwlAPIKey:             "",
		BrickOwlCountry:            "GB",
		SignOnPublicStats:          "1",
		MOTD:                       "",
		NotifyProviders:            "/root/docker-server/wms/notify-provider-config.yaml",
		PluginUser:                 "nobody",
		BricklinkCurrency:          "GBP",
		BricklinkRegion:            "europe",
		BricklinkCondition:         "U",
		BricklinkDailyBudget:       "4500",
		DiscordBotToken:            "", // from Discord's Developer Portal (discord.com/developers/applications): create an app, add a Bot, copy its token
		DiscordBotUserID:           "", // the recipient's Discord user ID (Discord: enable Developer Mode, right-click your name, Copy User ID)
		ArchiveDir:                 "/root/docker-server/wms/archive",
		// Both empty by default: the telnet/web gateway only offers a "connect to the dev
		// build instead" picker when an admin has deliberately set both, on the LIVE
		// gateway's own env — never on the test instance itself (see docs/guides/
		// telnet-and-web.md). TestBinaryPath is the dev-built binary (e.g.
		// /usr/local/bin/wms-go-test); TestEnvFile is that instance's own EnvironmentFile
		// (e.g. /root/.config/wms-go/test.env), read the same way systemd would.
		TestBinaryPath: "",
		TestEnvFile:    "",
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
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil { // holds API keys and tokens (settings.json)
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// ReadEnvFile reads a systemd EnvironmentFile-style file: one KEY=VALUE per
// line, blank lines and lines starting with # ignored, no quoting or
// variable expansion — the same shape every systemd unit in this project
// already uses (e.g. /root/.config/wms-go/test.env). Used to hand a second
// instance's whole environment to a child process started by a different
// one (see WMS_TEST_ENV_FILE, internal/gateway's Live/Test picker) without
// re-implementing systemd's own parsing beyond this.
func ReadEnvFile(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.Contains(line, "=") {
			return nil, fmt.Errorf("%s: invalid line %q (want KEY=VALUE)", path, line)
		}
		out = append(out, line)
	}
	return out, nil
}
