package dashboardapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestSessionsListsWhatWasSeen(t *testing.T) {
	db, srv := testServer(t, "secret")
	defer srv.Close()

	if err := db.Heartbeat("sess1", "dave", "checker", "telnet", "", "hub"); err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/dashboard/admin/sessions", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var rows []sessionRow
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Username != "dave" || rows[0].SessionID != "sess1" {
		t.Fatalf("expected the one live session, got %+v", rows)
	}
}

func TestMessageUserSendsAMessage(t *testing.T) {
	db, srv := testServer(t, "secret")
	defer srv.Close()

	body := `{"username":"dave","body":"please double check your last set","by":"admin"}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/dashboard/admin/message", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	msgs, err := db.UndeliveredMessages("dave")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].From != "admin" || msgs[0].Body != "please double check your last set" {
		t.Fatalf("expected the one sent message, got %+v", msgs)
	}
}

func TestMessageUserRequiresEveryField(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	body := `{"username":"dave","body":""}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/dashboard/admin/message", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestKickSessionFlagsAForceLogoff(t *testing.T) {
	db, srv := testServer(t, "secret")
	defer srv.Close()

	if err := db.Heartbeat("sess1", "dave", "checker", "telnet", "", "hub"); err != nil {
		t.Fatal(err)
	}

	body := `{"session_id":"sess1","message":"logged off remotely","by":"admin"}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/dashboard/admin/kick", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	msg, found, err := db.ConsumeForceLogoff("sess1")
	if err != nil || !found || msg != "logged off remotely" {
		t.Fatalf("ConsumeForceLogoff = %q, %v, %v", msg, found, err)
	}
}

func TestKickSessionRequiresEveryField(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	body := `{"session_id":"sess1","message":""}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/dashboard/admin/kick", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestSessionRoutesRequireTheSharedToken(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/dashboard/admin/sessions")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}

	for _, path := range []string{"/dashboard/admin/message", "/dashboard/admin/kick"} {
		resp, err := http.Post(srv.URL+path, "application/json", strings.NewReader(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s: status = %d, want 401", path, resp.StatusCode)
		}
	}
}
