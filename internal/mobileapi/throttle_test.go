package mobileapi

import (
	"net/http"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/gateway"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/wmsdb"
)

// TestRepeatedFailedLoginsLockOutTheAddress mirrors the telnet gateway's own
// TestGatewayBlocksAnAddressWhoseSessionsKeepFailingSignIn — the same
// gateway.Throttle, just reached through /mobile/login instead of a telnet
// connection. KeepLoopback is needed because httptest dials from 127.0.0.1,
// which Throttle otherwise exempts (true for a real deployment, not useful
// for exercising the behavior here).
func TestRepeatedFailedLoginsLockOutTheAddress(t *testing.T) {
	_, s := testEnv(t)
	s.authWMS.(*fakeWMS).authResult = &wmsdb.UserAuthResult{Found: false}
	s.throttle = gateway.NewThrottle()
	s.throttle.KeepLoopback = true
	strikes := s.throttle.Strikes
	srv := newTestServer(s)
	defer srv.Close()

	var last *http.Response
	for i := 0; i < strikes; i++ {
		last, _ = post(t, srv, "/mobile/login", map[string]string{"username": "dave", "password": "wrong"})
		if last.StatusCode != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d, want 401 (not yet blocked)", i, last.StatusCode)
		}
	}

	resp, body := post(t, srv, "/mobile/login", map[string]string{"username": "dave", "password": "wrong"})
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("after %d failures: status = %d, want 429", strikes+1, resp.StatusCode)
	}
	if body["error"] == "" || body["error"] == nil {
		t.Error("a 429 should still explain why")
	}

	// The right password doesn't bypass an active block — the address is
	// blocked, not the credentials.
	s.authWMS.(*fakeWMS).authResult = &wmsdb.UserAuthResult{Found: true, ID: 1, UserName: "dave", Role: "checker", IsValid: true}
	resp2, _ := post(t, srv, "/mobile/login", map[string]string{"username": "dave", "password": "secret"})
	if resp2.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("a correct password during a block: status = %d, want 429", resp2.StatusCode)
	}
}

// TestSuccessfulLoginDoesNotStrike confirms the common path (right password
// first try) never touches the throttle's strike count.
func TestSuccessfulLoginDoesNotStrike(t *testing.T) {
	_, s := testEnv(t)
	s.throttle = gateway.NewThrottle()
	s.throttle.KeepLoopback = true
	srv := newTestServer(s)
	defer srv.Close()

	resp, _ := post(t, srv, "/mobile/login", map[string]string{"username": "dave", "password": "secret"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}
