package mobileapi

import (
	"net/http"
	"testing"
)

// TestResumeOrderWithNoPosParamPicksUpAfterTheLastReceivedLine is
// check_walk_resume_test.go's TestResumeWithNoPosParamPicksUpAfterTheLast
// ConfirmedLine for order picking -- same gap, same fix
// (AdvanceOrderPosition), same forward-only MAX semantics.
func TestResumeOrderWithNoPosParamPicksUpAfterTheLastReceivedLine(t *testing.T) {
	s, db := orderWalkEnv(t)
	srv := newCheckWalkTestServer(s)
	defer srv.Close()
	token := issueToken(t, db)

	_, first := getLineResume(t, srv, token)
	if first.Position != 1 {
		t.Fatalf("a never-touched order should resume at line 0 (Position 1), got Position %d", first.Position)
	}

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/mobile/confirm", jsonBody(t, confirmRequest{Position: 0, Action: "receive", Count: 10}))
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("confirm line 0: status = %d", resp.StatusCode)
	}

	status, resumed := getLineResume(t, srv, token)
	if status != http.StatusOK {
		t.Fatalf("resume: status = %d", status)
	}
	if resumed.Position != 2 {
		t.Errorf("after receiving line 0, resuming with no ?pos landed on Position %d, want 2", resumed.Position)
	}

	_, explicit := getLine(t, srv, token, 0)
	if explicit.Position != 1 {
		t.Errorf("explicit pos=0 should still show line 0 (Position 1), got Position %d", explicit.Position)
	}
}
