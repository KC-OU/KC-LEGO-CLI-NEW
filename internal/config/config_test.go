package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSetOverrideRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	t.Setenv(SettingsFile, path)

	if got := Get(RebrickableAPIKey); got != "" {
		t.Fatalf("expected no key before SetOverride, got %q", got)
	}

	if err := SetOverride(RebrickableAPIKey, "abc123"); err != nil {
		t.Fatalf("SetOverride: %v", err)
	}
	if got := Get(RebrickableAPIKey); got != "abc123" {
		t.Fatalf("Get after SetOverride: got %q, want %q", got, "abc123")
	}

	// A second key must not clobber the first.
	if err := SetOverride(SyncAdminUser, "newadmin"); err != nil {
		t.Fatalf("SetOverride: %v", err)
	}
	if got := Get(RebrickableAPIKey); got != "abc123" {
		t.Fatalf("first override lost after a second SetOverride: got %q", got)
	}
	if got := Get(SyncAdminUser); got != "newadmin" {
		t.Fatalf("Get after second SetOverride: got %q, want %q", got, "newadmin")
	}
}

func TestGetFallsBackWithoutOverrideFile(t *testing.T) {
	t.Setenv(SettingsFile, filepath.Join(t.TempDir(), "does-not-exist.json"))
	if got := Get(SyncPort); got != Defaults()[SyncPort] {
		t.Fatalf("Get with no override file: got %q, want default %q", got, Defaults()[SyncPort])
	}
}

func TestReadEnvFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.env")
	if err := os.WriteFile(path, []byte("# a comment\n\nLEGO_DB_PATH=/x/lego.db\nWMS_SETTINGS_FILE=/x/settings.json\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadEnvFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"LEGO_DB_PATH=/x/lego.db", "WMS_SETTINGS_FILE=/x/settings.json"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("ReadEnvFile = %v, want %v (blank lines and comments skipped)", got, want)
	}
}

func TestReadEnvFileRejectsAMalformedLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.env")
	if err := os.WriteFile(path, []byte("not a valid line\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadEnvFile(path); err == nil {
		t.Error("a line with no '=' should be an error, not silently dropped or half-parsed")
	}
}

func TestReadEnvFileMissingFile(t *testing.T) {
	if _, err := ReadEnvFile(filepath.Join(t.TempDir(), "nonexistent.env")); err == nil {
		t.Error("expected an error for a missing file")
	}
}
