package authbridge

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

func withConfig(t *testing.T, base, token string) {
	t.Helper()
	t.Setenv(config.AuthServiceURL, base)
	t.Setenv(config.DashboardProvisionToken, token)
}

func TestConfiguredIsFalseUntilBothAreSet(t *testing.T) {
	withConfig(t, "", "")
	if Configured() {
		t.Fatal("expected Configured() to be false with nothing set")
	}
	withConfig(t, "http://127.0.0.1:1", "")
	if Configured() {
		t.Fatal("expected Configured() to be false with only the URL set")
	}
	withConfig(t, "http://127.0.0.1:1", "tok")
	if !Configured() {
		t.Fatal("expected Configured() to be true with both set")
	}
}

func TestProvisionRefusesWhenNotConfigured(t *testing.T) {
	withConfig(t, "", "")
	if _, err := Provision("12345", "", "pin"); err == nil {
		t.Fatal("expected an error when unconfigured")
	}
}

func TestProvisionSendsTheExpectedRequestAndParsesSuccess(t *testing.T) {
	var gotPath, gotToken string
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotToken = r.Header.Get("X-Provision-Token")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{"provisioned": true, "userId": "u_123"})
	}))
	defer srv.Close()
	withConfig(t, srv.URL, "shh")

	res, err := Provision("12345", "test@example.com", "a-temp-pin")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/internal/provision-dashboard-user" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotToken != "shh" {
		t.Fatalf("token = %q", gotToken)
	}
	if gotBody["username"] != "12345" || gotBody["password"] != "a-temp-pin" {
		t.Fatalf("body = %v", gotBody)
	}
	if !res.Provisioned || res.UserID != "u_123" {
		t.Fatalf("result = %+v", res)
	}
}

func TestProvisionReportsAlreadyExistsWithoutAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"provisioned": false, "reason": "a dashboard account for this username already exists"})
	}))
	defer srv.Close()
	withConfig(t, srv.URL, "shh")

	res, err := Provision("12345", "", "a-temp-pin")
	if err != nil {
		t.Fatal(err)
	}
	if res.Provisioned || res.Reason == "" {
		t.Fatalf("result = %+v", res)
	}
}

func TestProvisionSurfacesAnErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "unauthorized"})
	}))
	defer srv.Close()
	withConfig(t, srv.URL, "wrong")

	if _, err := Provision("12345", "", "a-temp-pin"); err == nil {
		t.Fatal("expected an error")
	}
}
