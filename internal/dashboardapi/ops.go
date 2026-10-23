// Operations: inventory lookup, master data (SPUs/suppliers/customers),
// and receiving stock -- mirrors internal/uiapp/inventory.go, masterdata.go
// and asn.go over *wmsdb.Client. ReceiveStock is also the TUI's only
// "Warehouse Ops stock adjust" mechanism (there's no separate adjust-only
// call) -- receiving IS how stock increases here, distinct from Part-DB's
// own AdjustStock in partdb.go.
//
// Scope cut: Outbound beyond its one list view, and ASN beyond receiving a
// single identifier, stay CLI/TUI-only -- internal/uiapp's own outbound.go
// is a 22-line list screen with no write action either.
package dashboardapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

func wmsUnavailable(w http.ResponseWriter) {
	writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "warehouse operations are not available in this environment"})
}

func (s *Server) handleInventoryLookup(w http.ResponseWriter, r *http.Request) {
	if s.wms == nil {
		wmsUnavailable(w)
		return
	}
	rows, err := s.wms.StockLookup(r.Context(), r.URL.Query().Get("q"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rows": rows})
}

func (s *Server) handleMasterDataSPUs(w http.ResponseWriter, r *http.Request) {
	if s.wms == nil {
		wmsUnavailable(w)
		return
	}
	rows, err := s.wms.MasterDataSPUs(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rows": rows})
}

func (s *Server) handleMasterDataSuppliers(w http.ResponseWriter, r *http.Request) {
	if s.wms == nil {
		wmsUnavailable(w)
		return
	}
	rows, err := s.wms.MasterDataSuppliers(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rows": rows})
}

func (s *Server) handleMasterDataCustomers(w http.ResponseWriter, r *http.Request) {
	if s.wms == nil {
		wmsUnavailable(w)
		return
	}
	rows, err := s.wms.MasterDataCustomers(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rows": rows})
}

func (s *Server) handleDispatchList(w http.ResponseWriter, r *http.Request) {
	if s.wms == nil {
		wmsUnavailable(w)
		return
	}
	rows, err := s.wms.DispatchList(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rows": rows})
}

type receiveStockRequest struct {
	Identifier string `json:"identifier"`
	Qty        int    `json:"qty"`
	By         string `json:"by"`
}

// handleReceiveStock is both "Inbound ASN" and "Warehouse Ops stock
// adjust" from the TUI -- `identifier` is an ASN number or SPU code
// (ReceiveStock resolves either), `qty` is added to whatever's on hand.
func (s *Server) handleReceiveStock(w http.ResponseWriter, r *http.Request) {
	if s.wms == nil {
		wmsUnavailable(w)
		return
	}
	var req receiveStockRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	req.Identifier, req.By = strings.TrimSpace(req.Identifier), strings.TrimSpace(req.By)
	if req.Identifier == "" || req.By == "" || req.Qty <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "identifier and by are required and qty must be positive"})
		return
	}
	result, err := s.wms.ReceiveStock(r.Context(), req.Identifier, req.Qty, req.By)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(req.By, "DASHBOARD_STOCK_RECEIVED", "SUCCESS", req.Identifier+" qty="+strconv.Itoa(req.Qty))
	writeJSON(w, http.StatusOK, result)
}
