package mobileapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

func postFinish(t *testing.T, srv *httptest.Server, token string, force bool) (int, finishSummary) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/mobile/finish", jsonBody(t, finishRequest{Force: force}))
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var v finishSummary
	_ = json.NewDecoder(resp.Body).Decode(&v)
	return resp.StatusCode, v
}

func postConfirm(t *testing.T, srv *httptest.Server, token string, req confirmRequest) (int, lineView) {
	t.Helper()
	r, _ := http.NewRequest(http.MethodPost, srv.URL+"/mobile/confirm", jsonBody(t, req))
	r.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var v lineView
	_ = json.NewDecoder(resp.Body).Decode(&v)
	return resp.StatusCode, v
}

// TestFinishCheckWarnsWhenMissingWithoutForce is the mobile walk's missing
// piece under test: reaching the end of a check used to leave the ticket
// claimed forever with nothing recorded. Without force, a check with
// anything still missing comes back as a warning, not a completion.
func TestFinishCheckWarnsWhenMissingWithoutForce(t *testing.T) {
	s, _ := checkWalkEnv(t)
	srv := newCheckWalkTestServer(s)
	defer srv.Close()
	token := issueToken(t, s.legoDB)

	// Mark the first line (10 needed) as missing 3.
	if status, _ := postConfirm(t, srv, token, confirmRequest{Position: 0, Action: "missing", Count: 3}); status != http.StatusOK {
		t.Fatalf("confirm status = %d", status)
	}

	status, summary := postFinish(t, srv, token, false)
	if status != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (still missing, not forced)", status)
	}
	if summary.Finished {
		t.Error("Finished = true, want false — nothing should be recorded yet")
	}
	if summary.Missing == 0 || len(summary.Lines) == 0 {
		t.Errorf("summary = %+v, want a non-zero Missing and at least one line listed", summary)
	}

	// The ticket must still be open — finishing was refused, not silently applied.
	if cur, err := s.legoDB.CurrentTicket("dave"); err != nil || cur == nil {
		t.Fatalf("CurrentTicket = %+v, %v, want the check still claimed after a refused finish", cur, err)
	}
}

func TestFinishCheckForcedCompletesAndDocksAccuracy(t *testing.T) {
	s, _ := checkWalkEnv(t)
	srv := newCheckWalkTestServer(s)
	defer srv.Close()
	token := issueToken(t, s.legoDB)

	if status, _ := postConfirm(t, srv, token, confirmRequest{Position: 0, Action: "missing", Count: 3}); status != http.StatusOK {
		t.Fatalf("confirm status = %d", status)
	}

	status, summary := postFinish(t, srv, token, true)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (forced)", status)
	}
	if !summary.Finished || summary.Missing != 3 {
		t.Errorf("summary = %+v, want Finished=true, Missing=3", summary)
	}
	if summary.AccuracyPct >= 100 {
		t.Errorf("accuracy_pct = %v, want below 100 after a forced finish with missing parts", summary.AccuracyPct)
	}

	// The ticket is released — nothing left for this user to resume.
	if cur, err := s.legoDB.CurrentTicket("dave"); err != nil || cur != nil {
		t.Fatalf("CurrentTicket = %+v, %v, want nil after finishing", cur, err)
	}
}

func TestFinishCheckCleanKeepsFullAccuracy(t *testing.T) {
	s, _ := checkWalkEnv(t)
	srv := newCheckWalkTestServer(s)
	defer srv.Close()
	token := issueToken(t, s.legoDB)

	if status, _ := postConfirm(t, srv, token, confirmRequest{Position: 0, Action: "have_all"}); status != http.StatusOK {
		t.Fatalf("confirm status = %d", status)
	}
	if status, _ := postConfirm(t, srv, token, confirmRequest{Position: 1, Action: "have_all"}); status != http.StatusOK {
		t.Fatalf("confirm status = %d", status)
	}

	status, summary := postFinish(t, srv, token, false)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 — nothing missing, no force needed", status)
	}
	if !summary.Finished || summary.Missing != 0 || summary.AccuracyPct != 100 {
		t.Errorf("summary = %+v, want Finished=true, Missing=0, AccuracyPct=100", summary)
	}
}

func TestFinishWithNothingAssignedIsBadRequest(t *testing.T) {
	s, db := checkWalkEnv(t)
	// Finish the one ticket this env sets up first, so nothing is left claimed.
	if err := db.FinishTicket(lego.TicketCheck, "75192-1", "dave"); err != nil {
		t.Fatal(err)
	}
	srv := newCheckWalkTestServer(s)
	defer srv.Close()
	token := issueToken(t, db)

	status, _ := postFinish(t, srv, token, false)
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 with nothing claimed", status)
	}
}

// The order-ticket fixture (two lines: pos 0 needs 10, pos 1 needs 5) is
// order_walk_test.go's own orderWalkEnv — reused here rather than rebuilt.

func TestFinishOrderWarnsThenForcesReceivesTheRest(t *testing.T) {
	s, db := orderWalkEnv(t)
	srv := newCheckWalkTestServer(s)
	defer srv.Close()
	token := issueToken(t, db)

	orders, err := db.ListOrders("ordered")
	if err != nil || len(orders) != 1 {
		t.Fatalf("ListOrders = %+v, %v", orders, err)
	}
	orderID := orders[0].ID

	// Receive 6 of the 10 needed on line 0; line 1 (needs 5) is untouched.
	if status, _ := postConfirm(t, srv, token, confirmRequest{Position: 0, Action: "receive", Count: 6}); status != http.StatusOK {
		t.Fatalf("confirm status = %d", status)
	}

	status, summary := postFinish(t, srv, token, false)
	if status != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (4 + 5 still outstanding)", status)
	}
	if summary.Missing != 9 {
		t.Errorf("missing = %d, want 9 (4 on line 0, 5 on line 1)", summary.Missing)
	}

	status, summary = postFinish(t, srv, token, true)
	if status != http.StatusOK || !summary.Finished {
		t.Fatalf("status = %d, summary = %+v, want 200 Finished=true", status, summary)
	}
	// The ticket is closed now (same as the TUI's "mark received"), so
	// /next can no longer show it — verify completion straight from the DB.
	o, err := db.GetOrder(orderID)
	if err != nil {
		t.Fatal(err)
	}
	if o.Status != "received" || o.Lines[0].ReceivedQty != 10 || o.Lines[1].ReceivedQty != 5 {
		t.Errorf("order = %+v, want status=received, both lines fully received", o)
	}
	if cur, err := db.CurrentTicket("dave"); err != nil || cur != nil {
		t.Errorf("CurrentTicket = %+v, %v, want nil after finishing", cur, err)
	}
}

func TestUnreceiveReversesAMisScan(t *testing.T) {
	s, db := orderWalkEnv(t)
	srv := newCheckWalkTestServer(s)
	defer srv.Close()
	token := issueToken(t, db)

	if status, _ := postConfirm(t, srv, token, confirmRequest{Position: 0, Action: "receive", Count: 4}); status != http.StatusOK {
		t.Fatalf("confirm status = %d", status)
	}
	status, v := postConfirm(t, srv, token, confirmRequest{Position: 0, Action: "unreceive", Count: 4})
	if status != http.StatusOK {
		t.Fatalf("unreceive status = %d", status)
	}
	if v.Have != 0 {
		t.Errorf("have = %d after undoing the whole receive, want 0", v.Have)
	}
	_, again := getLine(t, srv, token, 0)
	if again.Have != 0 {
		t.Errorf("after undoing, a fresh /next shows have=%d — not persisted", again.Have)
	}
}

func TestUnreceiveClampsAtZero(t *testing.T) {
	s, db := orderWalkEnv(t)
	srv := newCheckWalkTestServer(s)
	defer srv.Close()
	token := issueToken(t, db)

	if status, _ := postConfirm(t, srv, token, confirmRequest{Position: 0, Action: "receive", Count: 2}); status != http.StatusOK {
		t.Fatalf("confirm status = %d", status)
	}
	status, v := postConfirm(t, srv, token, confirmRequest{Position: 0, Action: "unreceive", Count: 99})
	if status != http.StatusOK {
		t.Fatalf("unreceive status = %d", status)
	}
	if v.Have != 0 {
		t.Errorf("have = %d, want clamped to 0, not negative", v.Have)
	}
}
