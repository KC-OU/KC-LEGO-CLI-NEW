// Catalog-wide parts/minifigure search (the "Queries" screen): the same
// offline FTS5 search the TUI's own lookup screens use
// (internal/lego/search.go), not scoped to the owned collection -- that's
// what /dashboard/sets already covers via SearchSets. Thin skins only, no
// new search logic.
package dashboardapi

import "net/http"

// queryLimit caps a single lookup -- generous for a typed search, small
// enough that a one-letter query doesn't return the whole catalog.
const queryLimit = 50

// handleQueryParts is GET /dashboard/queries/parts?q=....
func (s *Server) handleQueryParts(w http.ResponseWriter, r *http.Request) {
	hits, err := s.legoDB.SearchCatalogParts(r.URL.Query().Get("q"), queryLimit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"parts": hits})
}

// handleQueryMinifigs is GET /dashboard/queries/minifigs?q=....
func (s *Server) handleQueryMinifigs(w http.ResponseWriter, r *http.Request) {
	hits, err := s.legoDB.SearchCatalogMinifigs(r.URL.Query().Get("q"), queryLimit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"minifigs": hits})
}
