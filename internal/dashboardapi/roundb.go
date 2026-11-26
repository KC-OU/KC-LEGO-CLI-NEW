// Round B (selling side): BrickLink sales orders and their pick lists, price
// movers, store repricing suggestions, and acquisitions. Thin handlers over
// internal/lego; the BrickLink calls go through the shared budgeted client.
package dashboardapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/bricklink"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

// BLSalesFeed adapts the BrickLink client to lego.SalesFeed.
type BLSalesFeed struct{ c *bricklink.Client }

// NewBLSalesFeed is for the gateway's background sync.
func NewBLSalesFeed(c *bricklink.Client) BLSalesFeed { return BLSalesFeed{c} }

func (f BLSalesFeed) ListOrders(ctx context.Context) ([]lego.SalesOrder, error) {
	orders, err := f.c.SellerOrders(ctx, "")
	if err != nil {
		return nil, err
	}
	out := make([]lego.SalesOrder, 0, len(orders))
	for _, o := range orders {
		out = append(out, lego.SalesOrder{OrderID: strconv.Itoa(o.OrderID), Buyer: o.BuyerName, Status: o.Status,
			OrderedAt: o.DateOrdered, Total: float64(o.Cost.GrandTotal), Currency: o.Cost.CurrencyCode, Pieces: o.TotalCount})
	}
	return out, nil
}

func (f BLSalesFeed) OrderLines(ctx context.Context, id string) ([]lego.SalesLine, error) {
	n, err := strconv.Atoi(id)
	if err != nil {
		return nil, err
	}
	items, err := f.c.OrderItems(ctx, n)
	if err != nil {
		return nil, err
	}
	out := make([]lego.SalesLine, 0, len(items))
	for _, it := range items {
		out = append(out, lego.SalesLine{ItemType: strings.ToUpper(it.Item.Type), ItemNo: it.Item.No, Name: it.Item.Name,
			BLColor: it.ColorID, ColorName: it.ColorName, Cond: it.NewOrUsed, Qty: it.Quantity, UnitPrice: float64(it.UnitPrice)})
	}
	return out, nil
}

func (s *Server) needBL(w http.ResponseWriter) bool {
	if s.bl == nil || !s.bl.Enabled() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": bricklink.ErrNotConfigured.Error()})
		return false
	}
	return true
}

// POST /dashboard/sales/sync -- import new BrickLink store orders.
func (s *Server) handleSalesSync(w http.ResponseWriter, r *http.Request) {
	if !s.needBL(w) {
		return
	}
	n, err := s.legoDB.SyncSalesOrders(r.Context(), BLSalesFeed{s.bl})
	resp := map[string]any{"added": n}
	if err != nil {
		resp["error"] = err.Error()
	}
	writeJSON(w, http.StatusOK, resp)
}

// GET /dashboard/sales?open=1
func (s *Server) handleSalesList(w http.ResponseWriter, r *http.Request) {
	orders, err := s.legoDB.ListSalesOrders(r.URL.Query().Get("open") != "0")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, orders)
}

// GET /dashboard/sales/{id} -- the order with lines in shelf order.
func (s *Server) handleSalesPickList(w http.ResponseWriter, r *http.Request) {
	o, err := s.legoDB.SalesPickList(s.pdb, r.PathValue("id"))
	switch {
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
	case o == nil:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such order"})
	default:
		writeJSON(w, http.StatusOK, o)
	}
}

// POST /dashboard/sales/{id}/picked {"line":1,"picked":3,"by":"alice"}
func (s *Server) handleSalesPicked(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Line, Picked int
		By           string
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || req.Line <= 0 || strings.TrimSpace(req.By) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "line (>0) and by are required"})
		return
	}
	id := r.PathValue("id")
	if err := s.legoDB.SetSalesLinePicked(id, req.Line, req.Picked); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(req.By, "DASHBOARD_SALES_PICKED", "SUCCESS", id+" line "+strconv.Itoa(req.Line))
	writeJSON(w, http.StatusOK, map[string]string{"order": id})
}

func qFloat(r *http.Request, name string, def float64) float64 {
	if v, err := strconv.ParseFloat(r.URL.Query().Get(name), 64); err == nil && v >= 0 {
		return v
	}
	return def
}

// GET /dashboard/lego/movers?days=7&pct=20&value=2&cond=U
func (s *Server) handleMovers(w http.ResponseWriter, r *http.Request) {
	cond := "U"
	if strings.EqualFold(r.URL.Query().Get("cond"), "N") {
		cond = "N"
	}
	out, err := s.legoDB.PriceMovers(cond, int(qFloat(r, "days", 7)), qFloat(r, "pct", 20), qFloat(r, "value", 2))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /dashboard/store/sync -- copy the store's lots down (one BrickLink call).
func (s *Server) handleStoreSync(w http.ResponseWriter, r *http.Request) {
	if !s.needBL(w) {
		return
	}
	lots, err := s.bl.StoreInventory(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	in := make([]lego.StoreLotIn, 0, len(lots))
	for _, l := range lots {
		in = append(in, lego.StoreLotIn{InventoryID: l.InventoryID, ItemType: strings.ToUpper(l.Item.Type), ItemNo: l.Item.No,
			BLColor: l.ColorID, Qty: l.Quantity, Cond: l.NewOrUsed, Description: l.Description, UnitPrice: float64(l.UnitPrice)})
	}
	if err := s.legoDB.ReplaceStoreLots(in); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"lots": len(in)})
}

// GET /dashboard/store/reprice?market=100&tolerance=10 -- suggestions only; nothing is changed.
func (s *Server) handleReprice(w http.ResponseWriter, r *http.Request) {
	out, err := s.legoDB.RepriceSuggestions(qFloat(r, "market", 100), qFloat(r, "tolerance", 10))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// GET /dashboard/acquisitions?stage=  /  POST /dashboard/acquisitions  /  POST .../{id}/advance
func (s *Server) handleAcquisitionsList(w http.ResponseWriter, r *http.Request) {
	out, err := s.legoDB.ListAcquisitions(r.URL.Query().Get("stage"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleAcquisitionAdd(w http.ResponseWriter, r *http.Request) {
	var a lego.Acquisition
	var req struct {
		Label, Supplier, SetNum, Currency, Note, By string
		Cost                                        float64
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || strings.TrimSpace(req.By) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "label and by are required"})
		return
	}
	a = lego.Acquisition{Label: strings.TrimSpace(req.Label), Supplier: req.Supplier, SetNum: req.SetNum, Cost: req.Cost, Currency: req.Currency, Note: req.Note, CreatedBy: req.By}
	id, err := s.legoDB.AddAcquisition(a)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(req.By, "DASHBOARD_ACQUISITION_ADD", "SUCCESS", a.Label)
	writeJSON(w, http.StatusOK, map[string]int64{"id": id})
}

func (s *Server) handleAcquisitionAdvance(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct{ By string }
	if json.NewDecoder(r.Body).Decode(&req) != nil || strings.TrimSpace(req.By) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "by is required"})
		return
	}
	stage, err := s.legoDB.AdvanceAcquisition(id)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(req.By, "DASHBOARD_ACQUISITION_ADVANCE", "SUCCESS", strconv.FormatInt(id, 10)+" -> "+stage)
	writeJSON(w, http.StatusOK, map[string]string{"stage": stage})
}

// GET /dashboard/borrowed  /  POST /dashboard/borrowed  /  POST .../{id}/advance -- parts borrowed from one set to
// finish building another, until the replacement is ordered from LEGO and put back.
func (s *Server) handleBorrowedList(w http.ResponseWriter, r *http.Request) {
	out, err := s.legoDB.ListBorrowedParts()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleBorrowedAdd(w http.ResponseWriter, r *http.Request) {
	var req struct {
		lego.BorrowedPart
		By string
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || strings.TrimSpace(req.By) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "set, part and by are required"})
		return
	}
	req.CreatedBy = req.By
	id, err := s.legoDB.AddBorrowedPart(req.BorrowedPart)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(req.By, "DASHBOARD_BORROWED_PART_ADD", "SUCCESS", req.BuildSetNum+" "+req.NeedPartNum+" from "+req.FromSetNum)
	writeJSON(w, http.StatusOK, map[string]int64{"id": id})
}

func (s *Server) handleBorrowedAdvance(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct{ By string }
	if json.NewDecoder(r.Body).Decode(&req) != nil || strings.TrimSpace(req.By) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "by is required"})
		return
	}
	stage, err := s.legoDB.AdvanceBorrowedPart(id)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(req.By, "DASHBOARD_BORROWED_PART_ADVANCE", "SUCCESS", strconv.FormatInt(id, 10)+" -> "+stage)
	writeJSON(w, http.StatusOK, map[string]string{"status": stage})
}
