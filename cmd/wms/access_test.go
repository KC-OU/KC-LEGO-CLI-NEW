package main

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
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

// TestAccessUserSetPasswordCreatesALocalEntry covers the solo-mode CLI path
// (see docs/guides/solo-mode.md): `wms access user set-password --source
// local` must create (or update) a "local:<name>" policy entry with a
// bcrypt password hash, printed once, usable by auth.AuthenticateLocal.
func TestAccessUserSetPasswordCreatesALocalEntry(t *testing.T) {
	accessEnv(t)
	var doc struct {
		User        string `json:"user"`
		PasswordSet bool   `json:"password_set"`
	}

	stdout, code := run(t, "access", "user", "set-password", "kc", "--source", "local", "--password", "a-chosen-phrase", "--json")
	if code != 0 {
		t.Fatalf("set-password: code=%d out=%q", code, stdout)
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("decoding %q: %v", stdout, err)
	}
	if doc.User != "local:kc" || !doc.PasswordSet {
		t.Fatalf("doc = %+v, want user=local:kc password_set=true", doc)
	}

	p, err := access.Load()
	if err != nil {
		t.Fatal(err)
	}
	u := p.Users["local:kc"]
	if u == nil || u.PasswordHash == "" {
		t.Fatalf("expected a local:kc entry with a password hash, got %+v", u)
	}
}

// TestAccessUserSetNameSetsFullName covers the kanban header's
// "7354 - Kieran Collins (100%)" display name, and confirms a blank name
// clears it back to showing just the ID.
func TestAccessUserSetNameSetsFullName(t *testing.T) {
	accessEnv(t)
	var doc struct {
		User     string `json:"user"`
		FullName string `json:"full_name"`
	}

	stdout, code := run(t, "access", "user", "set-name", "7354", "Kieran Collins", "--source", "local", "--json")
	if code != 0 {
		t.Fatalf("set-name: code=%d out=%q", code, stdout)
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("decoding %q: %v", stdout, err)
	}
	if doc.User != "local:7354" || doc.FullName != "Kieran Collins" {
		t.Fatalf("doc = %+v, want user=local:7354 full_name=\"Kieran Collins\"", doc)
	}

	p, err := access.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Users["local:7354"].FullName; got != "Kieran Collins" {
		t.Fatalf("FullName = %q, want \"Kieran Collins\"", got)
	}

	if _, code = run(t, "access", "user", "set-name", "7354", "", "--source", "local"); code != 0 {
		t.Fatalf("clearing the name: code=%d", code)
	}
	p, _ = access.Load()
	if got := p.Users["local:7354"].FullName; got != "" {
		t.Errorf("FullName after clearing = %q, want empty", got)
	}
}

func renameEnv(t *testing.T) {
	t.Helper()
	accessEnv(t)
	dir := t.TempDir()
	t.Setenv("LEGO_DB_PATH", filepath.Join(dir, "lego.db"))
	t.Setenv("TWOFA_FILE", filepath.Join(dir, "2fa.json"))
	t.Setenv(config.LoginLockFile, filepath.Join(dir, "login_lock.json"))
}

// TestAccessUserRenameMovesPolicyEntry covers an admin renaming a numeric
// Picker/Checker login ID: the access.Policy entry moves to the new key,
// the old key is gone, and the account's ticket history (internal/lego)
// follows it.
func TestAccessUserRenameMovesPolicyEntry(t *testing.T) {
	renameEnv(t)
	if _, code := run(t, "access", "user", "set-name", "7354", "Kieran Collins", "--source", "local"); code != 0 {
		t.Fatalf("seeding the account: code=%d", code)
	}

	db, err := openLego()
	if err != nil {
		t.Fatal(err)
	}
	tk, err := db.AssignTicket(lego.TicketCheck, "75192-1", "Falcon check", "7354", "", "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ClaimTicket(tk.ID, "7354"); err != nil {
		t.Fatal(err)
	}
	db.Close()

	stdout, code := run(t, "access", "user", "rename", "7354", "7421", "--json")
	if code != 0 {
		t.Fatalf("rename: code=%d out=%q", code, stdout)
	}

	p, err := access.Load()
	if err != nil {
		t.Fatal(err)
	}
	if p.Users["local:7354"] != nil {
		t.Error("the old key should be gone after a rename")
	}
	u := p.Users["local:7421"]
	if u == nil || u.FullName != "Kieran Collins" {
		t.Fatalf("local:7421 = %+v, want the moved entry with its FullName intact", u)
	}

	db, err = openLego()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if got, err := db.CurrentTicket("7421"); err != nil || got == nil || got.ID != tk.ID {
		t.Fatalf("CurrentTicket(7421) = %v, %v, want ticket %d", got, err, tk.ID)
	}
	if got, err := db.CurrentTicket("7354"); err != nil || got != nil {
		t.Fatalf("CurrentTicket(7354) after rename = %v, %v, want nil", got, err)
	}
}

// TestAccessUserRenameRejectsNonNumericAndCollision covers the two refusal
// cases: a new ID that wouldn't work on the MC9090's numeric keypad, and a
// new ID that's already someone else's.
func TestAccessUserRenameRejectsNonNumericAndCollision(t *testing.T) {
	renameEnv(t)
	if _, code := run(t, "access", "user", "set-name", "7354", "Kieran Collins", "--source", "local"); code != 0 {
		t.Fatalf("seeding 7354: code=%d", code)
	}
	if _, code := run(t, "access", "user", "set-name", "7421", "Someone Else", "--source", "local"); code != 0 {
		t.Fatalf("seeding 7421: code=%d", code)
	}

	if _, code := run(t, "access", "user", "rename", "7354", "not-numeric"); code == 0 {
		t.Error("a non-numeric new ID should be refused")
	}
	if _, code := run(t, "access", "user", "rename", "7354", "7421"); code == 0 {
		t.Error("renaming onto an ID that already has an entry should be refused, not clobber it")
	}

	p, err := access.Load()
	if err != nil {
		t.Fatal(err)
	}
	if p.Users["local:7354"] == nil || p.Users["local:7421"] == nil {
		t.Error("a rejected rename must not have moved or deleted either entry")
	}
}
