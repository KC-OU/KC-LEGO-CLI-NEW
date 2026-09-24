package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExpandAliasRewritesTheFirstWordOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "aliases")
	os.WriteFile(path, []byte("# a comment\nr = lego report missing\n\nbad-line-no-equals\n"), 0o600)
	t.Setenv("WMS_ALIASES_FILE", path)

	got := expandAlias([]string{"r", "75192", "-o", "out.html"})
	want := []string{"lego", "report", "missing", "75192", "-o", "out.html"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestExpandAliasLeavesUnknownWordsAlone(t *testing.T) {
	t.Setenv("WMS_ALIASES_FILE", filepath.Join(t.TempDir(), "does-not-exist"))
	args := []string{"lego", "report", "missing"}
	got := expandAlias(args)
	if strings.Join(got, " ") != strings.Join(args, " ") {
		t.Errorf("got %v, want unchanged %v", got, args)
	}
	if len(expandAlias(nil)) != 0 {
		t.Error("expandAlias(nil) should return nil/empty, not panic")
	}
}
