package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestMain points every path the program can write at a throwaway folder, so a test that forgets
// to set its own can never open the real collection, settings, audit log or 2FA store.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "wms-cmd-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for k, v := range map[string]string{
		"LEGO_DB_PATH":         filepath.Join(dir, "lego.db"),
		"WMS_SETTINGS_FILE":    filepath.Join(dir, "settings.json"),
		"AUDIT_LOG_FILE":       filepath.Join(dir, "audit.log"),
		"TWOFA_FILE":           filepath.Join(dir, "2fa.json"),
		"PARTDB_DB_PATH":       filepath.Join(dir, "partdb.db"),
		"WMS_IMAGE_DIR":        filepath.Join(dir, "img"),
		"WMS_PLUGIN_DIR":       filepath.Join(dir, "plugins"),
		"MODERNWMS_BACKUP_DIR": filepath.Join(dir, "backups"),
		"MODERNWMS_CONTAINER":  "no-such-container",
		"PARTDB_API_URL":       "http://127.0.0.1:1",
	} {
		os.Setenv(k, v)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
