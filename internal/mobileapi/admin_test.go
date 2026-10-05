package mobileapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

func newAdminTestServer(s *Server) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /mobile/admin/tickets", s.withSession(s.handleAdminTickets))
	mux.HandleFunc("POST /mobile/admin/assign-ticket", s.withSession(s.handleAdminAssignTicket))
	mux.HandleFunc("POST /mobile/admin/reopen-ticket", s.withSession(s.handleAdminReopenTicket))
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

// TestAdminCanReopenAFinishedCheck covers "reopen a wrong check/order": a
// reason is required, and the finished ticket checkWalkEnv's claim/finish
// would have produced stays untouched while a fresh open one appears.
func TestAdminCanReopenAFinishedCheck(t *testing.T) {
	s, db := checkWalkEnv(t)
	asAdmin(t, "dave")
	srv := newAdminTestServer(s)
	defer srv.Close()
	token := issueToken(t, db)

	tickets, err := db.AllOpenTickets()
	if err != nil || len(tickets) != 1 {
		t.Fatalf("checkWalkEnv's claimed ticket = %+v %v", tickets, err)
	}
	if err := db.FinishTicket("check", "75192-1", "dave"); err != nil {
		t.Fatal(err)
	}

	resp := authed(t, http.MethodPost, srv.URL+"/mobile/admin/reopen-ticket", token,
		map[string]string{"kind": "check", "target": "75192-1"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("reopen without a reason = %d, want 400", resp.StatusCode)
	}

	resp = authed(t, http.MethodPost, srv.URL+"/mobile/admin/reopen-ticket", token,
		map[string]string{"kind": "check", "target": "75192-1", "reason": "miscounted"})
	body := decodeMap(t, resp)
	if resp.StatusCode != http.StatusOK || body["status"] != "queued" {
		t.Fatalf("reopen-ticket = %d %+v", resp.StatusCode, body)
	}

	open, err := db.OpenTickets("check", "anyone")
	if err != nil || len(open) != 1 {
		t.Errorf("open tickets after reopening = %+v %v, want exactly one", open, err)
	}
}

// TestAdminTicketListShowsEstimatedAlongsideElapsed is the supervisor-view
// half of the time-estimate feature: dave's claimed check (checkWalkEnv's
// setup) carries both an elapsed time and, once there's finished-ticket
// history for "check", an estimate too.
func TestAdminTicketListShowsEstimatedAlongsideElapsed(t *testing.T) {
	s, db := checkWalkEnv(t)
	asAdmin(t, "dave")
	srv := newAdminTestServer(s)
	defer srv.Close()
	token := issueToken(t, db)

	tk, err := db.AssignTicket(lego.TicketCheck, "10696-1", "Classic Box", "", "", "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ClaimTicket(tk.ID, "sam"); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if _, err := db.Exec(`UPDATE job_tickets SET status = ?, claimed_at = ?, done_at = ? WHERE id = ?`,
		lego.TicketDone, now.Add(-10*time.Minute).Format(time.RFC3339), now.Format(time.RFC3339), tk.ID); err != nil {
		t.Fatal(err)
	}

	resp := authed(t, http.MethodGet, srv.URL+"/mobile/admin/tickets", token, nil)
	var list []adminTicketRow
	_ = json.NewDecoder(resp.Body).Decode(&list)
	if resp.StatusCode != http.StatusOK || len(list) != 1 {
		t.Fatalf("admin ticket list = %d %+v, want dave's one still-claimed check", resp.StatusCode, list)
	}
	row := list[0]
	if row.ClaimedAt == "" || row.ClaimedForSeconds < 0 {
		t.Errorf("claimed_at/claimed_for_seconds = %q/%d, want a present, non-negative elapsed time", row.ClaimedAt, row.ClaimedForSeconds)
	}
	if row.EstimatedSeconds != 600 {
		t.Errorf("estimated_seconds = %d, want 600 (10 minutes) once there's history", row.EstimatedSeconds)
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
