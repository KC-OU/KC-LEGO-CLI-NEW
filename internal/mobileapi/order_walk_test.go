package mobileapi

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/audit"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

// orderWalkEnv wires a scratch lego.db with a supplier order (two lines,
// nothing received yet) claimed by "dave" — the order-picking counterpart
// to check_walk_test.go's checkWalkEnv.
func orderWalkEnv(t *testing.T) (*Server, *lego.DB) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(config.AuditLogFile, filepath.Join(dir, "audit.log"))
	db, err := lego.Open(filepath.Join(dir, "lego.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	order := &lego.Order{SupplierKind: "bricklink", Supplier: "BrickHQ", Status: "ordered"}
	if err := db.SaveOrder(order); err != nil {
		t.Fatal(err)
	}
	if err := db.AddOrderLine(order.ID, lego.OrderLine{PartNum: "3001", ColorName: "Red", PartName: "Brick 2 x 4", Qty: 10, UnitPrice: 0.1}); err != nil {
		t.Fatal(err)
	}
	if err := db.AddOrderLine(order.ID, lego.OrderLine{PartNum: "3002", ColorName: "Blue", PartName: "Brick 2 x 2", Qty: 5, UnitPrice: 0.08}); err != nil {
		t.Fatal(err)
	}

	tk, err := db.AssignTicket(lego.TicketOrder, strconv.FormatInt(order.ID, 10), "Order #"+strconv.FormatInt(order.ID, 10), "", "", "", "setup")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ClaimTicket(tk.ID, "dave"); err != nil {
		t.Fatal(err)
	}

	s := &Server{legoDB: db, rebrick: &lego.Client{}, audit: audit.New()}
	return s, db
}

func TestNextOnAnOrderTicketReturnsAnOrderLine(t *testing.T) {
	s, _ := orderWalkEnv(t)
	srv := newCheckWalkTestServer(s)
	defer srv.Close()
	token := issueToken(t, s.legoDB)

	status, v := getLine(t, srv, token, 0)
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if v.Kind != "order" || v.Total != 2 || v.Position != 1 {
		t.Fatalf("line = %+v, want kind=order total=2 position=1", v)
	}
	if v.PartNum != "3001" || v.Need != 10 || v.Have != 0 || v.Missing != 10 {
		t.Fatalf("line = %+v, want 3001 need=10 have=0 missing=10", v)
	}
}

func TestConfirmReceiveBooksTheQuantityAndPersists(t *testing.T) {
	s, db := orderWalkEnv(t)
	srv := newCheckWalkTestServer(s)
	defer srv.Close()
	token := issueToken(t, db)

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/mobile/confirm", jsonBody(t, confirmRequest{Position: 0, Action: "receive", Count: 6}))
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var v lineView
	_ = json.NewDecoder(resp.Body).Decode(&v)
	if resp.StatusCode != http.StatusOK || v.Have != 6 || v.Missing != 4 {
		t.Fatalf("status = %d, v = %+v, want have=6 missing=4", resp.StatusCode, v)
	}

	// Persisted — a fresh /next call sees the same state.
	_, again := getLine(t, srv, token, 0)
	if again.Have != 6 {
		t.Errorf("after confirming, a fresh /next shows have=%d — not persisted", again.Have)
	}
}

func TestConfirmReceiveClampsToWhatsOutstanding(t *testing.T) {
	s, db := orderWalkEnv(t)
	srv := newCheckWalkTestServer(s)
	defer srv.Close()
	token := issueToken(t, db)

	// Line 1 only needs 5 — asking to receive 50 should clamp to 5, not error.
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/mobile/confirm", jsonBody(t, confirmRequest{Position: 1, Action: "receive", Count: 50}))
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var v lineView
	_ = json.NewDecoder(resp.Body).Decode(&v)
	if resp.StatusCode != http.StatusOK || v.Have != 5 || v.Missing != 0 {
		t.Fatalf("status = %d, v = %+v, want clamped to have=5 missing=0", resp.StatusCode, v)
	}
}

func TestConfirmOnAnOrderRejectsACheckOnlyAction(t *testing.T) {
	s, db := orderWalkEnv(t)
	srv := newCheckWalkTestServer(s)
	defer srv.Close()
	token := issueToken(t, db)

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/mobile/confirm", jsonBody(t, confirmRequest{Position: 0, Action: "have_all"}))
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for a check-only action on an order", resp.StatusCode)
	}
}
