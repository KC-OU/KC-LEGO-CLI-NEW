package mobileapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/wmsdb"
)

func loginToken(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	_, body := post(t, srv, "/mobile/login", map[string]string{"username": "dave", "password": "secret"})
	token, _ := body["token"].(string)
	if token == "" {
		t.Fatalf("expected a token, got %v", body)
	}
	return token
}

func authedPost(t *testing.T, srv *httptest.Server, path, token string, body any) (*http.Response, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+path, bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp, out
}

func TestChangePasswordRequiresASession(t *testing.T) {
	_, s := testEnv(t)
	srv := newTestServer(s)
	defer srv.Close()

	b, _ := json.Marshal(map[string]string{"old_password": "secret", "new_password": "newpass1"})
	resp, err := http.Post(srv.URL+"/mobile/change-password", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestChangePasswordRequiresBothFields(t *testing.T) {
	_, s := testEnv(t)
	srv := newTestServer(s)
	defer srv.Close()
	token := loginToken(t, srv)

	resp, body := authedPost(t, srv, "/mobile/change-password", token, map[string]string{"old_password": "secret"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %v, want 400", resp.StatusCode, body)
	}
}

func TestChangePasswordRejectsAWrongOldPassword(t *testing.T) {
	_, s := testEnv(t)
	srv := newTestServer(s)
	defer srv.Close()
	token := loginToken(t, srv)

	// Same trick throttle_test.go uses: flip the fake to "not found" to
	// simulate the old-password re-check (a second AuthenticateUser call,
	// same as login's) now failing, independent of the already-issued session.
	s.authWMS.(*fakeWMS).authResult = &wmsdb.UserAuthResult{Found: false}

	resp, body := authedPost(t, srv, "/mobile/change-password", token, map[string]string{"old_password": "wrong", "new_password": "newpass1"})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %v, want 401", resp.StatusCode, body)
	}
}

func TestChangePasswordWithoutAWiredUserServiceIs500(t *testing.T) {
	// testEnv's Server literal leaves s.users nil (no live ModernWMS/Part-DB
	// in these tests, same reasoning as the throttle nil-check in
	// handleLogin) — the old-password check still runs for real, but the
	// actual reset can't happen without a wired users.Service, and must fail
	// clearly rather than silently doing nothing or panicking.
	_, s := testEnv(t)
	srv := newTestServer(s)
	defer srv.Close()
	token := loginToken(t, srv)

	resp, body := authedPost(t, srv, "/mobile/change-password", token, map[string]string{"old_password": "secret", "new_password": "newpass1"})
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %v, want 500", resp.StatusCode, body)
	}
}

func TestMeReportsMustChangePasswordFalseWhenBackendsAreNotWiredIn(t *testing.T) {
	// s.wms/s.pdb are nil in these tests — /mobile/me must still answer,
	// defaulting to "no change needed" rather than panicking on a nil backend.
	_, s := testEnv(t)
	srv := newTestServer(s)
	defer srv.Close()
	token := loginToken(t, srv)

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/mobile/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if resp.StatusCode != http.StatusOK || body["must_change_password"] != false {
		t.Fatalf("status = %d, body = %v", resp.StatusCode, body)
	}
}
