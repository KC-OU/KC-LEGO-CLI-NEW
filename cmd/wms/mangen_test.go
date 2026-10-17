package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestManGenWritesRealTroffPages is a smoke test for `wms man-gen`: it must
// actually run cobra/doc.GenManTree against the real command tree (not a
// stub) and leave valid-looking troff behind, since this is what a
// goreleaser release and `man wms` ultimately depend on.
func TestManGenWritesRealTroffPages(t *testing.T) {
	dir := t.TempDir()
	root := newRootCmd()
	cmd := newManGenCmd(root)
	cmd.SetArgs(nil)
	if err := cmd.RunE(cmd, []string{dir}); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) < 2 {
		t.Fatalf("expected at least a top-level wms.1 plus subcommand pages, got %d entries", len(entries))
	}

	top := filepath.Join(dir, "wms.1")
	b, err := os.ReadFile(top)
	if err != nil {
		t.Fatalf("wms.1 missing: %v", err)
	}
	page := string(b)
	if !strings.Contains(page, `.TH "WMS" "1"`) {
		t.Errorf("doesn't look like a troff man page: %q", page[:min(200, len(page))])
	}
	if !strings.Contains(page, ".SH NAME") || !strings.Contains(page, ".SH SYNOPSIS") {
		t.Errorf("missing standard man sections: %q", page)
	}
}
