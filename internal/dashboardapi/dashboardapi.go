// Package dashboardapi is the new Rust-based KC-Parts web dashboard's only
// way to reach this project's data: a small, additive JSON API over the
// same business logic the TUI, telnet/web gateway, and mobile app already
// use (internal/lego), mirroring internal/mobileapi's dependency-injection
// shape rather than re-implementing anything.
//
// End-user identity is BetterAuth's job, handled entirely in the Rust
// service — this package never sees an individual dashboard user, only the
// Rust service itself, trusted via one shared bearer token
// (config.DashboardAPIToken). That is a service-to-service trust boundary,
// not a replacement for per-user permissions: endpoints here return the
// same admin-level view the TUI's own admin screens show, and per-user
// scoping (if the dashboard ever needs a non-admin view) belongs in a
// later endpoint, not retrofitted onto this token.
package dashboardapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/audit"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/partdb"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/users"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/wmsdb"
)

type Server struct {
	legoDB *lego.DB
	// usersSvc backs the admin "assign to" user list (handleUsers) and the
	// account-management endpoints -- nil in tests that only exercise
	// Kanban/Sets/accuracy, same "nil disables" convention as token below.
	usersSvc *users.Service
	// audit is the same tamper-evident, hash-chained log every other
	// surface (TUI, telnet, mobile) writes admin actions to -- nil in tests
	// that don't care about it; every write handler below nil-checks it the
	// same way internal/mobileapi's handlers nil-check their own audit field.
	audit *audit.Logger
	// token is the shared secret the Rust service presents. Empty disables
	// the check entirely -- only ever intentional in tests that build a
	// Server literal directly, the same "nil/empty disables" convention
	// internal/mobileapi's throttle field already uses.
	token string
	// rebrick backs the catalog-only set lookup (handleCatalogSetLookup) --
	// nil is fine, LookupSet/LookupSetInventory both already tolerate a nil
	// *lego.Client and fall back to the offline catalog alone.
	rebrick *lego.Client
	// pdb/pdbw back the Part-DB browse/create/adjust-stock endpoints -- nil
	// disables them the same way usersSvc being nil disables account
	// management (solo mode opens no Part-DB at all; see gateway.go).
	pdb  *partdb.DB
	pdbw *partdb.Writer
	// wms backs the Operations endpoints (inventory lookup, master data,
	// receive/ASN, dispatch) -- never nil in practice (wmsdb.NewClient()
	// doesn't itself open a connection), kept as a field for parity with the
	// other injected services and so tests can swap it.
	wms *wmsdb.Client
}

func NewServer(legoDB *lego.DB, usersSvc *users.Service, auditLog *audit.Logger, token string, rebrick *lego.Client, pdb *partdb.DB, wms *wmsdb.Client) *Server {
	var pdbw *partdb.Writer
	if pdb != nil {
		pdbw = partdb.NewWriter(pdb)
	}
	return &Server{legoDB: legoDB, usersSvc: usersSvc, audit: auditLog, token: token, rebrick: rebrick, pdb: pdb, pdbw: pdbw, wms: wms}
}

func (s *Server) logAudit(user, action, status, details string) {
	if s.audit == nil {
		return
	}
	_ = s.audit.Log(user, "dashboard-admin", action, status, details)
}

// Serve runs the dashboard API until ctx is cancelled.
func Serve(ctx context.Context, addr string, s *Server) error {
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
	srv := &http.Server{Addr: addr, Handler: mux}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// withToken gates everything except /dashboard/health behind the shared
// secret -- a health check needs to work for ops/monitoring without a
// credential, same as internal/mobileapi leaves /mobile/print's open page
// ungated while its session-bearing siblings are not.
func (s *Server) withToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.token != "" {
			got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) != 1 {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleKanban is the admin board (internal/lego.KanbanBoardAll): the
// shared backlog, every open ticket across every checker, recently
// abandoned and recently finished work -- exactly what the TUI's own admin
// Kanban screen shows, not a new view of the data.
func (s *Server) handleKanban(w http.ResponseWriter, r *http.Request) {
	cards, err := s.legoDB.KanbanBoardAll()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"cards": cards})
}

// handleSets lists the owned collection, optionally filtered by ?q= (name,
// theme or set number) -- the exact same lego.DB.SearchSets an empty-term
// call already uses for "show me everything" (bounded to 200 rows, a
// personal collection, not a paginated catalog).
func (s *Server) handleSets(w http.ResponseWriter, r *http.Request) {
	sets, err := s.legoDB.SearchSets(r.URL.Query().Get("q"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sets": sets})
}

// handleSetDetail is one set's full picture for the dashboard's Sets
// section: the owned-collection record, its check/location/missing-qty
// state, and its parts list from the offline catalog (no network call --
// CatalogSetInventory, not the live Rebrickable client).
func (s *Server) handleSetDetail(w http.ResponseWriter, r *http.Request) {
	setNum := r.PathValue("setNum")
	set, err := s.legoDB.GetSetByNum(setNum)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	inventory, err := s.legoDB.CatalogSetInventory(setNum)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	// CatalogSet is the one place an admin's image override (SetImageOverride)
	// is applied -- the same picture the TUI and every other reader see, so
	// the dashboard shows it here rather than reading cat_sets.img_url itself.
	var imageURL string
	if hit, _ := s.legoDB.CatalogSet(setNum); hit != nil {
		imageURL = hit.ImgURL
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"set":       set,
		"state":     s.legoDB.GetSetState(setNum),
		"inventory": inventory,
		"image_url": imageURL,
	})
}

// handleAccuracy is the same "My Accuracy" picture the TUI's own palette
// entry shows (today's %, the 7-day trend, today's event log) -- for
// whichever Go username the caller names via ?role=picker|checker (default
// checker). There is deliberately no lookup from the signed-in BetterAuth
// dashboard identity to a Go username here: dashboard accounts (email) and
// Go CLI/mobile accounts (a separate username space) aren't linked yet, so
// this is a named-username lookup, not an auto-scoped "my own" view, until
// that identity bridge is designed.
func (s *Server) handleAccuracy(w http.ResponseWriter, r *http.Request) {
	username := r.PathValue("username")
	role := r.URL.Query().Get("role")
	if role == "" {
		role = lego.AccuracyChecker
	}

	today, err := s.legoDB.AccuracyToday(username, role)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	trend, err := s.legoDB.AccuracyTrend(username, role, 7)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	history, err := s.legoDB.AccuracyHistory(username, role, "")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"username": username,
		"role":     role,
		"today":    today,
		"trend":    trend,
		"history":  history,
	})
}
