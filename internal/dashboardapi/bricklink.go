// BrickLink (and, for parts, BrickOwl) live lookups for the dashboard --
// read-only, no writes to either marketplace. This is a straightforward
// wire-up of internal/bricklink.Client, which was already fully working and
// configured before this file existed; nothing here is a bugfix.
package dashboardapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/bricklink"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

func blUnavailable(w http.ResponseWriter) {
	writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "BrickLink is not configured"})
}

// parseBLItemType mirrors cmd/wms/bricklink.go's parseItemType -- duplicated
// rather than exported across the cmd/internal boundary for one switch.
func parseBLItemType(s string) (bricklink.ItemType, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "part", "p":
		return bricklink.Part, true
	case "set", "s":
		return bricklink.Set, true
	case "minifig", "minifigure", "m":
		return bricklink.Minifig, true
	}
	return "", false
}

// resolveBLColor mirrors cmd/wms/bricklink.go's blColorFlag: color is a
// Rebrickable colour name, blColor (if > 0) is a raw BrickLink colour
// number and wins outright.
func resolveBLColor(db *lego.DB, color string, blColor int) (int, string) {
	if blColor > 0 {
		return blColor, ""
	}
	color = strings.TrimSpace(color)
	if color == "" {
		return 0, ""
	}
	cols, _ := db.CatalogColors()
	c, ok := lego.MatchColor(cols, color)
	if !ok {
		return 0, "no colour \"" + color + "\" in the catalog"
	}
	if id, ok := db.BLColorFor(c.ID); ok {
		return id, ""
	}
	return 0, "no BrickLink number is known for " + c.Name + " yet -- run the colour sync first"
}

// handleBLItem is GET /dashboard/bricklink/item/{type}/{number}.
func (s *Server) handleBLItem(w http.ResponseWriter, r *http.Request) {
	if s.bl == nil {
		blUnavailable(w)
		return
	}
	t, ok := parseBLItemType(r.PathValue("type"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "type must be part, set or minifig"})
		return
	}
	item, err := s.bl.GetItem(r.Context(), t, r.PathValue("number"))
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, item)
}

type blPriceResponse struct {
	BrickLink    *bricklink.Price `json:"bricklink"`
	BrickOwl     *boPriceView     `json:"brickowl,omitempty"`
	ColorWarning string           `json:"color_warning,omitempty"`
}

type boPriceView struct {
	BOID     string  `json:"boid"`
	Low      float64 `json:"low"`
	Avg      float64 `json:"avg"`
	Currency string  `json:"currency"`
	Listings int     `json:"listings"`
	Qty      int     `json:"qty"`
}

// handleBLPrice is GET /dashboard/bricklink/price/{type}/{number}, with
// optional ?color=<name>&bl_color=<number>&condition=used|new&stock=1.
// For a part, also attaches a BrickOwl price when one can be resolved
// (element id known + BrickOwl configured) -- mirrors what the CLI's
// `shopping` list already shows side by side, best-effort, never fatal.
func (s *Server) handleBLPrice(w http.ResponseWriter, r *http.Request) {
	if s.bl == nil {
		blUnavailable(w)
		return
	}
	t, ok := parseBLItemType(r.PathValue("type"))
	if !ok || t == bricklink.Minifig {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "type must be part or set"})
		return
	}
	number := r.PathValue("number")
	q := r.URL.Query()
	blColor, _ := strconv.Atoi(q.Get("bl_color"))
	colorID, colorWarning := resolveBLColor(s.legoDB, q.Get("color"), blColor)
	guide := bricklink.Sold
	if q.Get("stock") == "1" {
		guide = bricklink.Stock
	}
	condition := strings.ToUpper(q.Get("condition"))
	price, err := s.bl.PriceGuide(r.Context(), t, number, colorID, guide, condition)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	resp := blPriceResponse{BrickLink: price, ColorWarning: colorWarning}
	if t == bricklink.Part && s.bo != nil && s.bo.Enabled() && colorID > 0 {
		if rbColor, ok := s.legoDB.RBColorFor(colorID); ok {
			if elementID := s.legoDB.ElementFor(number, rbColor.ID); elementID != "" {
				if boid, err := s.bo.Lookup(r.Context(), elementID, ""); err == nil && boid != "" {
					if p, err := s.bo.Availability(r.Context(), boid); err == nil && p != nil {
						resp.BrickOwl = &boPriceView{BOID: p.BOID, Low: p.Low, Avg: p.Avg, Currency: p.Currency, Listings: p.Listings, Qty: p.Qty}
					}
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleBLWhereUsed is GET /dashboard/bricklink/where-used/{number}, with
// optional ?color=<name>&bl_color=<number>.
func (s *Server) handleBLWhereUsed(w http.ResponseWriter, r *http.Request) {
	if s.bl == nil {
		blUnavailable(w)
		return
	}
	q := r.URL.Query()
	blColor, _ := strconv.Atoi(q.Get("bl_color"))
	colorID, colorWarning := resolveBLColor(s.legoDB, q.Get("color"), blColor)
	supersets, err := s.bl.Supersets(r.Context(), bricklink.Part, r.PathValue("number"), colorID)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"supersets": supersets, "color_warning": colorWarning})
}

type blColorsSyncRequest struct {
	By string `json:"by"`
}

// handleBLColorsSync is POST /dashboard/admin/bricklink/colors/sync -- a
// dashboard button for the same one-time mapping `wms bricklink colors
// sync` does, deliberately manual (not scheduled) per the interview.
func (s *Server) handleBLColorsSync(w http.ResponseWriter, r *http.Request) {
	if s.bl == nil {
		blUnavailable(w)
		return
	}
	var req blColorsSyncRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	req.By = strings.TrimSpace(req.By)
	if req.By == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "by is required"})
		return
	}
	cols, err := s.bl.Colors(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	names := map[int]string{}
	for _, c := range cols {
		names[c.ColorID] = c.ColorName
	}
	rows, unmatched, err := s.legoDB.MatchBLColorsByName(names)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := s.legoDB.SetBLColors(rows); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(req.By, "DASHBOARD_BL_COLORS_SYNCED", "SUCCESS", strconv.Itoa(len(rows))+" mapped")
	writeJSON(w, http.StatusOK, map[string]any{"mapped": len(rows), "unmatched": unmatched})
}
