// Collection-value-over-time and BrickLink system-health, both read-only:
// wraps internal/lego's own CollectionValue (live, from stored prices) and
// Snapshots (one data point per day, already taken automatically -- see
// ensureDailySnapshot) into a single chart-friendly response, plus the same
// numbers `wms bricklink status` already computes.
package dashboardapi

import (
	"net/http"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

type valueSnapshotView struct {
	Day    string  `json:"day"`
	Value  float64 `json:"value"`
	Pieces int     `json:"pieces"`
	Sets   int     `json:"sets"`
	Priced int     `json:"priced"`
}

// handleCollectionValue is GET /dashboard/lego/value?condition=N|U -- the
// live value right now plus the daily history for a chart.
func (s *Server) handleCollectionValue(w http.ResponseWriter, r *http.Request) {
	cond := firstNonEmpty(r.URL.Query().Get("condition"), config.Get(config.BricklinkCondition))
	current, err := s.legoDB.CollectionValue(cond)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	snaps, err := s.legoDB.Snapshots(90)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	history := make([]valueSnapshotView, 0, len(snaps))
	for i := len(snaps) - 1; i >= 0; i-- { // Snapshots is newest-first; a chart reads oldest-first
		sn := snaps[i]
		history = append(history, valueSnapshotView{Day: sn.Day, Value: sn.Value, Pieces: sn.Pieces, Sets: sn.Sets, Priced: sn.Priced})
	}
	writeJSON(w, http.StatusOK, map[string]any{"current": current, "history": history})
}

type systemHealthView struct {
	BrickLinkConfigured bool   `json:"bricklink_configured"`
	BrickLinkCallsToday int    `json:"bricklink_calls_today,omitempty"`
	BrickLinkCallBudget int    `json:"bricklink_call_budget,omitempty"`
	BrickLinkColours    int    `json:"bricklink_colours_mapped,omitempty"`
	BrickOwlConfigured  bool   `json:"brickowl_configured"`
	PartDBConfigured    bool   `json:"partdb_configured"`
	Currency            string `json:"currency,omitempty"`
}

// handleSystemHealth is GET /dashboard/admin/system-health -- a small
// at-a-glance panel, reusing the exact numbers `wms bricklink status`
// already computes rather than inventing new instrumentation.
func (s *Server) handleSystemHealth(w http.ResponseWriter, r *http.Request) {
	v := systemHealthView{PartDBConfigured: s.pdb != nil}
	if s.bl != nil {
		v.BrickLinkConfigured = s.bl.Enabled()
		v.Currency = s.bl.Currency
		v.BrickLinkCallBudget = s.bl.DailyBudget
		v.BrickLinkColours = s.legoDB.BLColorCount()
		if s.legoDB != nil {
			v.BrickLinkCallsToday = s.legoDB.APIUsage("bricklink", time.Now())
		}
	}
	if s.bo != nil {
		v.BrickOwlConfigured = s.bo.Enabled()
	}
	writeJSON(w, http.StatusOK, v)
}
