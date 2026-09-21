package config

import (
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
