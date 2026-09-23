package exports

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

func TestLinkIsSingleUse(t *testing.T) {
	t.Setenv(config.AccessFile, filepath.Join(t.TempDir(), "access.json"))
	dir := t.TempDir()
	path, err := Save(dir, "kc/../x", "missing", "75192", "json", []byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != dir {
		t.Fatalf("user name escaped the folder: %s", path)
	}
	tok, err := NewLink(dir, path, "kc")
	if err != nil {
		t.Fatal(err)
	}
	if len(tok) != 32 {
		t.Fatalf("token %q", tok)
	}
	file, user, err := Claim(dir, strings.ToLower(tok)) // a scanner may lower-case it
	if err != nil || file != path || user != "kc" {
		t.Fatalf("first claim = %q %q %v", file, user, err)
	}
	if _, _, err := Claim(dir, tok); !errors.Is(err, ErrNoLink) {
		t.Fatalf("second claim should fail, got %v", err)
	}
}

func TestLinkRefusals(t *testing.T) {
	dir := t.TempDir()
	write := func(tok string, l link) {
		b, _ := json.Marshal(l)
		os.MkdirAll(filepath.Join(dir, linkDir), 0o700)
		os.WriteFile(linkPath(dir, tok), b, 0o600)
	}
	os.WriteFile(filepath.Join(dir, "ok.json"), []byte("{}"), 0o600)
	secret := filepath.Join(t.TempDir(), "secret")
	os.WriteFile(secret, []byte("x"), 0o600)
	cases := map[string]link{
		"expired": {File: "ok.json", Expires: time.Now().Add(-time.Minute)},
		"escape":  {File: "../" + filepath.Base(secret), Expires: time.Now().Add(time.Minute)},
		"abs":     {File: secret, Expires: time.Now().Add(time.Minute)},
		"hidden":  {File: ".links", Expires: time.Now().Add(time.Minute)},
	}
	i := 0
	for name, l := range cases {
		tok := string(rune('A'+i)) + "2222222222222222222222222222222"
		i++
		write(tok, l)
		if _, _, err := Claim(dir, tok); !errors.Is(err, ErrNoLink) {
			t.Errorf("%s: want ErrNoLink, got %v", name, err)
		}
	}
	for _, bad := range []string{"", "../../etc/passwd", "ZZ"} {
		if _, _, err := Claim(dir, bad); !errors.Is(err, ErrNoLink) {
			t.Errorf("token %q: want ErrNoLink", bad)
		}
	}
}

// TestShareLinkIsMultiUseAndOutlivesDefaultLinkTTL guards the bug a custom, longer-than-
// default TTL exposed: Cleanup used to sweep .links by file age against the site-wide
// LinkTTL default (15 min), so a share link (or a Discord --discord-expires) set longer
// than that got deleted early, well before its own chosen expiry.
func TestShareLinkIsMultiUseAndOutlivesDefaultLinkTTL(t *testing.T) {
	t.Setenv(config.AccessFile, filepath.Join(t.TempDir(), "access.json"))
	if LinkTTL() != 15*time.Minute {
		t.Fatal("test assumes the 15-minute default")
	}
	dir := t.TempDir()
	path, err := Save(dir, "kc", "collection", "", "html", []byte("<html></html>"))
	if err != nil {
		t.Fatal(err)
	}
	tok, err := NewShareLink(dir, path, "kc", 24*time.Hour) // far longer than the 15-min default
	if err != nil {
		t.Fatal(err)
	}
	// backdate the link record past the default LinkTTL, as if it had sat for 20 minutes —
	// the bug would have swept it here; the fix must not, since its own expiry is 24h out.
	p := linkPath(dir, tok)
	old := time.Now().Add(-20 * time.Minute)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}
	if n := Cleanup(dir); n != 0 {
		t.Fatalf("cleanup removed %d record(s); the share link should have survived", n)
	}
	// multi-use: opened twice, both succeed, neither consumes it
	for i := 0; i < 2; i++ {
		file, user, err := Open(dir, tok)
		if err != nil || file != path || user != "kc" {
			t.Fatalf("open #%d = %q %q %v", i, file, user, err)
		}
	}
	// a single-use link is refused by Open — /share/ can't replay a /dl/ token
	dlTok, err := NewLink(dir, path, "kc")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := Open(dir, dlTok); !errors.Is(err, ErrNoLink) {
		t.Fatalf("a single-use token should be refused by Open, got %v", err)
	}
}

func TestShareLinkExpires(t *testing.T) {
	t.Setenv(config.AccessFile, filepath.Join(t.TempDir(), "access.json"))
	dir := t.TempDir()
	path, err := Save(dir, "kc", "collection", "", "html", []byte("<html></html>"))
	if err != nil {
		t.Fatal(err)
	}
	tok, err := NewShareLink(dir, path, "kc", time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	if _, _, err := Open(dir, tok); !errors.Is(err, ErrNoLink) {
		t.Fatalf("expired share link should be refused, got %v", err)
	}
}

func TestNewShareLinkClampsToMaxLinkTTL(t *testing.T) {
	t.Setenv(config.AccessFile, filepath.Join(t.TempDir(), "access.json"))
	dir := t.TempDir()
	path, err := Save(dir, "kc", "collection", "", "html", []byte("<html></html>"))
	if err != nil {
		t.Fatal(err)
	}
	tok, err := NewShareLink(dir, path, "kc", 365*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(linkPath(dir, tok))
	if err != nil {
		t.Fatal(err)
	}
	var l link
	if json.Unmarshal(b, &l) != nil {
		t.Fatal("bad link record")
	}
	if l.Expires.After(time.Now().Add(MaxLinkTTL + time.Minute)) {
		t.Fatalf("expiry %v was not clamped to MaxLinkTTL", l.Expires)
	}
}

func TestCleanup(t *testing.T) {
	t.Setenv(config.AccessFile, filepath.Join(t.TempDir(), "access.json"))
	dir := t.TempDir()
	old := filepath.Join(dir, "old.json")
	os.WriteFile(old, nil, 0o600)
	os.Chtimes(old, time.Now().Add(-MaxAge()-time.Hour), time.Now().Add(-MaxAge()-time.Hour))
	os.WriteFile(filepath.Join(dir, "new.json"), nil, 0o600)
	if n := Cleanup(dir); n != 1 {
		t.Fatalf("cleaned %d, want 1", n)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("old export kept")
	}
}

func TestTimingsFollowThePolicy(t *testing.T) {
	t.Setenv(config.AccessFile, filepath.Join(t.TempDir(), "access.json"))
	if MaxAge() != 7*24*time.Hour || LinkTTL() != 15*time.Minute {
		t.Fatal("defaults")
	}
	if _, err := access.Update(func(p *access.Policy) error {
		p.Settings.ExportDays, p.Settings.LinkMinutes = access.Int(2), access.Int(5)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if MaxAge() != 48*time.Hour || LinkTTL() != 5*time.Minute {
		t.Errorf("got %v %v", MaxAge(), LinkTTL())
	}
}
