// Rebrickable live set/part search for the dashboard -- internal/lego.Client
// was already a full, working, rate-limited Rebrickable client, already
// instantiated and passed into this server (see cmd/wms/gateway.go); it was
// just never reachable over HTTP. This is a thin wire-up, same shape as
// bricklink.go's handlers, not a new integration.
package dashboardapi

import "net/http"

func rebrickUnavailable(w http.ResponseWriter) {
	writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Rebrickable is not configured"})
}

// handleRebrickableSets is GET /dashboard/rebrickable/sets?q=<term>.
func (s *Server) handleRebrickableSets(w http.ResponseWriter, r *http.Request) {
	if s.rebrick == nil || !s.rebrick.Enabled() {
		rebrickUnavailable(w)
		return
	}
	q := r.URL.Query().Get("q")
	if q == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "q is required"})
		return
	}
	sets, err := s.rebrick.SearchSets(r.Context(), q)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, sets)
}

// handleRebrickableParts is GET /dashboard/rebrickable/parts?q=<term>.
func (s *Server) handleRebrickableParts(w http.ResponseWriter, r *http.Request) {
	if s.rebrick == nil || !s.rebrick.Enabled() {
		rebrickUnavailable(w)
		return
	}
	q := r.URL.Query().Get("q")
	if q == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "q is required"})
		return
	}
	parts, err := s.rebrick.SearchParts(r.Context(), q)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, parts)
}
