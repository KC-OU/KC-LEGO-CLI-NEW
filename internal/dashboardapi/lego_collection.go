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

type setElementIDRequest struct {
	PartNum   string `json:"part_num"`
	ColorID   int    `json:"color_id"`
	ElementID string `json:"element_id"` // "" clears an override, falling back to the catalog's own value if any
	By        string `json:"by"`
}

// handleSetElementID is the web dashboard's first access to what `wms lego
// element-id` and the TUI's scrSetElementID screen already do -- a
// checker/admin's own correction for a part+colour's element id (see
// lego.DB.SetElementID), surfaced in the UI via the ElementID field
// enrichElementIDs now adds to every InvItem. Permission (sets.check, same
// as the TUI) is enforced by the Rust gateway before this is ever called,
// same as every other POST under /dashboard/lego/.
func (s *Server) handleSetElementID(w http.ResponseWriter, r *http.Request) {
	var req setElementIDRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	req.PartNum, req.By = strings.TrimSpace(req.PartNum), strings.TrimSpace(req.By)
	if req.PartNum == "" || req.By == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "part_num and by are required"})
		return
	}
	if err := s.legoDB.SetElementID(req.PartNum, req.ColorID, req.ElementID, req.By); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(req.By, "DASHBOARD_ELEMENT_ID_SET", "SUCCESS", req.PartNum)
	writeJSON(w, http.StatusOK, map[string]string{"part_num": req.PartNum, "element_id": s.legoDB.ElementFor(req.PartNum, req.ColorID)})
}

type setImageOverrideRequest struct {
	PartNum  string `json:"part_num"`
	ColorID  int    `json:"color_id"`
	ImageURL string `json:"image_url"` // "" clears an override, falling back to the catalog-derived picture if any
	By       string `json:"by"`
}

// handleSetImageOverride is the web dashboard's direct fix for a missing or
// wrong part photo (see lego.DB.SetPartImageOverride) -- independent of
// SetElementID: fixing the element id only helps when the *correct* element
// id actually has a usable photo, so this covers both failure modes.
// Permission (sets.check) is enforced by the Rust gateway before this is
// ever called, same as handleSetElementID.
func (s *Server) handleSetImageOverride(w http.ResponseWriter, r *http.Request) {
	var req setImageOverrideRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	req.PartNum, req.By = strings.TrimSpace(req.PartNum), strings.TrimSpace(req.By)
	if req.PartNum == "" || req.By == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "part_num and by are required"})
		return
	}
	if err := s.legoDB.SetPartImageOverride(req.PartNum, req.ColorID, req.ImageURL, req.By); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(req.By, "DASHBOARD_IMAGE_OVERRIDE_SET", "SUCCESS", req.PartNum)
	writeJSON(w, http.StatusOK, map[string]string{"part_num": req.PartNum, "image_url": s.legoDB.ImageOverrideFor(req.PartNum, req.ColorID)})
}
