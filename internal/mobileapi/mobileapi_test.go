package mobileapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/audit"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/partdb"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/twofa"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/wmsdb"
)

type fakeWMS struct {
	authResult *wmsdb.UserAuthResult
	authErr    error
}

func (f *fakeWMS) AuthenticateUser(ctx context.Context, usernameOrID, password, passwordMD5 string) (*wmsdb.UserAuthResult, error) {
	if f.authErr != nil {
		return nil, f.authErr
	}
	return f.authResult, nil
}
func (f *fakeWMS) FetchUserPermissions(ctx context.Context, role string) (*wmsdb.Permissions, error) {
	return &wmsdb.Permissions{RoleName: role, Menus: []string{"*"}, CanWrite: true}, nil
}

type fakePartDB struct{}

func (f *fakePartDB) PasswordHash(name string) (string, bool, error) { return "", false, nil }
func (f *fakePartDB) GetUserByName(name string) (*partdb.PartDBUser, error) {
	return nil, nil
}

// testEnv wires a scratch lego.db (so mobile_sessions is a real table) and a
// fake ModernWMS that always accepts "dave"/"secret" — never a real
// container or Part-DB file.
func testEnv(t *testing.T) (*lego.DB, *Server) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(config.TwoFAFile, filepath.Join(dir, "2fa.json"))
	t.Setenv(config.AuditLogFile, filepath.Join(dir, "audit.log"))
	db, err := lego.Open(filepath.Join(dir, "lego.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	wms := &fakeWMS{authResult: &wmsdb.UserAuthResult{Found: true, ID: 1, UserName: "dave", Role: "checker", IsValid: true}}
	s := &Server{authWMS: wms, authPDB: &fakePartDB{}, legoDB: db, audit: audit.New()}
	return db, s
}

func newTestServer(s *Server) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /mobile/login", s.handleLogin)
	mux.HandleFunc("POST /mobile/login/2fa", s.handleLogin2FA)
	mux.HandleFunc("POST /mobile/logout", s.withSession(s.handleLogout))
	mux.HandleFunc("GET /mobile/me", s.withSession(s.handleMe))
	return httptest.NewServer(mux)
}

func post(t *testing.T, srv *httptest.Server, path string, body any) (*http.Response, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(body)
	resp, err := http.Post(srv.URL+path, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp, out
}

func TestLoginWithoutTwoFAIsImmediatelyUsable(t *testing.T) {
	_, s := testEnv(t)
	srv := newTestServer(s)
	defer srv.Close()

	resp, body := post(t, srv, "/mobile/login", map[string]string{"username": "dave", "password": "secret"})
	if resp.StatusCode != http.StatusOK || body["needs_2fa"] != false {
		t.Fatalf("status = %d, body = %v", resp.StatusCode, body)
	}
	token, _ := body["token"].(string)
	if token == "" {
		t.Fatal("expected a token")
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/mobile/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	var me map[string]any
	_ = json.NewDecoder(resp2.Body).Decode(&me)
	if resp2.StatusCode != http.StatusOK || me["username"] != "dave" {
		t.Fatalf("/mobile/me status = %d, body = %v", resp2.StatusCode, me)
	}
}

func TestLoginWithWrongPasswordIs401(t *testing.T) {
	_, s := testEnv(t)
	s.authWMS.(*fakeWMS).authResult = &wmsdb.UserAuthResult{Found: false}
	srv := newTestServer(s)
	defer srv.Close()

	resp, _ := post(t, srv, "/mobile/login", map[string]string{"username": "dave", "password": "wrong"})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestLoginWithTwoFARequiresTheSecondStep(t *testing.T) {
	_, s := testEnv(t)
	srv := newTestServer(s)
	defer srv.Close()

	secret, _, err := twofa.Enroll("dave", "modernwms")
	if err != nil {
		t.Fatal(err)
	}
	code, _ := totp.GenerateCode(secret, time.Now())
	if _, err := twofa.Confirm("dave", "modernwms", code); err != nil {
		t.Fatal(err)
	}

	resp, body := post(t, srv, "/mobile/login", map[string]string{"username": "dave", "password": "secret"})
	if resp.StatusCode != http.StatusOK || body["needs_2fa"] != true {
		t.Fatalf("status = %d, body = %v, want needs_2fa=true", resp.StatusCode, body)
	}
	token, _ := body["token"].(string)

	// The token is not yet a usable session.
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/mobile/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp2, _ := http.DefaultClient.Do(req)
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a pending token must not work on /mobile/me yet, got %d", resp2.StatusCode)
	}

	nextCode, _ := totp.GenerateCode(secret, time.Now().Add(30*time.Second))
	resp3, body3 := post(t, srv, "/mobile/login/2fa", map[string]string{"token": token, "code": nextCode})
	if resp3.StatusCode != http.StatusOK || body3["token"] != token {
		t.Fatalf("2fa status = %d, body = %v", resp3.StatusCode, body3)
	}

	req2, _ := http.NewRequest(http.MethodGet, srv.URL+"/mobile/me", nil)
	req2.Header.Set("Authorization", "Bearer "+token)
	resp4, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp4.Body.Close()
	if resp4.StatusCode != http.StatusOK {
		t.Fatalf("after confirming 2FA, /mobile/me status = %d", resp4.StatusCode)
	}
}

func TestLogoutEndsTheSession(t *testing.T) {
	_, s := testEnv(t)
	srv := newTestServer(s)
	defer srv.Close()

	_, body := post(t, srv, "/mobile/login", map[string]string{"username": "dave", "password": "secret"})
	token, _ := body["token"].(string)

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/mobile/logout", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("logout status = %d", resp.StatusCode)
	}

	req2, _ := http.NewRequest(http.MethodGet, srv.URL+"/mobile/me", nil)
	req2.Header.Set("Authorization", "Bearer "+token)
	resp2, _ := http.DefaultClient.Do(req2)
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a logged-out token must be refused, got %d", resp2.StatusCode)
	}
}

func TestMeWithNoTokenIs401(t *testing.T) {
	_, s := testEnv(t)
	srv := newTestServer(s)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/mobile/me")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}
