package main

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

func accessEnv(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(config.AccessFile, filepath.Join(dir, "access.json"))
	t.Setenv(config.AuditLogFile, filepath.Join(dir, "audit.log"))
}

func TestAccessUserBadgeIssueShowRegenerateAndClear(t *testing.T) {
	accessEnv(t)
	var doc struct {
		User       string `json:"user"`
		BadgeToken string `json:"badge_token"`
	}
	decode := func(stdout string) { t.Helper(); json.Unmarshal([]byte(stdout), &doc) }

	stdout, code := run(t, "access", "user", "badge", "alex", "--source", "partdb", "--json")
	decode(stdout)
	if code != 0 || doc.BadgeToken == "" {
		t.Fatalf("issue: code=%d out=%q", code, stdout)
	}
	first := doc.BadgeToken

	stdout, code = run(t, "access", "user", "badge", "alex", "--source", "partdb", "--json")
	decode(stdout)
	if code != 0 || doc.BadgeToken != first {
		t.Fatalf("re-showing should return the same token: %q vs %q (code=%d)", doc.BadgeToken, first, code)
	}

	stdout, code = run(t, "access", "user", "badge", "alex", "--source", "partdb", "--regenerate", "--json")
	decode(stdout)
	if code != 0 || doc.BadgeToken == "" || doc.BadgeToken == first {
		t.Fatalf("--regenerate should issue a new token: %q vs %q (code=%d)", doc.BadgeToken, first, code)
	}
	second := doc.BadgeToken

	p, err := access.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.FindByBadge(first); ok {
		t.Error("the old token must stop working after --regenerate")
	}
	if name, ok := p.FindByBadge(second); !ok || name != "alex" {
		t.Errorf("the new token should resolve to alex: %q %v", name, ok)
	}

	if _, code = run(t, "access", "user", "badge", "alex", "--source", "partdb", "--clear"); code != 0 {
		t.Fatalf("--clear: code=%d", code)
	}
	p, _ = access.Load()
	if p.Users["partdb:alex"].BadgeToken != "" {
		t.Error("--clear should remove the token")
	}
	if _, ok := p.FindByBadge(second); ok {
		t.Error("a cleared token must stop resolving")
	}
}
