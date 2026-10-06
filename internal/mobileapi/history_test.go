package mobileapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

func newHistoryTestServer(s *Server) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /mobile/history", s.withSession(s.handleHistory))
	return httptest.NewServer(mux)
}

// TestHistoryReturnsOnlyTheSignedInUsersOwnFinishedTickets is the direct
// test for the check/pick history screen's data: dave's claimed-but-not-
// finished ticket from checkWalkEnv doesn't show up (only done ones do),
// and a finished ticket belonging to someone else doesn't leak in.
func TestHistoryReturnsOnlyTheSignedInUsersOwnFinishedTickets(t *testing.T) {
	s, db := checkWalkEnv(t)
	srv := newHistoryTestServer(s)
	defer srv.Close()
	token := issueToken(t, db)

	resp := authed(t, http.MethodGet, srv.URL+"/mobile/history", token, nil)
	var rows []historyRow
	_ = json.NewDecoder(resp.Body).Decode(&rows)
	if resp.StatusCode != http.StatusOK || len(rows) != 0 {
		t.Fatalf("history before finishing anything = %d %+v, want none yet (the claimed one isn't done)", resp.StatusCode, rows)
	}

	now := time.Now()
	if _, err := db.Exec(`UPDATE job_tickets SET status = ?, claimed_at = ?, done_at = ? WHERE kind = ? AND target = ? AND assigned_to = 'dave'`,
		lego.TicketDone, now.Add(-15*time.Minute).Format(time.RFC3339), now.Format(time.RFC3339), lego.TicketCheck, "75192-1"); err != nil {
		t.Fatal(err)
	}
	otherTk, err := db.AssignTicket(lego.TicketCheck, "10696-1", "Classic Box", "", "", "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ClaimTicket(otherTk.ID, "sam"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE job_tickets SET status = ?, done_at = ? WHERE id = ?`, lego.TicketDone, now.Format(time.RFC3339), otherTk.ID); err != nil {
		t.Fatal(err)
	}

	resp = authed(t, http.MethodGet, srv.URL+"/mobile/history", token, nil)
	rows = nil
	_ = json.NewDecoder(resp.Body).Decode(&rows)
	if resp.StatusCode != http.StatusOK || len(rows) != 1 {
		t.Fatalf("history = %d %+v, want exactly dave's one finished ticket", resp.StatusCode, rows)
	}
	if rows[0].Target != "75192-1" || rows[0].DurationSeconds != 900 {
		t.Errorf("history row = %+v, want 75192-1 at 900s (15 minutes)", rows[0])
	}
}
