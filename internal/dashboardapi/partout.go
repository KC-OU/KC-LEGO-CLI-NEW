package dashboardapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/bricklink"
)

// handlePartOut is GET /dashboard/lego/partout?set=75192-1&cond=U&fee=3&labour=10
// -- what the set is worth as loose parts (stored prices only; costs no
// BrickLink calls) and the most worth paying for it. fee/labour are percentages
// of the gross, defaulting to 3 (marketplace + payment fees) and 10.
func (s *Server) handlePartOut(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	set := strings.TrimSpace(q.Get("set"))
	if set == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "set is required"})
		return
	}
	pct := func(name string, def float64) float64 {
		if v, err := strconv.ParseFloat(q.Get(name), 64); err == nil && v >= 0 && v < 100 {
			return v
		}
		return def
	}
	out, err := s.legoDB.PartOutValue(set, strings.ToUpper(q.Get("cond")), pct("fee", 3), pct("labour", 10))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// blPricer adapts the BrickLink client to lego.PriceFetcher. It mirrors
// cmd/wms/bricklink.go's blFetcher (this package does not import cmd/wms).
type blPricer struct{ c *bricklink.Client }

func (f blPricer) AvgPrice(ctx context.Context, itemType, no string, blColor int, cond string) (float64, string, bool, error) {
	pr, err := f.c.PriceGuide(ctx, bricklink.ItemType(itemType), no, blColor, bricklink.Sold, cond)
	switch {
	case errors.Is(err, bricklink.ErrNotFound):
		return 0, "", false, nil
	case err != nil:
		return 0, "", false, err
	case pr.TotalQuantity == 0 || pr.Avg == 0:
		return 0, pr.CurrencyCode, false, nil
	}
	return float64(pr.Avg), pr.CurrencyCode, true, nil
}

// handlePartOutFetch is POST /dashboard/lego/partout/fetch?set=&cond=&limit=
// -- spends up to limit (default 40, max 100) BrickLink calls pricing this
// set's biggest-quantity lines that have no stored price. The client's own
// daily budget still applies; hitting it is reported, not retried.
func (s *Server) handlePartOutFetch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	set := strings.TrimSpace(q.Get("set"))
	if set == "" || s.bl == nil || !s.bl.Enabled() {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "set is required and BrickLink must be configured"})
		return
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 {
		limit = 40
	}
	limit = min(limit, 100)
	cond := "U"
	if strings.EqualFold(q.Get("cond"), "N") {
		cond = "N"
	}
	fetched, unknown, err := s.legoDB.RefreshSetPrices(r.Context(), blPricer{s.bl}, set, cond, limit)
	resp := map[string]any{"fetched": fetched, "unknown": unknown}
	if err != nil {
		resp["error"] = err.Error() // partial progress is kept; the caller shows both
	}
	writeJSON(w, http.StatusOK, resp)
}
