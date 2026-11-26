package dashboardapi

import (
	"encoding/json"
	"net/http"
	"os"
	"testing"
)

// isolateGraceStores points DASHBOARD_GRACE_FILE/LOGIN_LOCK_FILE at fresh
// temp files for the test -- both are package-level, config-driven stores
// (dashboardgrace, loginlock), not fields on *Server, so every test that
// exercises the grace-login/grace-mint handlers needs this or it would
// silently read/write the real deployment's files.
func isolateGraceStores(t *testing.T) {
	t.Helper()
	gf, err := os.CreateTemp(t.TempDir(), "dashboard-grace-*.json")
	if err != nil {
		t.Fatal(err)
	}
	gf.Close()
	t.Setenv("DASHBOARD_GRACE_FILE", gf.Name())

	lf, err := os.CreateTemp(t.TempDir(), "login-lock-*.json")
	if err != nil {
		t.Fatal(err)
	}
	lf.Close()
	t.Setenv("LOGIN_LOCK_FILE", lf.Name())
}

func TestGraceMintReturnsAFreshTokenAndExpiry(t *testing.T) {
	isolateGraceStores(t)
	_, srv := testServer(t, "secret")
	defer srv.Close()

	resp := postJSON(t, srv.URL, "/dashboard/auth/grace-mint", `{"clerk_user_id":"user_abc"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var body graceMintResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Token == "" || body.ExpiresAtUnix == 0 {
		t.Fatalf("expected a token and expiry, got %+v", body)
	}
}

func TestGraceMintRequiresAClerkUserID(t *testing.T) {
	isolateGraceStores(t)
	_, srv := testServer(t, "secret")
	defer srv.Close()

	resp := postJSON(t, srv.URL, "/dashboard/auth/grace-mint", `{"clerk_user_id":""}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// With no WMS_CLERK_SECRET_KEY configured (the test harness's default --
// matches a real deployment that never set one up), grace-login must fail
// closed no matter what's posted, rather than erroring out in some
// distinguishable way. This is the same property Provision/FindUser/
// VerifyTOTP already have in clerkbridge; this test pins it end to end
// through the real HTTP handler, not just the clerkbridge function.
func TestGraceLoginFailsClosedWhenClerkIsNotConfigured(t *testing.T) {
	isolateGraceStores(t)
	_, srv := testServer(t, "secret")
	defer srv.Close()

	resp := postJSON(t, srv.URL, "/dashboard/auth/grace-login", `{"email":"dave@example.com","password":"whatever","grace_token":"sometoken"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["error"] != "invalid credentials" {
		t.Fatalf("expected the generic failure message, got %+v", body)
	}
}

func TestGraceLoginRejectsMissingFieldsWithTheSameGenericFailure(t *testing.T) {
	isolateGraceStores(t)
	_, srv := testServer(t, "secret")
	defer srv.Close()

	for _, body := range []string{
		`{"email":"","password":"x","grace_token":"x"}`,
		`{"email":"dave@example.com","password":"","grace_token":"x"}`,
		`{"email":"dave@example.com","password":"x","grace_token":""}`,
	} {
		resp := postJSON(t, srv.URL, "/dashboard/auth/grace-login", body)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("body=%s: status = %d, want 401", body, resp.StatusCode)
		}
		resp.Body.Close()
	}
}

func TestGraceLoginLocksOutAfterRepeatedFailures(t *testing.T) {
	isolateGraceStores(t)
	_, srv := testServer(t, "secret")
	defer srv.Close()

	// No Clerk secret configured, so every attempt fails closed and counts
	// as a failure -- MaxFailures of those should lock the account out the
	// same as a run of wrong passwords would.
	for i := 0; i < 6; i++ {
		resp := postJSON(t, srv.URL, "/dashboard/auth/grace-login", `{"email":"dave@example.com","password":"x","grace_token":"x"}`)
		resp.Body.Close()
	}
	resp := postJSON(t, srv.URL, "/dashboard/auth/grace-login", `{"email":"dave@example.com","password":"x","grace_token":"x"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (locked)", resp.StatusCode)
	}
}

func TestGraceLoginMalformedBodyIs400(t *testing.T) {
	isolateGraceStores(t)
	_, srv := testServer(t, "secret")
	defer srv.Close()

	resp := postJSON(t, srv.URL, "/dashboard/auth/grace-login", `not json`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}
