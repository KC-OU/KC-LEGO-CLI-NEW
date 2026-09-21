package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func fakeGitHub(t *testing.T, tag string, status int) {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	bin := []byte("#!/bin/sh\necho updated\n")
	tw.WriteHeader(&tar.Header{Name: "wms", Mode: 0o755, Size: int64(len(bin)), Typeflag: tar.TypeReg})
	tw.Write(bin)
	tw.Close()
	zw.Close()
	name := fmt.Sprintf("wms-go_%s_%s_%s.tar.gz", strings.TrimPrefix(tag, "v"), runtime.GOOS, runtime.GOARCH)
	sum := sha256.Sum256(buf.Bytes())
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/KC-OU/KC-LEGO-CLI-NEW/releases/latest":
			if status != 0 {
				w.WriteHeader(status)
				return
			}
			fmt.Fprintf(w, `{"tag_name":%q,"assets":[{"name":%q,"browser_download_url":"%s/dl/a"},{"name":"checksums.txt","browser_download_url":"%s/dl/s"}]}`, tag, name, srv.URL, srv.URL)
		case "/dl/a":
			w.Write(buf.Bytes())
		case "/dl/s":
			fmt.Fprintf(w, "%s  %s\n", hex.EncodeToString(sum[:]), name)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("WMS_UPDATE_URL", srv.URL)
}

func TestVersionCommand(t *testing.T) {
	stdout, code := run(t, "version", "--json")
	if code != 0 || !strings.Contains(stdout, `"version": "dev"`) || !strings.Contains(stdout, runtime.GOOS) {
		t.Errorf("code=%d %q", code, stdout)
	}
	if stdout, _ := run(t, "version", "--quiet"); strings.TrimSpace(stdout) != "dev" {
		t.Errorf("--quiet prints just the version: %q", stdout)
	}
}

func TestUpdateCheckApplyAndRefusals(t *testing.T) {
	fakeGitHub(t, "v1.4.0", 404)
	if stdout, code := run(t, "update"); code != 0 || !strings.Contains(stdout, "No release has been published yet") {
		t.Errorf("no release: code=%d %q", code, stdout)
	}
	fakeGitHub(t, "v1.4.0", 500)
	if _, code := run(t, "update"); code != exitNetwork {
		t.Errorf("GitHub down = %d", code)
	}

	fakeGitHub(t, "v1.4.0", 0)
	stdout, code := run(t, "update")
	if code != 0 || !strings.Contains(stdout, "v1.4.0") || !strings.Contains(stdout, "wms update --apply") {
		t.Fatalf("check: code=%d %q", code, stdout)
	}
	exe := filepath.Join(t.TempDir(), "wms")
	os.WriteFile(exe, []byte("OLD"), 0o755)
	old := updateTarget
	updateTarget = exe
	defer func() { updateTarget = old }()

	if _, code := run(t, "update", "--apply"); code != exitUsage {
		t.Errorf("--apply needs --yes when there is no terminal: %d", code)
	}
	if b, _ := os.ReadFile(exe); string(b) != "OLD" {
		t.Fatal("nothing may change without confirmation")
	}
	stdout, code = run(t, "update", "--apply", "--yes")
	if code != 0 || !strings.Contains(stdout, "Installed v1.4.0") || !strings.Contains(stdout, "Not restarted") {
		t.Fatalf("apply: code=%d %q", code, stdout)
	}
	if b, _ := os.ReadFile(exe); !strings.Contains(string(b), "echo updated") {
		t.Errorf("binary not replaced: %q", b)
	}
	if b, _ := os.ReadFile(exe + ".old"); string(b) != "OLD" {
		t.Errorf("old binary kept: %q", b)
	}
}
