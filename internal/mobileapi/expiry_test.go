package mobileapi

import (
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

// TestExpiredAccountCannotGetAMobileSession is the direct regression test
// for a real gap found this session: the TUI (continueSignOn,
// internal/uiapp/login.go) already refuses an expired account, but
// /mobile/login went through auth.AuthenticateUser alone and never
// consulted the access policy at all — the exact same account could sign
// into the phone after its access had ended.
func TestExpiredAccountCannotGetAMobileSession(t *testing.T) {
	_, s := testEnv(t)
	dir := t.TempDir()
	t.Setenv(config.AccessFile, filepath.Join(dir, "access.json"))
	if _, err := access.Update(func(p *access.Policy) error {
		p.Users["modernwms:dave"] = &access.User{Groups: []string{"checker"}, Expires: "2020-01-01"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	srv := newTestServer(s)
	defer srv.Close()

	resp, body := post(t, srv, "/mobile/login", map[string]string{"username": "dave", "password": "secret"})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for an expired account, body = %+v", resp.StatusCode, body)
	}
	if body["error"] == "" || body["error"] == "invalid username or password" {
		t.Errorf("error = %q, want a distinct expired-access message, not the generic bad-credentials one", body["error"])
	}
}

func TestNonExpiredAccountLogsInNormally(t *testing.T) {
	_, s := testEnv(t)
	dir := t.TempDir()
	t.Setenv(config.AccessFile, filepath.Join(dir, "access.json"))
	future := time.Now().AddDate(1, 0, 0).Format("2006-01-02")
	if _, err := access.Update(func(p *access.Policy) error {
		p.Users["modernwms:dave"] = &access.User{Groups: []string{"checker"}, Expires: future}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	srv := newTestServer(s)
	defer srv.Close()

	resp, body := post(t, srv, "/mobile/login", map[string]string{"username": "dave", "password": "secret"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 for an account that expires in the future, body = %+v", resp.StatusCode, body)
	}
}

// TestNoAccessPolicyFileDoesNotBlockLogin is a fresh install (no access.json
// yet): access.Load() seeds an empty policy rather than erroring, so the
// account simply isn't in p.Users and Expired() on a nil *User is false --
// login proceeds, same as it always did before this check existed.
func TestNoAccessPolicyFileDoesNotBlockLogin(t *testing.T) {
	_, s := testEnv(t)
	dir := t.TempDir()
	t.Setenv(config.AccessFile, filepath.Join(dir, "does-not-exist.json"))
	srv := newTestServer(s)
	defer srv.Close()

	resp, body := post(t, srv, "/mobile/login", map[string]string{"username": "dave", "password": "secret"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 with no access policy file yet, body = %+v", resp.StatusCode, body)
	}
}
