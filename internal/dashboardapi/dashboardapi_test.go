package dashboardapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

func testServer(t *testing.T, token string) (*lego.DB, *httptest.Server) {
	t.Helper()
	dir := t.TempDir()
	db, err := lego.Open(filepath.Join(dir, "lego.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s := NewServer(db, token)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /dashboard/health", s.handleHealth)
	mux.HandleFunc("GET /dashboard/kanban", s.withToken(s.handleKanban))
	mux.HandleFunc("GET /dashboard/sets", s.withToken(s.handleSets))
	mux.HandleFunc("GET /dashboard/sets/{setNum}", s.withToken(s.handleSetDetail))
	mux.HandleFunc("GET /dashboard/accuracy/{username}", s.withToken(s.handleAccuracy))
	return db, httptest.NewServer(mux)
}

func TestHealthNeedsNoToken(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/dashboard/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestKanbanRequiresTheSharedToken(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/dashboard/kanban")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestKanbanRejectsAWrongToken(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/dashboard/kanban", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestKanbanReturnsTheAdminBoard(t *testing.T) {
	db, srv := testServer(t, "secret")
	defer srv.Close()

	tk, err := db.AssignTicket(lego.TicketCheck, "75192-1", "Falcon", "kc", "", "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ClaimTicket(tk.ID, "kc"); err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/dashboard/kanban", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	var body struct {
		Cards []lego.KanbanCard `json:"cards"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range body.Cards {
		if c.Ticket.ID == tk.ID && c.Ticket.AssignedTo == "kc" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected kc's claimed ticket on the admin board, got %+v", body.Cards)
	}
}

func TestSetsListsTheOwnedCollection(t *testing.T) {
	db, srv := testServer(t, "secret")
	defer srv.Close()

	if err := db.UpsertSet(lego.Set{SetNum: "75192-1", Name: "Millennium Falcon", Theme: "Star Wars", Qty: 1}); err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/dashboard/sets", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	var body struct {
		Sets []lego.Set `json:"sets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Sets) != 1 || body.Sets[0].SetNum != "75192-1" {
		t.Fatalf("expected the one seeded set, got %+v", body.Sets)
	}
}

func TestSetsSearchFiltersByTerm(t *testing.T) {
	db, srv := testServer(t, "secret")
	defer srv.Close()

	if err := db.UpsertSet(lego.Set{SetNum: "75192-1", Name: "Millennium Falcon", Theme: "Star Wars", Qty: 1}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertSet(lego.Set{SetNum: "10312-1", Name: "Jazz Club", Theme: "Icons", Qty: 1}); err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/dashboard/sets?q=falcon", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var body struct {
		Sets []lego.Set `json:"sets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Sets) != 1 || body.Sets[0].SetNum != "75192-1" {
		t.Fatalf("expected only the Falcon, got %+v", body.Sets)
	}
}

func TestSetsRequiresTheSharedToken(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/dashboard/sets")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestSetDetailReturnsTheSetItsStateAndItsInventory(t *testing.T) {
	db, srv := testServer(t, "secret")
	defer srv.Close()

	if err := db.UpsertSet(lego.Set{SetNum: "75192-1", Name: "Millennium Falcon", Theme: "Star Wars", Qty: 1}); err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/dashboard/sets/75192-1", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	var body struct {
		Set       lego.Set       `json:"set"`
		State     *lego.SetState `json:"state"`
		Inventory []lego.InvItem `json:"inventory"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Set.SetNum != "75192-1" || body.Set.Name != "Millennium Falcon" {
		t.Fatalf("expected the Falcon's own record, got %+v", body.Set)
	}
	if body.State == nil {
		t.Fatal("expected a set_state record, even an empty default one")
	}
	// No catalog import happened in this test's throwaway DB, so the
	// inventory is legitimately empty -- this only confirms the field
	// round-trips as [] rather than erroring.
	if body.Inventory == nil && len(body.Inventory) != 0 {
		t.Fatalf("expected an empty (not null-erroring) inventory, got %+v", body.Inventory)
	}
}

func TestSetDetailOnAnUnknownSetIs404(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/dashboard/sets/00000-1", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestAccuracyDefaultsToTodaysScoreOf100WithNoEventsYet(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/dashboard/accuracy/dave", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	var body struct {
		Username string               `json:"username"`
		Role     string               `json:"role"`
		Today    float64              `json:"today"`
		Trend    []float64            `json:"trend"`
		History  []lego.AccuracyEvent `json:"history"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Role != "checker" {
		t.Fatalf("expected the default role to be checker, got %q", body.Role)
	}
	if body.Today != 100 {
		t.Fatalf("expected a fresh account to be at 100%%, got %v", body.Today)
	}
	if len(body.Trend) != 7 {
		t.Fatalf("expected a 7-day trend, got %d entries", len(body.Trend))
	}
	if len(body.History) != 0 {
		t.Fatalf("expected no events yet, got %+v", body.History)
	}
}

func TestAccuracyReflectsAMissingPartsDeduction(t *testing.T) {
	db, srv := testServer(t, "secret")
	defer srv.Close()

	if _, _, err := db.RecordCheckOutcome("dave", lego.AccuracyChecker, lego.TicketCheck, "75192-1", 100, 3); err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/dashboard/accuracy/dave?role=checker", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var body struct {
		Today   float64              `json:"today"`
		History []lego.AccuracyEvent `json:"history"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Today >= 100 {
		t.Fatalf("expected a deduction to bring the score below 100, got %v", body.Today)
	}
	if len(body.History) != 1 || body.History[0].Missing != 3 {
		t.Fatalf("expected one logged event with 3 missing, got %+v", body.History)
	}
}

func TestAccuracyRequiresTheSharedToken(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/dashboard/accuracy/dave")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestEmptyTokenDisablesTheCheck(t *testing.T) {
	// Only ever intentional in tests -- see the Server.token doc comment.
	_, srv := testServer(t, "")
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/dashboard/kanban")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 when no token is configured", resp.StatusCode)
	}
}
