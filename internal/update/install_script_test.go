package update

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runInstaller(t *testing.T, f *fakeRelease, env ...string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command("sh", "../../scripts/install.sh")
	cmd.Env = append([]string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "INSTALL_DIR=" + dir,
		"WMS_INSTALL_API=" + f.URL, "WMS_INSTALL_DOWNLOAD=" + f.URL + "/dl"}, env...)
	out, err := cmd.CombinedOutput()
	return string(out) + "\ndir=" + dir, err
}

func TestInstallScriptVerifiesTheChecksumBeforeInstalling(t *testing.T) {
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl is not available")
	}
	f := newRelease(t, "v1.2.0", []byte("#!/bin/sh\necho installed-ok\n"))
	out, err := runInstaller(t, f)
	if err != nil {
		t.Fatalf("install failed: %v\n%s", err, out)
	}
	dir := out[strings.LastIndex(out, "dir=")+4:]
	if b, _ := os.ReadFile(filepath.Join(dir, "wms-go")); !strings.Contains(string(b), "installed-ok") {
		t.Errorf("binary not installed:\n%s", out)
	}
	if !strings.Contains(out, "Checksum verified") || !strings.Contains(out, "(v1.2.0)") {
		t.Errorf("output:\n%s", out)
	}

	// a tampered download must not be installed
	f.set(func(f *fakeRelease) { f.archive = append(f.archive, 'x') })
	out, err = runInstaller(t, f)
	dir = out[strings.LastIndex(out, "dir=")+4:]
	if err == nil || !strings.Contains(out, "checksum mismatch") {
		t.Errorf("a tampered archive must fail: %v\n%s", err, out)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "wms-go")); statErr == nil {
		t.Error("nothing may be installed after a checksum mismatch")
	}
	// a binary that does not run is not installed either
	bad := newRelease(t, "v1.2.0", []byte("#!/bin/sh\nexit 3\n"))
	out, err = runInstaller(t, bad)
	dir = out[strings.LastIndex(out, "dir=")+4:]
	if err == nil || !strings.Contains(out, "does not run") {
		t.Errorf("a broken binary must fail: %v\n%s", err, out)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "wms-go")); statErr == nil {
		t.Error("a broken binary must not be installed")
	}
	// no release yet
	f.set(func(f *fakeRelease) { f.status = 404 })
	if out, err := runInstaller(t, f); err == nil || !strings.Contains(out, "no release has been published") {
		t.Errorf("no release: %v\n%s", err, out)
	}
	// a hostile tag is refused
	f.set(func(f *fakeRelease) { f.status = 0 })
	f.set(func(f *fakeRelease) { f.tag = "v1.0.0; rm -rf /" })
	if out, err := runInstaller(t, f); err == nil {
		t.Errorf("an odd tag must be refused: %s", out)
	}
	// a pinned version
	f2 := newRelease(t, "v0.9.0", []byte("#!/bin/sh\necho pinned\n"))
	if out, err := runInstaller(t, f2, "VERSION=v0.9.0"); err != nil {
		t.Errorf("pinned version: %v\n%s", err, out)
	}
}

func TestInstallScriptSyntax(t *testing.T) {
	if out, err := exec.Command("sh", "-n", "../../scripts/install.sh").CombinedOutput(); err != nil {
		t.Fatalf("sh -n: %v\n%s", err, out)
	}
	if path, err := exec.LookPath("shellcheck"); err == nil {
		if out, err := exec.Command(path, "../../scripts/install.sh").CombinedOutput(); err != nil {
			t.Errorf("shellcheck:\n%s", out)
		}
	}
}
