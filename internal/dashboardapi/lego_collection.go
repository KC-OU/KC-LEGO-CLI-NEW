// Add/update an owned set or owned part -- the one piece of collection
// management (beyond read-only Sets/Catalog) the dashboard was missing.
// Mirrors internal/uiapp/lego_add.go and part_flow.go's UpsertSet/SetInfo
// and AddOwnedPart calls exactly.
package dashboardapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

type upsertSetRequest struct {
	SetNum    string `json:"set_num"`
	Name      string `json:"name"`
	Theme     string `json:"theme"`
	Year      int    `json:"year"`
	PartsQty  int    `json:"parts_qty"`
	Qty       int    `json:"qty"`
	Location  string `json:"location"`
	Condition string `json:"condition"`
	Note      string `json:"note"`
	By        string `json:"by"`
}

// handleUpsertSet creates a new owned set or updates an existing one's
// name/theme/year/parts count/owned quantity (UpsertSet, keyed on set_num),
// then location/condition/note if given (SetInfo). Missing-qty has no
// direct setter anywhere in internal/lego -- it's only ever derived by
// finishing a stock check -- so it isn't accepted here.
func (s *Server) handleUpsertSet(w http.ResponseWriter, r *http.Request) {
	var req upsertSetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	req.SetNum, req.By = strings.TrimSpace(req.SetNum), strings.TrimSpace(req.By)
	if req.SetNum == "" || req.By == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "set_num and by are required"})
		return
	}
	existing, _ := s.legoDB.GetSetByNum(req.SetNum)
	set := lego.Set{SetNum: req.SetNum, Name: req.Name, Theme: req.Theme, Year: req.Year, PartsQty: req.PartsQty, Qty: req.Qty}
	if err := s.legoDB.UpsertSet(set); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.Location != "" || req.Condition != "" || req.Note != "" {
		if err := s.legoDB.SetInfo(req.SetNum, req.Location, req.Condition, req.Note); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	}
	action := "DASHBOARD_SET_UPDATED"
	if existing == nil {
		action = "DASHBOARD_SET_ADDED"
	}
	s.logAudit(req.By, action, "SUCCESS", req.SetNum)
	writeJSON(w, http.StatusOK, map[string]string{"set_num": req.SetNum})
}

type upsertPartRequest struct {
	PartNum   string `json:"part_num"`
	Name      string `json:"name"`
	Category  string `json:"category"`
	ColorID   int    `json:"color_id"`
	ColorName string `json:"color_name"`
	Qty       int    `json:"qty"`
	MinQty    int    `json:"min_qty"`
	By        string `json:"by"`
}

// handleUpsertPart sets the *total* quantity held of a loose part/colour
// (AddOwnedPart matches on part_num+colour, last-write-wins on Qty -- not
// an increment, same as the TUI).
func (s *Server) handleUpsertPart(w http.ResponseWriter, r *http.Request) {
	var req upsertPartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	req.PartNum, req.By = strings.TrimSpace(req.PartNum), strings.TrimSpace(req.By)
	if req.PartNum == "" || req.By == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "part_num and by are required"})
		return
	}
	part := lego.OwnedPart{
		PartNum: req.PartNum, Name: req.Name, Category: req.Category,
		ColorID: req.ColorID, ColorName: req.ColorName, Qty: req.Qty, MinQty: req.MinQty,
	}
	if err := s.legoDB.AddOwnedPart(part); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(req.By, "DASHBOARD_PART_UPSERTED", "SUCCESS", req.PartNum)
	writeJSON(w, http.StatusOK, map[string]string{"part_num": req.PartNum})
}
