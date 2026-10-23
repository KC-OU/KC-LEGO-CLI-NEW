// Parts Orders workflow: list/get/create an order, add a line to it,
// advance its status (which is also how a line gets marked received --
// there's no separate per-line receive call), auto-build one from a set's
// shortages, and a spend report. Mirrors cmd/wms/lego_sets.go's `orders`
// subcommands over the same internal/lego/orders.go functions.
package dashboardapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

func (s *Server) handleListOrders(w http.ResponseWriter, r *http.Request) {
	orders, err := s.legoDB.ListOrders(r.URL.Query().Get("status"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"orders": orders})
}

func (s *Server) handleGetOrder(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id must be a number"})
		return
	}
	order, err := s.legoDB.GetOrder(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, order)
}

type createOrderRequest struct {
	SupplierKind string  `json:"supplier_kind"`
	Supplier     string  `json:"supplier"`
	OrderNo      string  `json:"order_no"`
	Currency     string  `json:"currency"`
	Shipping     float64 `json:"shipping"`
	Note         string  `json:"note"`
	By           string  `json:"by"`
}

// handleCreateOrder creates an order header (no lines yet -- add those via
// handleAddOrderLine). Status starts "wanted", same as SaveOrder's default
// for a zero-value Order.
func (s *Server) handleCreateOrder(w http.ResponseWriter, r *http.Request) {
	var req createOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	req.By = strings.TrimSpace(req.By)
	if req.By == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "by is required"})
		return
	}
	order := &lego.Order{
		SupplierKind: req.SupplierKind, Supplier: req.Supplier, OrderNo: req.OrderNo,
		Currency: req.Currency, Shipping: req.Shipping, Note: req.Note, CreatedBy: req.By,
	}
	if err := s.legoDB.SaveOrder(order); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(req.By, "DASHBOARD_ORDER_CREATED", "SUCCESS", strconv.FormatInt(order.ID, 10))
	writeJSON(w, http.StatusOK, order)
}

type createOrderFromMissingRequest struct {
	SetNum       string  `json:"set_num"`
	SupplierKind string  `json:"supplier_kind"`
	Supplier     string  `json:"supplier"`
	OrderNo      string  `json:"order_no"`
	Currency     string  `json:"currency"`
	Shipping     float64 `json:"shipping"`
	Note         string  `json:"note"`
	By           string  `json:"by"`
}

// handleCreateOrderFromMissing starts an order and fills it with a line for
// every part setNum is still short, prices left at 0 -- the dashboard form
// has no per-part pricing shape, same as the CLI's own default (a nil price
// func), so this always passes nil rather than a flat per-line guess.
func (s *Server) handleCreateOrderFromMissing(w http.ResponseWriter, r *http.Request) {
	var req createOrderFromMissingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	req.SetNum, req.By = strings.TrimSpace(req.SetNum), strings.TrimSpace(req.By)
	if req.SetNum == "" || req.By == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "set_num and by are required"})
		return
	}
	order := &lego.Order{
		SupplierKind: req.SupplierKind, Supplier: req.Supplier, OrderNo: req.OrderNo,
		Currency: req.Currency, Shipping: req.Shipping, Note: req.Note, CreatedBy: req.By,
	}
	if err := s.legoDB.OrderFromMissing(req.SetNum, order, nil); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(req.By, "DASHBOARD_ORDER_FROM_MISSING", "SUCCESS", req.SetNum+" -> order #"+strconv.FormatInt(order.ID, 10))
	writeJSON(w, http.StatusOK, order)
}

type addOrderLineRequest struct {
	SetNum    string  `json:"set_num"`
	PartNum   string  `json:"part_num"`
	ColorID   int     `json:"color_id"`
	ColorName string  `json:"color_name"`
	PartName  string  `json:"part_name"`
	Qty       int     `json:"qty"`
	UnitPrice float64 `json:"unit_price"`
	By        string  `json:"by"`
}

func (s *Server) handleAddOrderLine(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id must be a number"})
		return
	}
	var req addOrderLineRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	req.By = strings.TrimSpace(req.By)
	if req.By == "" || req.Qty <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "by is required and qty must be positive"})
		return
	}
	line := lego.OrderLine{
		SetNum: req.SetNum, PartNum: req.PartNum, ColorID: req.ColorID,
		ColorName: req.ColorName, PartName: req.PartName, Qty: req.Qty, UnitPrice: req.UnitPrice,
	}
	if err := s.legoDB.AddOrderLine(id, line); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(req.By, "DASHBOARD_ORDER_LINE_ADDED", "SUCCESS", strconv.FormatInt(id, 10))
	writeJSON(w, http.StatusOK, map[string]bool{"added": true})
}

type setOrderStatusRequest struct {
	Status string `json:"status"`
	By     string `json:"by"`
}

// handleSetOrderStatus advances an order through wanted -> ordered ->
// shipped -> received (or cancelled). Moving to "received" is also how
// its lines get marked received and may complete owned sets -- the
// response's `completed` field names which ones did.
func (s *Server) handleSetOrderStatus(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id must be a number"})
		return
	}
	var req setOrderStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	req.Status, req.By = strings.TrimSpace(req.Status), strings.TrimSpace(req.By)
	if req.Status == "" || req.By == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "status and by are required"})
		return
	}
	completed, err := s.legoDB.SetOrderStatus(id, req.Status)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(req.By, "DASHBOARD_ORDER_STATUS_SET", "SUCCESS", strconv.FormatInt(id, 10)+" -> "+req.Status)
	writeJSON(w, http.StatusOK, map[string]any{"status": req.Status, "completed": completed})
}

// handleSpendReport is `wms orders spend` -- ?by=set|supplier|month
// (default "month").
func (s *Server) handleSpendReport(w http.ResponseWriter, r *http.Request) {
	by := r.URL.Query().Get("by")
	if by == "" {
		by = "month"
	}
	rows, err := s.legoDB.Spend(by)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rows": rows})
}
