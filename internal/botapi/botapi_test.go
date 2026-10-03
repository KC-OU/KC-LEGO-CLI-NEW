package botapi

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

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/audit"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/auth"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/twofa"
)

// testEnv wires config at fresh temp paths (access.json, lego.db, audit.log,
// 2fa.json) so this package never touches anything real, then links a
// governed admin ("partdb:bot-admin", the "admin" starter group — users.view
// and everything else) with a known PIN and security question.
func testEnv(t *testing.T) (db *lego.DB, auditLog *audit.Logger, key string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(config.AccessFile, filepath.Join(dir, "access.json"))
	t.Setenv(config.LegoDBPath, filepath.Join(dir, "lego.db"))
	t.Setenv(config.AuditLogFile, filepath.Join(dir, "audit.log"))
	t.Setenv(config.TwoFAFile, filepath.Join(dir, "2fa.json"))

	db, err := lego.Open(filepath.Join(dir, "lego.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	auditLog = audit.New()

	key = access.Key("partdb", "bot-admin")
	pinHash, err := auth.HashPartDB("1234")
	if err != nil {
		t.Fatal(err)
	}
	answerHash, err := auth.HashPartDB("rex")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := access.Update(func(p *access.Policy) error {
		p.Users[key] = &access.User{
			Groups: []string{"admin"}, DiscordID: "111222333",
			BotPINHash: pinHash, SecurityQuestion: "First pet?", SecurityAnswerHash: answerHash,
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return db, auditLog, key
}

func post(t *testing.T, srv *httptest.Server, path string, body any) (*http.Response, map[string]any) {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(srv.URL+path, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp, out
}

// newTestServer wraps the real mux (via a throwaway server struct) in an
// httptest.Server, bypassing Serve's loopback-string check and ctx/shutdown
// plumbing, which httptest.NewServer already provides.
func newTestServer(db *lego.DB, auditLog *audit.Logger) *httptest.Server {
	s := NewServer(db, auditLog)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /bot/action", s.handleAction)
	mux.HandleFunc("POST /bot/reset-pin", s.handleResetPIN)
	return httptest.NewServer(mux)
}

func TestServeRefusesANonLoopbackAddr(t *testing.T) {
	err := Serve(context.TODO(), "0.0.0.0:8080", nil) // s is never read before the refusal
	if err == nil {
		t.Fatal("a non-loopback addr must be refused")
	}
}

func TestActionUnknownIDAndWrongPINCollapseToTheSame401(t *testing.T) {
	db, auditLog, _ := testEnv(t)
	srv := newTestServer(db, auditLog)
	defer srv.Close()

	resp1, _ := post(t, srv, "/bot/action", map[string]any{"platform": "discord", "id": "no-such-id", "pin": "1234", "action": "list_open_tickets"})
	resp2, _ := post(t, srv, "/bot/action", map[string]any{"platform": "discord", "id": "111222333", "pin": "0000", "action": "list_open_tickets"})
	if resp1.StatusCode != http.StatusUnauthorized || resp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unknown id = %d, wrong pin = %d, want both 401", resp1.StatusCode, resp2.StatusCode)
	}
}

func TestActionLocksOutAfterRepeatedBadPINs(t *testing.T) {
	db, auditLog, key := testEnv(t)
	srv := newTestServer(db, auditLog)
	defer srv.Close()

	for i := 0; i < maxPINFailures; i++ {
		resp, _ := post(t, srv, "/bot/action", map[string]any{"platform": "discord", "id": "111222333", "pin": "wrong", "action": "list_open_tickets"})
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d, want 401", i, resp.StatusCode)
		}
	}
	resp, body := post(t, srv, "/bot/action", map[string]any{"platform": "discord", "id": "111222333", "pin": "1234", "action": "list_open_tickets"})
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("after %d failures, even the right PIN should be locked out: status = %d, body = %v", maxPINFailures, resp.StatusCode, body)
	}

	p, _ := access.Load()
	if p.Users[key].BotPINLockUntil == "" {
		t.Error("the lockout should be recorded on the policy, not just in memory")
	}
}

func TestActionRefusesWhenThePermissionIsMissing(t *testing.T) {
	db, auditLog, key := testEnv(t)
	if _, err := access.Update(func(p *access.Policy) error {
		p.Users[key].Groups = nil // no group at all => no permissions
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	srv := newTestServer(db, auditLog)
	defer srv.Close()

	resp, _ := post(t, srv, "/bot/action", map[string]any{"platform": "discord", "id": "111222333", "pin": "1234", "action": "list_open_tickets"})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

func TestActionUnknownActionIs400(t *testing.T) {
	db, auditLog, _ := testEnv(t)
	srv := newTestServer(db, auditLog)
	defer srv.Close()

	resp, _ := post(t, srv, "/bot/action", map[string]any{"platform": "discord", "id": "111222333", "pin": "1234", "action": "nonsense"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestReassignTicketAction(t *testing.T) {
	db, auditLog, _ := testEnv(t)
	srv := newTestServer(db, auditLog)
	defer srv.Close()

	tk, err := db.AssignTicket(lego.TicketCheck, "75192-1", "75192-1 Falcon", "", "", "", "setup")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ClaimTicket(tk.ID, "dave"); err != nil {
		t.Fatal(err)
	}

	resp, body := post(t, srv, "/bot/action", map[string]any{
		"platform": "discord", "id": "111222333", "pin": "1234", "action": "reassign_ticket",
		"params": map[string]any{"ticket_id": tk.ID, "to": "pat"},
	})
	if resp.StatusCode != http.StatusOK || body["ok"] != true {
		t.Fatalf("status = %d, body = %v", resp.StatusCode, body)
	}

	evs, err := db.AdminEvents(10)
	if err != nil || len(evs) == 0 || evs[0].Kind != lego.EventReassigned {
		t.Fatalf("AdminEvents = %+v, %v, want the newest to be reassigned", evs, err)
	}
	msgs, err := db.UndeliveredMessages("dave")
	if err != nil || len(msgs) == 0 {
		t.Fatalf("dave should have been sent the reassurance message: %+v, %v", msgs, err)
	}
}

func TestMessageUserAction(t *testing.T) {
	db, auditLog, _ := testEnv(t)
	srv := newTestServer(db, auditLog)
	defer srv.Close()

	resp, body := post(t, srv, "/bot/action", map[string]any{
		"platform": "discord", "id": "111222333", "pin": "1234", "action": "message_user",
		"params": map[string]any{"username": "dave", "body": "please double check your last set"},
	})
	if resp.StatusCode != http.StatusOK || body["ok"] != true {
		t.Fatalf("status = %d, body = %v", resp.StatusCode, body)
	}
	msgs, err := db.UndeliveredMessages("dave")
	if err != nil || len(msgs) != 1 || msgs[0].Body != "please double check your last set" {
		t.Fatalf("UndeliveredMessages = %+v, %v", msgs, err)
	}
}

func TestKickSessionAction(t *testing.T) {
	db, auditLog, _ := testEnv(t)
	srv := newTestServer(db, auditLog)
	defer srv.Close()

	if err := db.Heartbeat("sess1", "dave", "checker", "telnet", "", "hub"); err != nil {
		t.Fatal(err)
	}

	resp, body := post(t, srv, "/bot/action", map[string]any{
		"platform": "discord", "id": "111222333", "pin": "1234", "action": "kick_session",
		"params": map[string]any{"session_id": "sess1", "message": "logged off remotely"},
	})
	if resp.StatusCode != http.StatusOK || body["ok"] != true {
		t.Fatalf("status = %d, body = %v", resp.StatusCode, body)
	}
	msg, found, err := db.ConsumeForceLogoff("sess1")
	if err != nil || !found || msg != "logged off remotely" {
		t.Fatalf("ConsumeForceLogoff = %q, %v, %v", msg, found, err)
	}
}

func TestListOpenTicketsAction(t *testing.T) {
	db, auditLog, _ := testEnv(t)
	srv := newTestServer(db, auditLog)
	defer srv.Close()

	if _, err := db.AssignTicket(lego.TicketCheck, "75192-1", "75192-1 Falcon", "", "", "", "setup"); err != nil {
		t.Fatal(err)
	}

	resp, body := post(t, srv, "/bot/action", map[string]any{"platform": "discord", "id": "111222333", "pin": "1234", "action": "list_open_tickets"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %v", resp.StatusCode, body)
	}
	tickets, ok := body["tickets"].([]any)
	if !ok || len(tickets) != 1 {
		t.Fatalf("tickets = %v, want 1", body["tickets"])
	}
}

func TestListAlertsAction(t *testing.T) {
	db, auditLog, _ := testEnv(t)
	srv := newTestServer(db, auditLog)
	defer srv.Close()

	if _, _, err := db.RecordCheckOutcome("dave", lego.AccuracyChecker, lego.TicketCheck, "75192-1", 500, 25); err != nil {
		t.Fatal(err)
	}

	resp, body := post(t, srv, "/bot/action", map[string]any{"platform": "discord", "id": "111222333", "pin": "1234", "action": "list_alerts"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %v", resp.StatusCode, body)
	}
	alerts, ok := body["alerts"].([]any)
	if !ok || len(alerts) != 1 {
		t.Fatalf("alerts = %v, want 1", body["alerts"])
	}
}

func TestResetPINRequiresBothFactors(t *testing.T) {
	db, auditLog, key := testEnv(t)
	srv := newTestServer(db, auditLog)
	defer srv.Close()

	secret, _, err := twofa.Enroll("bot-admin", "partdb")
	if err != nil {
		t.Fatal(err)
	}
	firstCode, _ := totp.GenerateCode(secret, time.Now())
	if _, err := twofa.Confirm("bot-admin", "partdb", firstCode); err != nil {
		t.Fatal(err)
	}

	resp, _ := post(t, srv, "/bot/reset-pin", map[string]any{
		"platform": "discord", "id": "111222333", "code": "000000", "answer": "rex", "new_pin": "9999",
	})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a bogus code with the right answer must be refused, got %d", resp.StatusCode)
	}

	resp, _ = post(t, srv, "/bot/reset-pin", map[string]any{
		"platform": "discord", "id": "111222333", "code": firstCode, "answer": "wrong", "new_pin": "9999",
	})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("the right code with a wrong answer must be refused, got %d", resp.StatusCode)
	}

	validCode, _ := totp.GenerateCode(secret, time.Now().Add(30*time.Second))
	resp, body := post(t, srv, "/bot/reset-pin", map[string]any{
		"platform": "discord", "id": "111222333", "code": validCode, "answer": "Rex", "new_pin": "9999",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("both factors correct should succeed, got %d, body %v", resp.StatusCode, body)
	}
	p, _ := access.Load()
	if !auth.VerifyPartDB("9999", p.Users[key].BotPINHash) {
		t.Error("the PIN should now be 9999")
	}
}
