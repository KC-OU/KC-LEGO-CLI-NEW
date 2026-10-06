package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

// TestMobileSetupQRSavesAndReusesTheURL is the direct test for "QR-code-
// assisted first-time server setup": no URL saved yet is a usage error
// (nothing to encode), giving one saves it (Settings-style, see
// config.SetOverride), and a later call with no argument reuses what was
// saved instead of asking again.
func TestMobileSetupQRSavesAndReusesTheURL(t *testing.T) {
	t.Setenv(config.SettingsFile, filepath.Join(t.TempDir(), "settings.json"))

	if _, code := run(t, "sys", "mobile-setup-qr"); code != exitUsage {
		t.Errorf("no URL saved yet = %d, want %d", code, exitUsage)
	}

	stdout, code := run(t, "sys", "mobile-setup-qr", "https://wms-mobile.example.com")
	if code != 0 || !strings.Contains(stdout, "https://wms-mobile.example.com") {
		t.Fatalf("mobile-setup-qr <url>: code=%d out=%q", code, stdout)
	}
	// A QR code is block glyphs, not literal text, but it's definitely more
	// than just the one line with the URL on it.
	if lines := strings.Count(stdout, "\n"); lines < 10 {
		t.Errorf("expected a multi-line QR code in the output, got %d line(s):\n%s", lines, stdout)
	}

	stdout, code = run(t, "sys", "mobile-setup-qr")
	if code != 0 || !strings.Contains(stdout, "https://wms-mobile.example.com") {
		t.Fatalf("mobile-setup-qr (no arg, reusing the saved URL): code=%d out=%q", code, stdout)
	}

	// Giving a new one updates what's saved.
	if _, code := run(t, "sys", "mobile-setup-qr", "https://new-address.example.com"); code != 0 {
		t.Fatalf("updating the saved URL: code=%d", code)
	}
	stdout, _ = run(t, "sys", "mobile-setup-qr")
	if strings.Contains(stdout, "wms-mobile.example.com") || !strings.Contains(stdout, "new-address.example.com") {
		t.Errorf("expected the updated URL to replace the old one, got:\n%s", stdout)
	}
}

// TestPDFEditorSavesAndReusesTheURL is pdf-editor's counterpart to
// TestMobileSetupQRSavesAndReusesTheURL — same Settings-style save/reuse
// contract, just a different config key (config.StirlingPDFURL).
func TestPDFEditorSavesAndReusesTheURL(t *testing.T) {
	t.Setenv(config.SettingsFile, filepath.Join(t.TempDir(), "settings.json"))

	if _, code := run(t, "sys", "pdf-editor"); code != exitUsage {
		t.Errorf("no URL saved yet = %d, want %d", code, exitUsage)
	}

	stdout, code := run(t, "sys", "pdf-editor", "https://pdf.example.com/editor")
	if code != 0 || !strings.Contains(stdout, "https://pdf.example.com/editor") {
		t.Fatalf("pdf-editor <url>: code=%d out=%q", code, stdout)
	}
	if lines := strings.Count(stdout, "\n"); lines < 10 {
		t.Errorf("expected a multi-line QR code in the output, got %d line(s):\n%s", lines, stdout)
	}

	stdout, code = run(t, "sys", "pdf-editor")
	if code != 0 || !strings.Contains(stdout, "https://pdf.example.com/editor") {
		t.Fatalf("pdf-editor (no arg, reusing the saved URL): code=%d out=%q", code, stdout)
	}
}
