// Part-DB: browse/search, get one part, create-or-update, and adjust/
// receive stock -- mirrors internal/uiapp/partdb_screens.go's calls on
// *partdb.DB (reads) and *partdb.Writer (writes). Nil-guarded the same
// "service unavailable" way usersSvc is: solo-mode installs open no
// Part-DB at all (see cmd/wms/gateway.go).
//
// Neither UpsertPart nor AdjustStock writes to internal/audit themselves
// (confirmed: no audit import anywhere in internal/partdb) -- the TUI logs
// right after each call succeeds, and these handlers do the same via
// s.logAudit.
package dashboardapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/partdb"
)

func partDBUnavailable(w http.ResponseWriter) {
	writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Part-DB is not available in this environment"})
}

func (s *Server) handlePartDBSearch(w http.ResponseWriter, r *http.Request) {
	if s.pdb == nil {
		partDBUnavailable(w)
		return
	}
	parts, err := s.pdb.SearchParts(r.URL.Query().Get("q"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"parts": parts})
}

func (s *Server) handlePartDBGet(w http.ResponseWriter, r *http.Request) {
	if s.pdb == nil {
		partDBUnavailable(w)
		return
	}
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id must be a number"})
		return
	}
	part, err := s.pdb.GetPart(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, part)
}

type upsertPartDBRequest struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	IPN         string  `json:"ipn"`
	MfgPN       string  `json:"mfg_pn"`
	CategoryID  int     `json:"category_id"`
	Qty         float64 `json:"qty"`
	By          string  `json:"by"`
}

// handlePartDBUpsert makes the part with the given IPN exist with exactly
// `qty` in stock -- creating it in Part-DB first if no part with that IPN
// exists yet (Writer.UpsertPart, the same call `cmd/wms/lego.go`'s
// import-parts path and the TUI's "Create New Part" screen both use).
func (s *Server) handlePartDBUpsert(w http.ResponseWriter, r *http.Request) {
	if s.pdbw == nil {
		partDBUnavailable(w)
		return
	}
	var req upsertPartDBRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	req.Name, req.IPN, req.By = strings.TrimSpace(req.Name), strings.TrimSpace(req.IPN), strings.TrimSpace(req.By)
	if req.Name == "" || req.IPN == "" || req.By == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name, ipn and by are required"})
		return
	}
	spec := partdb.PartSpec{Name: req.Name, Description: req.Description, IPN: req.IPN, MfgPN: req.MfgPN, CategoryID: req.CategoryID}
	ctx, cancel := s.pdbw.Ctx()
	defer cancel()
	result, err := s.pdbw.UpsertPart(ctx, spec, req.Qty)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	action := "DASHBOARD_PARTDB_PART_UPDATED"
	if result.Created {
		action = "DASHBOARD_PARTDB_PART_CREATED"
	}
	s.logAudit(req.By, action, "SUCCESS", req.IPN)
	writeJSON(w, http.StatusOK, map[string]any{"part_id": result.PartID, "created": result.Created, "prev_qty": result.PrevQty, "new_qty": result.NewQty})
}

type adjustStockRequest struct {
	Delta float64 `json:"delta"`
	By    string  `json:"by"`
}

// handlePartDBAdjustStock changes stock by `delta` (negative to remove) --
// the same write-gated action ("ADJUST_PARTDB_STOCK") the TUI's screen
// performs.
func (s *Server) handlePartDBAdjustStock(w http.ResponseWriter, r *http.Request) {
	if s.pdbw == nil {
		partDBUnavailable(w)
		return
	}
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id must be a number"})
		return
	}
	var req adjustStockRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	req.By = strings.TrimSpace(req.By)
	if req.By == "" || req.Delta == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "by is required and delta must not be zero"})
		return
	}
	newAmount, _, err := s.pdbw.AdjustStock(id, req.Delta)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(req.By, "DASHBOARD_PARTDB_STOCK_ADJUSTED", "SUCCESS", strconv.Itoa(id)+" delta="+strconv.FormatFloat(req.Delta, 'f', -1, 64))
	writeJSON(w, http.StatusOK, map[string]float64{"new_amount": newAmount})
}
