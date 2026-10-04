package mobileapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestMessagesReturnsUndeliveredThenClears(t *testing.T) {
	s, db := checkWalkEnv(t)
	srv := newCheckWalkTestServer(s)
	defer srv.Close()
	token := issueToken(t, db)

	if err := db.SendMessage("admin", "dave", "Running low on red 2x4 — check the back shelf."); err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/mobile/messages", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var msgs []mobileMessage
	_ = json.NewDecoder(resp.Body).Decode(&msgs)
	if resp.StatusCode != http.StatusOK || len(msgs) != 1 || msgs[0].From != "admin" {
		t.Fatalf("status = %d, msgs = %+v, want one message from admin", resp.StatusCode, msgs)
	}

	// Delivered — a second poll sees nothing new, same as the TUI's own pollMessages.
	req2, _ := http.NewRequest(http.MethodGet, srv.URL+"/mobile/messages", nil)
	req2.Header.Set("Authorization", "Bearer "+token)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	var again []mobileMessage
	_ = json.NewDecoder(resp2.Body).Decode(&again)
	if len(again) != 0 {
		t.Errorf("second poll = %+v, want empty — already delivered", again)
	}
}

func TestMessageAdminRecordsAnEventAndRejectsBlank(t *testing.T) {
	s, db := checkWalkEnv(t)
	srv := newCheckWalkTestServer(s)
	defer srv.Close()
	token := issueToken(t, db)

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/mobile/message-admin", jsonBody(t, messageAdminRequest{Body: "Running 20 minutes late today."}))
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	req2, _ := http.NewRequest(http.MethodPost, srv.URL+"/mobile/message-admin", jsonBody(t, messageAdminRequest{Body: "   "}))
	req2.Header.Set("Authorization", "Bearer "+token)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for a blank message", resp2.StatusCode)
	}
}
