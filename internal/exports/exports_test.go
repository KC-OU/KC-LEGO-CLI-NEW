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
