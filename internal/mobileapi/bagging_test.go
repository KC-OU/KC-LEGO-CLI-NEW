package mobileapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func postFinishWithBag(t *testing.T, srv *httptest.Server, token string, force bool, bagCode string) (int, finishSummary) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/mobile/finish", jsonBody(t, finishRequest{Force: force, BagCode: bagCode}))
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

// TestFinishCheckHoldsForABagCodeWhenAnySmallBagLineExists is the
// bag-barcode-verification gate under test: a check with a small-bag part
// can't be finished (even with force, for missing parts) until a bag code
// has actually been confirmed, once. Supplying it on the same request that
// clears the missing-parts warning finishes in one step.
func TestFinishCheckHoldsForABagCodeWhenAnySmallBagLineExists(t *testing.T) {
	s, db := checkWalkEnv(t)
	if err := db.SetBagSize("3001", "small"); err != nil {
		t.Fatal(err)
	}
	srv := newCheckWalkTestServer(s)
	defer srv.Close()
	token := issueToken(t, db)

	// Clear every line (no missing) so only the bag gate is left to clear.
	for pos := 0; pos < 2; pos++ {
		if code, _ := postConfirm(t, srv, token, confirmRequest{Position: pos, Action: "have_all"}); code != http.StatusOK {
			t.Fatalf("confirm pos %d: status=%d", pos, code)
		}
	}

	status, summary := postFinish(t, srv, token, false)
	if status != http.StatusConflict || !summary.NeedsBagCode || summary.Finished {
		t.Fatalf("expected a bag-code hold, got status=%d summary=%+v", status, summary)
	}
	if len(summary.SmallBagParts) == 0 {
		t.Error("expected the small-bag parts to be listed in the warning")
	}

	status, summary = postFinishWithBag(t, srv, token, false, "BAG-0099")
	if status != http.StatusOK || !summary.Finished {
		t.Fatalf("expected finishing to succeed once a bag code is given, got status=%d summary=%+v", status, summary)
	}
}

// TestFinishCheckSkipsTheBagGateWithNoSmallBagLines confirms a completely
// ordinary check (nothing marked small-bag) is unaffected — no regression
// for the overwhelming majority of checks that never touch bagging at all.
func TestFinishCheckSkipsTheBagGateWithNoSmallBagLines(t *testing.T) {
	s, db := checkWalkEnv(t)
	srv := newCheckWalkTestServer(s)
	defer srv.Close()
	token := issueToken(t, db)

	for pos := 0; pos < 2; pos++ {
		if code, _ := postConfirm(t, srv, token, confirmRequest{Position: pos, Action: "have_all"}); code != http.StatusOK {
			t.Fatalf("confirm pos %d: status=%d", pos, code)
		}
	}
	status, summary := postFinish(t, srv, token, false)
	if status != http.StatusOK || !summary.Finished || summary.NeedsBagCode {
		t.Fatalf("expected a normal finish with no bag gate, got status=%d summary=%+v", status, summary)
	}
}
