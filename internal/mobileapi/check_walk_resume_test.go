package mobileapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// getLineResume is getLine without a ?pos at all -- "I have no local memory
// of where I was," the shape a fresh app load (including a different
// device) actually sends. getLine always sets ?pos=<n> explicitly, which
// can never exercise this path.
func getLineResume(t *testing.T, srv *httptest.Server, token string) (int, lineView) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/mobile/next", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var v lineView
	_ = json.NewDecoder(resp.Body).Decode(&v)
	return resp.StatusCode, v
}

// TestResumeWithNoPosParamPicksUpAfterTheLastConfirmedLine is the actual
// "resume a check on another device" contract: a second device opening the
// same check sends no ?pos (it has never seen this ticket before), and must
// land past whatever was already confirmed -- not back at line 0, which
// would otherwise look identical to "never started" (an unconfirmed line
// defaults to have == need, the same value "have_all" sets).
func TestResumeWithNoPosParamPicksUpAfterTheLastConfirmedLine(t *testing.T) {
	s, db := checkWalkEnv(t)
	srv := newCheckWalkTestServer(s)
	defer srv.Close()
	token := issueToken(t, db)

	// Fresh ticket, nothing confirmed yet: resume (no pos) and explicit
	// pos=0 must agree -- both show line 0.
	_, first := getLineResume(t, srv, token)
	if first.Position != 1 { // lineView.Position is 1-indexed for display; array index 0 == Position 1
		t.Fatalf("a never-touched check should resume at line 0 (Position 1), got Position %d", first.Position)
	}

	// Confirm line 0 from "device A".
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/mobile/confirm", jsonBody(t, confirmRequest{Position: 0, Action: "have_all"}))
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("confirm line 0: status = %d", resp.StatusCode)
	}

	// "Device B" opens the same check with no local position memory.
	status, resumed := getLineResume(t, srv, token)
	if status != http.StatusOK {
		t.Fatalf("resume: status = %d", status)
	}
	if resumed.Position != 2 { // array index 1 (the line after the one just confirmed) == Position 2
		t.Errorf("after confirming line 0, resuming with no ?pos landed on Position %d, want 2", resumed.Position)
	}

	// Explicit navigation back to line 0 must still work -- the resume
	// cursor only applies when the client sends no position at all.
	_, explicit := getLine(t, srv, token, 0)
	if explicit.Position != 1 {
		t.Errorf("explicit pos=0 should still show line 0 (Position 1) even after the resume cursor advanced, got Position %d", explicit.Position)
	}
}

// TestAdvanceCheckPositionNeverGoesBackward covers the MAX semantics
// directly: jumping back to fix an earlier line (e.g. from the "missing
// parts" list) and confirming it must not un-advance the resume point a
// different device would pick up.
func TestAdvanceCheckPositionNeverGoesBackward(t *testing.T) {
	s, db := checkWalkEnv(t)
	srv := newCheckWalkTestServer(s)
	defer srv.Close()
	token := issueToken(t, db)

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/mobile/confirm", jsonBody(t, confirmRequest{Position: 1, Action: "have_all"}))
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("confirm line 1: status = %d", resp.StatusCode)
	}

	var checkID int64
	if err := db.QueryRow(`SELECT id FROM set_checks ORDER BY id DESC LIMIT 1`).Scan(&checkID); err != nil {
		t.Fatal(err)
	}
	c, err := db.GetCheck(checkID)
	if err != nil {
		t.Fatal(err)
	}
	if c.LastPosition != 2 {
		t.Fatalf("last_position after confirming line 1 = %d, want 2", c.LastPosition)
	}

	// Jump back and confirm line 0 -- forward-only MAX must leave last_position at 2.
	if err := db.AdvanceCheckPosition(checkID, 1); err != nil {
		t.Fatal(err)
	}
	c2, err := db.GetCheck(checkID)
	if err != nil {
		t.Fatal(err)
	}
	if c2.LastPosition != 2 {
		t.Errorf("last_position after advancing backward to 1 = %d, want unchanged 2", c2.LastPosition)
	}
}
