package botapi

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

// signedDiscordRequest builds an httptest request carrying body, signed the
// way Discord actually signs an Interactions Endpoint call (Ed25519 over
// timestamp+body) — the only way to exercise InteractionsHandler's signature
// check without a real Discord application.
func signedDiscordRequest(t *testing.T, url string, pub ed25519.PublicKey, priv ed25519.PrivateKey, body []byte) *http.Request {
	t.Helper()
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := ed25519.Sign(priv, append([]byte(ts), body...))
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Signature-Timestamp", ts)
	req.Header.Set("X-Signature-Ed25519", hex.EncodeToString(sig))
	return req
}

func newDiscordTestServer(t *testing.T) (*httptest.Server, string, ed25519.PublicKey, ed25519.PrivateKey, *lego.DB) {
	t.Helper()
	db, auditLog, _ := testEnv(t)
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	s := NewServer(db, auditLog)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /discord/interactions", InteractionsHandler(s, hex.EncodeToString(pub)))
	return httptest.NewServer(mux), hex.EncodeToString(pub), pub, priv, db
}

func TestInteractionsHandlerRejectsBadSignature(t *testing.T) {
	srv, _, pub, _, _ := newDiscordTestServer(t)
	defer srv.Close()
	_, wrongPriv, _ := ed25519.GenerateKey(nil) // signed with the wrong key entirely

	body := []byte(`{"type":1}`)
	req := signedDiscordRequest(t, srv.URL+"/discord/interactions", pub, wrongPriv, body)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestInteractionsHandlerPing(t *testing.T) {
	srv, _, pub, priv, _ := newDiscordTestServer(t)
	defer srv.Close()

	body := []byte(`{"type":1}`)
	req := signedDiscordRequest(t, srv.URL+"/discord/interactions", pub, priv, body)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || out["type"] != float64(discordTypePong) {
		t.Fatalf("status = %d, body = %v, want 200 {type:1}", resp.StatusCode, out)
	}
}

// discordCommand builds the minimal interaction payload shape InteractionsHandler
// reads: a single "wms" command with exactly one subcommand holding its options.
func discordCommand(userID, sub string, opts map[string]any) []byte {
	options := make([]map[string]any, 0, len(opts))
	for name, v := range opts {
		options = append(options, map[string]any{"name": name, "value": v})
	}
	payload := map[string]any{
		"type":   discordTypeApplicationCommand,
		"member": map[string]any{"user": map[string]any{"id": userID}},
		"data": map[string]any{
			"name": "wms",
			"options": []map[string]any{{
				"name":    sub,
				"options": options,
			}},
		},
	}
	b, _ := json.Marshal(payload)
	return b
}

func TestInteractionsHandlerDispatchesTicketsSubcommand(t *testing.T) {
	srv, _, pub, priv, _ := newDiscordTestServer(t)
	defer srv.Close()

	body := discordCommand("111222333", "tickets", map[string]any{"pin": "1234"})
	req := signedDiscordRequest(t, srv.URL+"/discord/interactions", pub, priv, body)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %v", resp.StatusCode, out)
	}
	data, _ := out["data"].(map[string]any)
	content, _ := data["content"].(string)
	if !strings.HasPrefix(content, "✅") {
		t.Errorf("content = %q, want a success (✅) reply for a correct PIN", content)
	}
	if data["flags"] != float64(64) {
		t.Errorf("flags = %v, want 64 (ephemeral)", data["flags"])
	}
}

func TestInteractionsHandlerWrongPINGivesEphemeralError(t *testing.T) {
	srv, _, pub, priv, _ := newDiscordTestServer(t)
	defer srv.Close()

	body := discordCommand("111222333", "tickets", map[string]any{"pin": "0000"})
	req := signedDiscordRequest(t, srv.URL+"/discord/interactions", pub, priv, body)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	// Discord's outer envelope is always 200 — the failure shows up as an
	// ephemeral error message, not an HTTP error, same as Discord's own API
	// expects for interaction responses.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (errors are ephemeral replies, not HTTP errors)", resp.StatusCode)
	}
	data, _ := out["data"].(map[string]any)
	content, _ := data["content"].(string)
	if !strings.HasPrefix(content, "❌") {
		t.Errorf("content = %q, want a failure (❌) reply for a wrong PIN", content)
	}
}

func TestInteractionsHandlerReassignSubcommand(t *testing.T) {
	srv, _, pub, priv, _ := newDiscordTestServer(t)
	defer srv.Close()

	// The server in newDiscordTestServer wraps a fresh db each time; reach
	// it through the handler's own Dispatch is not exposed, so assign a
	// ticket the same way the HTTP-level tests do, via a second server built
	// on the same db — simplest is to just assert the reply reports success
	// against a ticket id that doesn't exist, proving the subcommand wired
	// through to Dispatch's own "no such open ticket" check, not a parsing bug.
	body := discordCommand("111222333", "reassign", map[string]any{"pin": "1234", "ticket_id": 999, "to": "pat"})
	req := signedDiscordRequest(t, srv.URL+"/discord/interactions", pub, priv, body)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	data, _ := out["data"].(map[string]any)
	content, _ := data["content"].(string)
	if content != "❌ no such open ticket" {
		t.Errorf("content = %q, want the Dispatch error surfaced verbatim", content)
	}
}

func TestInteractionsHandlerAlertsSubcommand(t *testing.T) {
	srv, _, pub, priv, db := newDiscordTestServer(t)
	defer srv.Close()

	if _, _, err := db.RecordCheckOutcome("dave", lego.AccuracyChecker, lego.TicketCheck, "75192-1", 500, 25); err != nil {
		t.Fatal(err)
	}

	body := discordCommand("111222333", "alerts", map[string]any{"pin": "1234"})
	req := signedDiscordRequest(t, srv.URL+"/discord/interactions", pub, priv, body)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	data, _ := out["data"].(map[string]any)
	content, _ := data["content"].(string)
	if content != "✅ 1 open alert(s) — see Admin → Alerts for details" {
		t.Errorf("content = %q, want it to report the one open alert", content)
	}
}
