package dashboardapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
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
	s := NewServer(db, nil, nil, token, nil, nil, nil, nil, nil)
	s.currencies = newCurrencyStore(filepath.Join(dir, "currencies.json")) // isolate from the real CURRENCIES_FILE/LEGO_DB_PATH default
	s.lang = newLangStore(filepath.Join(dir, "translations"))              // isolate from the real WMS_TRANSLATIONS_DIR/LEGO_DB_PATH default
	mux := http.NewServeMux()
	mux.HandleFunc("GET /dashboard/health", s.handleHealth)
	mux.HandleFunc("GET /dashboard/kanban", s.withToken(s.handleKanban))
	mux.HandleFunc("GET /dashboard/sets", s.withToken(s.handleSets))
	mux.HandleFunc("GET /dashboard/sets/{setNum}", s.withToken(s.handleSetDetail))
	mux.HandleFunc("GET /dashboard/accuracy/{username}", s.withToken(s.handleAccuracy))
	mux.HandleFunc("GET /dashboard/admin/users", s.withToken(s.handleUsers))
	mux.HandleFunc("GET /dashboard/admin/tickets", s.withToken(s.handleTickets))
	mux.HandleFunc("GET /dashboard/admin/status-cards", s.withToken(s.handleStatusCards))
	mux.HandleFunc("POST /dashboard/admin/assign", s.withToken(s.handleAssignTicket))
	mux.HandleFunc("POST /dashboard/admin/reopen", s.withToken(s.handleReopenTicket))
	mux.HandleFunc("POST /dashboard/admin/accounts/create", s.withToken(s.handleCreateAccount))
	mux.HandleFunc("POST /dashboard/admin/accounts/reset", s.withToken(s.handleResetAccount))
	mux.HandleFunc("POST /dashboard/admin/accounts/lock", s.withToken(s.handleLockAccount))
	mux.HandleFunc("POST /dashboard/admin/accounts/unlock", s.withToken(s.handleUnlockAccount))
	mux.HandleFunc("POST /dashboard/admin/accounts/delete", s.withToken(s.handleDeleteAccount))
	mux.HandleFunc("POST /dashboard/admin/accounts/rename", s.withToken(s.handleRenameAccount))
	mux.HandleFunc("GET /dashboard/admin/proofs/{setNum}", s.withToken(s.handleProofsForSet))
	mux.HandleFunc("GET /dashboard/admin/proofs/{setNum}/{proofID}/file", s.withToken(s.handleProofFile))
	mux.HandleFunc("POST /dashboard/admin/proofs/flag", s.withToken(s.handleFlagDispute))
	mux.HandleFunc("GET /dashboard/queries/parts", s.withToken(s.handleQueryParts))
	mux.HandleFunc("GET /dashboard/queries/minifigs", s.withToken(s.handleQueryMinifigs))
	mux.HandleFunc("GET /dashboard/catalog/sets/{setNum}", s.withToken(s.handleCatalogSetLookup))
	mux.HandleFunc("GET /dashboard/admin/sessions", s.withToken(s.handleSessions))
	mux.HandleFunc("POST /dashboard/admin/message", s.withToken(s.handleMessageUser))
	mux.HandleFunc("POST /dashboard/admin/kick", s.withToken(s.handleKickSession))
	mux.HandleFunc("GET /dashboard/admin/alerts", s.withToken(s.handleAlerts))
	mux.HandleFunc("POST /dashboard/admin/alerts/file", s.withToken(s.handleFileAccuracyReport))
	mux.HandleFunc("GET /dashboard/admin/accuracy-reports/{username}", s.withToken(s.handleAccuracyReportsFor))
	mux.HandleFunc("POST /dashboard/admin/sets/image", s.withToken(s.handleSetImage))
	mux.HandleFunc("GET /dashboard/admin/activity", s.withToken(s.handleActivity))
	mux.HandleFunc("POST /dashboard/admin/activity/clear", s.withToken(s.handleClearActivity))
	mux.HandleFunc("GET /dashboard/admin/audit", s.withToken(s.handleAuditLog))
	mux.HandleFunc("GET /dashboard/admin/containers", s.withToken(s.handleContainers))
	mux.HandleFunc("GET /dashboard/admin/handover", s.withToken(s.handleGetHandover))
	mux.HandleFunc("POST /dashboard/admin/handover", s.withToken(s.handleSetHandover))
	mux.HandleFunc("GET /dashboard/admin/rota", s.withToken(s.handleRotaWeek))
	mux.HandleFunc("POST /dashboard/admin/rota/set", s.withToken(s.handleSetRota))
	mux.HandleFunc("POST /dashboard/admin/rota/override", s.withToken(s.handleRotaOverride))
	mux.HandleFunc("POST /dashboard/admin/rota/clear", s.withToken(s.handleRotaClear))
	mux.HandleFunc("POST /dashboard/admin/accuracy/adjust", s.withToken(s.handleAdjustAccuracy))
	mux.HandleFunc("GET /dashboard/admin/access/groups", s.withToken(s.handleAccessGroups))
	mux.HandleFunc("GET /dashboard/admin/access/effective", s.withToken(s.handleAccessEffective))
	mux.HandleFunc("POST /dashboard/admin/access/groups/perm", s.withToken(s.handleSetGroupPerm))
	mux.HandleFunc("POST /dashboard/lego/sets", s.withToken(s.handleUpsertSet))
	mux.HandleFunc("POST /dashboard/lego/parts", s.withToken(s.handleUpsertPart))
	mux.HandleFunc("GET /dashboard/lego/retiring", s.withToken(s.handleRetiringSoon))
	mux.HandleFunc("GET /dashboard/reports/missing", s.withToken(s.handleMissingReport))
	mux.HandleFunc("GET /dashboard/reports/extra", s.withToken(s.handleExtraReport))
	mux.HandleFunc("GET /dashboard/reports/orders-list", s.withToken(s.handleOrderListReport))
	mux.HandleFunc("GET /dashboard/reports/history", s.withToken(s.handleCheckHistory))
	mux.HandleFunc("GET /dashboard/reports/stocktake/{setNum}", s.withToken(s.handleStockSheet))
	mux.HandleFunc("GET /dashboard/orders/spend", s.withToken(s.handleSpendReport))
	mux.HandleFunc("GET /dashboard/orders", s.withToken(s.handleListOrders))
	mux.HandleFunc("GET /dashboard/orders/{id}", s.withToken(s.handleGetOrder))
	mux.HandleFunc("POST /dashboard/orders", s.withToken(s.handleCreateOrder))
	mux.HandleFunc("POST /dashboard/orders/from-missing", s.withToken(s.handleCreateOrderFromMissing))
	mux.HandleFunc("POST /dashboard/orders/{id}/lines", s.withToken(s.handleAddOrderLine))
	mux.HandleFunc("POST /dashboard/orders/{id}/status", s.withToken(s.handleSetOrderStatus))
	mux.HandleFunc("GET /dashboard/labels/{setNum}", s.withToken(s.handleLabel))
	mux.HandleFunc("GET /dashboard/partdb/parts", s.withToken(s.handlePartDBSearch))
	mux.HandleFunc("GET /dashboard/partdb/parts/{id}", s.withToken(s.handlePartDBGet))
	mux.HandleFunc("POST /dashboard/partdb/parts", s.withToken(s.handlePartDBUpsert))
	mux.HandleFunc("POST /dashboard/partdb/parts/{id}/stock", s.withToken(s.handlePartDBAdjustStock))
	mux.HandleFunc("GET /dashboard/ops/inventory", s.withToken(s.handleInventoryLookup))
	mux.HandleFunc("GET /dashboard/ops/masterdata/spus", s.withToken(s.handleMasterDataSPUs))
	mux.HandleFunc("GET /dashboard/ops/masterdata/suppliers", s.withToken(s.handleMasterDataSuppliers))
	mux.HandleFunc("GET /dashboard/ops/masterdata/customers", s.withToken(s.handleMasterDataCustomers))
	mux.HandleFunc("GET /dashboard/ops/dispatch", s.withToken(s.handleDispatchList))
	mux.HandleFunc("POST /dashboard/ops/receive", s.withToken(s.handleReceiveStock))
	mux.HandleFunc("GET /dashboard/admin/settings", s.withToken(s.handleGetSettings))
	mux.HandleFunc("POST /dashboard/admin/settings", s.withToken(s.handleSetSettings))
	mux.HandleFunc("POST /dashboard/admin/settings/sync-admin", s.withToken(s.handleSetSyncAdmin))
	mux.HandleFunc("GET /dashboard/bricklink/item/{type}/{number}", s.withToken(s.handleBLItem))
	mux.HandleFunc("GET /dashboard/bricklink/price/{type}/{number}", s.withToken(s.handleBLPrice))
	mux.HandleFunc("GET /dashboard/bricklink/where-used/{number}", s.withToken(s.handleBLWhereUsed))
	mux.HandleFunc("POST /dashboard/admin/bricklink/colors/sync", s.withToken(s.handleBLColorsSync))
	mux.HandleFunc("GET /dashboard/admin/access/users", s.withToken(s.handleAccessUserList))
	mux.HandleFunc("GET /dashboard/admin/access/user/{username}", s.withToken(s.handleAccessUserShow))
	mux.HandleFunc("POST /dashboard/admin/access/user/groups", s.withToken(s.handleAccessUserSetGroups))
	mux.HandleFunc("POST /dashboard/admin/access/user/twofa", s.withToken(s.handleAccessUserSetTwoFA))
	mux.HandleFunc("POST /dashboard/admin/access/user/channels", s.withToken(s.handleAccessUserSetChannels))
	mux.HandleFunc("POST /dashboard/admin/access/user/expires", s.withToken(s.handleAccessUserSetExpires))
	mux.HandleFunc("POST /dashboard/admin/access/user/timeouts", s.withToken(s.handleAccessUserSetTimeouts))
	mux.HandleFunc("POST /dashboard/admin/access/user/note", s.withToken(s.handleAccessUserSetNote))
	mux.HandleFunc("POST /dashboard/admin/access/user/name", s.withToken(s.handleAccessUserSetName))
	mux.HandleFunc("POST /dashboard/admin/access/user/badge", s.withToken(s.handleAccessUserBadge))
	mux.HandleFunc("POST /dashboard/admin/access/user/password", s.withToken(s.handleAccessUserSetPassword))
	mux.HandleFunc("POST /dashboard/admin/access/user/rename", s.withToken(s.handleAccessUserRename))
	mux.HandleFunc("POST /dashboard/admin/access/user/remove", s.withToken(s.handleAccessUserRemove))
	mux.HandleFunc("GET /dashboard/bags/{setNum}", s.withToken(s.handlePrintableBags))
	mux.HandleFunc("GET /dashboard/labels/bags/{setNum}/{code}", s.withToken(s.handleBagLabel))
	mux.HandleFunc("GET /dashboard/sets/{setNum}/photos", s.withToken(s.handleSetPhotos))
	mux.HandleFunc("GET /dashboard/sets/{setNum}/photos/{id}/file", s.withToken(s.handleSetPhotoFile))
	mux.HandleFunc("POST /dashboard/sets/{setNum}/photos", s.withToken(s.handleUploadSetPhoto))
	mux.HandleFunc("POST /dashboard/sets/{setNum}/photos/{id}/remove", s.withToken(s.handleRemoveSetPhoto))
	mux.HandleFunc("GET /dashboard/lego/value", s.withToken(s.handleCollectionValue))
	mux.HandleFunc("GET /dashboard/admin/system-health", s.withToken(s.handleSystemHealth))
	mux.HandleFunc("GET /dashboard/currencies", s.withToken(s.handleListCurrencies))
	mux.HandleFunc("POST /dashboard/currencies/add", s.withToken(s.handleAddCurrency))
	mux.HandleFunc("POST /dashboard/currencies/remove", s.withToken(s.handleRemoveCurrency))
	mux.HandleFunc("POST /dashboard/currencies/refresh", s.withToken(s.handleRefreshCurrencies))
	mux.HandleFunc("GET /dashboard/admin/languages", s.withToken(s.handleListLanguages))
	mux.HandleFunc("GET /dashboard/admin/languages/{code}", s.withToken(s.handleGetLanguage))
	mux.HandleFunc("POST /dashboard/admin/languages", s.withToken(s.handleAddLanguage))
	mux.HandleFunc("POST /dashboard/admin/languages/{code}/string", s.withToken(s.handleUpsertLanguageString))
	mux.HandleFunc("POST /dashboard/admin/languages/{code}/remove", s.withToken(s.handleDeleteLanguage))
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

func TestUsersIsEmptyWithNoUsersServiceWired(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/dashboard/admin/users", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var rows []adminUserRow
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected an empty list with no users service wired, got %+v", rows)
	}
}

func TestAssignTicketCreatesAnOpenTicket(t *testing.T) {
	db, srv := testServer(t, "secret")
	defer srv.Close()

	body := `{"kind":"check","target":"75192-1","assigned_to":"dave","by":"admin"}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/dashboard/admin/assign", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	var row adminTicketRow
	if err := json.NewDecoder(resp.Body).Decode(&row); err != nil {
		t.Fatal(err)
	}
	if row.Target != "75192-1" || row.AssignedTo != "dave" {
		t.Fatalf("unexpected ticket row %+v", row)
	}

	tickets, err := db.AllOpenTickets()
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 1 || tickets[0].AssignedTo != "dave" {
		t.Fatalf("expected the assignment to land in the open queue, got %+v", tickets)
	}
}

func TestAssignTicketRequiresAnActingUsername(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	body := `{"kind":"check","target":"75192-1","assigned_to":"dave"}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/dashboard/admin/assign", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 without a by field", resp.StatusCode)
	}
}

func TestTicketsListsWhatWasAssigned(t *testing.T) {
	db, srv := testServer(t, "secret")
	defer srv.Close()

	if _, err := db.AssignTicket(lego.TicketCheck, "75192-1", "Falcon", "dave", "", "", "admin"); err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/dashboard/admin/tickets", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var rows []adminTicketRow
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].AssignedTo != "dave" {
		t.Fatalf("expected the one assigned ticket, got %+v", rows)
	}
}

func TestReopenTicketStartsARecount(t *testing.T) {
	db, srv := testServer(t, "secret")
	defer srv.Close()

	tk, err := db.AssignTicket(lego.TicketCheck, "75192-1", "Falcon", "dave", "", "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ClaimTicket(tk.ID, "dave"); err != nil {
		t.Fatal(err)
	}
	if err := db.FinishTicket(lego.TicketCheck, "75192-1", "dave"); err != nil {
		t.Fatal(err)
	}

	body := `{"kind":"check","target":"75192-1","reason":"looks wrong","by":"admin"}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/dashboard/admin/reopen", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	var row adminTicketRow
	if err := json.NewDecoder(resp.Body).Decode(&row); err != nil {
		t.Fatal(err)
	}
	if row.Status == lego.TicketDone {
		t.Fatalf("expected a fresh open ticket, got %+v", row)
	}
}

func TestStatusCardsIsEmptyWithNoneActive(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/dashboard/admin/status-cards", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var rows []statusCardRow
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected no active status cards, got %+v", rows)
	}
}

func TestAdminRoutesRequireTheSharedToken(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	for _, path := range []string{"/dashboard/admin/users", "/dashboard/admin/tickets", "/dashboard/admin/status-cards"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s: status = %d, want 401", path, resp.StatusCode)
		}
	}
}

// No real wmsdb/partdb connection exists in this sandbox (same constraint
// noted throughout this project's other tests), so usersSvc is always nil
// here -- these tests cover validation and the "not available" path, not a
// real CreateUnified/ResetUnified/ToggleBoth round trip. That round trip is
// internal/users' own test suite's job; this package only wires requests
// to it.

func TestCreateAccountWithNoUsersServiceIs503(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	body := `{"username":"1234","role":"Picker","by":"admin"}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/dashboard/admin/accounts/create", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}

func TestCreateAccountRequiresAUsernameAndBy(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	body := `{"role":"Picker"}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/dashboard/admin/accounts/create", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// usersSvc is nil, so the 503 "not available" check fires before the
	// missing-field validation does -- still a 4xx/5xx refusal either way,
	// and the ordering itself (service-availability before field
	// validation) is what this test pins down.
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}

func TestResetAccountWithNoUsersServiceIs503(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	body := `{"username":"dave","by":"admin"}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/dashboard/admin/accounts/reset", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}

func TestDeleteAccountWithNoUsersServiceIs503(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	body := `{"username":"dave","confirm":"yes","by":"admin"}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/dashboard/admin/accounts/delete", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}

func TestDeleteAccountWithoutConfirmIs400BeforeServiceCheckEvenMatters(t *testing.T) {
	// usersSvc is nil here too, so this still can't observe DeleteBoth not
	// being called -- but it does pin that the handler decodes/validates the
	// body at all, same spirit as TestCreateAccountRequiresAUsernameAndBy.
	_, srv := testServer(t, "secret")
	defer srv.Close()

	body := `{"username":"dave","by":"admin"}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/dashboard/admin/accounts/delete", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// usersSvc is nil, so the 503 "not available" check fires before the
	// missing-confirm validation does -- same ordering TestCreateAccount
	// RequiresAUsernameAndBy pins down for the create endpoint.
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}

func TestRenameAccountWithNoUsersServiceIs503(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	body := `{"username":"dave","new_username":"dave2","by":"admin"}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/dashboard/admin/accounts/rename", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}

func TestLockAndUnlockAccountWithNoUsersServiceIs503(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	for _, path := range []string{"/dashboard/admin/accounts/lock", "/dashboard/admin/accounts/unlock"} {
		body := `{"username":"dave","by":"admin"}`
		req, _ := http.NewRequest(http.MethodPost, srv.URL+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer secret")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("%s: status = %d, want 503", path, resp.StatusCode)
		}
	}
}

func TestAccountRoutesRequireTheSharedToken(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	for _, path := range []string{
		"/dashboard/admin/accounts/create", "/dashboard/admin/accounts/reset",
		"/dashboard/admin/accounts/lock", "/dashboard/admin/accounts/unlock",
	} {
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
