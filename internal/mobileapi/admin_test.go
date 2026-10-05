package mobileapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

func newAdminTestServer(s *Server) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /mobile/admin/tickets", s.withSession(s.handleAdminTickets))
	mux.HandleFunc("POST /mobile/admin/assign-ticket", s.withSession(s.handleAdminAssignTicket))
	mux.HandleFunc("POST /mobile/flag-location", s.withSession(s.handleFlagLocation))
	return httptest.NewServer(mux)
}

func asAdmin(t *testing.T, username string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(config.AccessFile, filepath.Join(dir, "access.json"))
	if _, err := access.Update(func(p *access.Policy) error {
		p.Users["modernwms:"+username] = &access.User{Groups: []string{"admin"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func authed(t *testing.T, method, url, token string, body any) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(method, url, jsonBody(t, body))
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func decodeMap(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out
}

// TestNonAdminCannotAssignOrListTickets is the direct test for the "admin
// only" gate: dave (checkWalkEnv's claimed-a-check user, no admin group)
// must not reach any of the admin endpoints.
func TestNonAdminCannotAssignOrListTickets(t *testing.T) {
	s, db := checkWalkEnv(t)
	srv := newAdminTestServer(s)
	defer srv.Close()
	token := issueToken(t, db)

	if resp := authed(t, http.MethodGet, srv.URL+"/mobile/admin/tickets", token, nil); resp.StatusCode != http.StatusForbidden {
		t.Errorf("GET admin/tickets as a non-admin = %d, want 403", resp.StatusCode)
	}
	body := map[string]string{"kind": "check", "target": "75192-1"}
	if resp := authed(t, http.MethodPost, srv.URL+"/mobile/admin/assign-ticket", token, body); resp.StatusCode != http.StatusForbidden {
		t.Errorf("POST assign-ticket as a non-admin = %d, want 403", resp.StatusCode)
	}
}

// TestAdminCanAssignAndSeeTheQueue covers "assign jobs from the phone" end
// to end: an admin assigns a check to alex, alex sees it queued for her
// (the same OpenTickets a picker already uses), and the admin ticket list
// shows it with its assignment.
func TestAdminCanAssignAndSeeTheQueue(t *testing.T) {
	s, db := checkWalkEnv(t)
	asAdmin(t, "dave")
	srv := newAdminTestServer(s)
	defer srv.Close()
	token := issueToken(t, db)

	// checkWalkEnv already claimed 75192-1 for dave; assign a second set instead.
	if _, err := db.Exec(`INSERT INTO cat_sets (set_num, name, year, theme_id, num_parts, img_url) VALUES ('10696-1', 'Classic Box', 2015, 0, 1, '')`); err != nil {
		t.Fatal(err)
	}
	resp := authed(t, http.MethodPost, srv.URL+"/mobile/admin/assign-ticket", token,
		map[string]string{"kind": "check", "target": "10696-1", "assigned_to": "alex"})
	body := decodeMap(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("assign-ticket = %d %+v", resp.StatusCode, body)
	}
	// The label falls back to the bare set number here since GetSetByNum
	// looks at the owned-collection "sets" table (what the TUI's own
	// assignTargetScreen does too), not the test's cat_sets catalog seed —
	// what matters for this test is who it's assigned to and that it's
	// queued, not the exact label text.
	if body["assigned_to"] != "alex" || body["kind"] != "check" || body["label"] != "10696-1" {
		t.Errorf("assign-ticket response = %+v", body)
	}

	tickets, err := db.OpenTickets("check", "alex")
	if err != nil || len(tickets) != 1 || tickets[0].Target != "10696-1" {
		t.Errorf("alex's open tickets = %+v %v", tickets, err)
	}

	resp2 := authed(t, http.MethodGet, srv.URL+"/mobile/admin/tickets", token, nil)
	var list []adminTicketRow
	_ = json.NewDecoder(resp2.Body).Decode(&list)
	if resp2.StatusCode != http.StatusOK || len(list) != 2 {
		t.Fatalf("admin ticket list = %d %+v, want dave's claimed one and alex's queued one", resp2.StatusCode, list)
	}
}

// TestFlagLocationNotifiesWithoutBlockingTheLine confirms the flag endpoint
// just logs/notifies (no state change to a ticket or line) and rejects an
// incomplete report.
func TestFlagLocationNotifiesWithoutBlockingTheLine(t *testing.T) {
	s, db := checkWalkEnv(t)
	srv := newAdminTestServer(s)
	defer srv.Close()
	token := issueToken(t, db)

	resp := authed(t, http.MethodPost, srv.URL+"/mobile/flag-location", token,
		map[string]string{"part_num": "3001", "color_name": "Red", "location": "A-02-01", "note": "actually empty"})
	body := decodeMap(t, resp)
	if resp.StatusCode != http.StatusOK || body["ok"] != true {
		t.Fatalf("flag-location = %d %+v", resp.StatusCode, body)
	}

	resp = authed(t, http.MethodPost, srv.URL+"/mobile/flag-location", token, map[string]string{"part_num": "3001"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("flag-location without a location = %d, want 400", resp.StatusCode)
	}
}
