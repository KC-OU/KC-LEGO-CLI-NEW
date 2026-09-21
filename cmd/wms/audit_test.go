package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/audit"
)

func TestVerifyAuditFailsWhenPreChainHistoryIsEdited(t *testing.T) {
	l := &audit.Logger{Path: filepath.Join(t.TempDir(), "audit.log")}
	old := "[2026-08-10 08:01:40] USER:viewonly | ROLE:N/A | ACTION:LOGIN | STATUS:FAILED_INVALID_CREDENTIALS\n"
	if err := os.WriteFile(l.Path, []byte(old), 0600); err != nil {
		t.Fatal(err)
	}
	if err := verifyAudit(l); err != nil {
		t.Fatalf("a log with no chain yet is not a failure: %v", err)
	}
	_ = l.Log("admin", "Admin", "LOGIN", "SUCCESS", "")
	if err := verifyAudit(l); err != nil {
		t.Fatalf("an intact chain must pass: %v", err)
	}
	data, _ := os.ReadFile(l.Path)
	_ = os.WriteFile(l.Path, []byte(strings.Replace(string(data), "viewonly", "vieWonly", 1)), 0600)
	if err := verifyAudit(l); err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("editing an old line must fail verification at the CHAIN_START line, got %v", err)
	}
}
