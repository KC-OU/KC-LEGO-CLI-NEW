// Catalog-only set lookup: type any set number, see its metadata and full
// parts list, without it being owned. Deliberately a different route from
// /dashboard/sets/{setNum} (handleSetDetail), which 404s on anything not
// already in the owned collection -- this one never touches owned_* tables
// at all, same offline-catalog-then-live-Rebrickable chain the TUI's own
// "Lookup" screen (internal/uiapp/detail.go) uses, minus the owned-collection
// fields that screen also mixes in.
package dashboardapi

import "net/http"

type catalogSetLookupResponse struct {
	Set             interface{} `json:"set"`
	Source          string      `json:"source"`
	Notes           []string    `json:"notes,omitempty"`
	Inventory       interface{} `json:"inventory"`
	InventorySource string      `json:"inventory_source"`
}

// handleCatalogSetLookup is GET /dashboard/catalog/sets/{setNum}.
func (s *Server) handleCatalogSetLookup(w http.ResponseWriter, r *http.Request) {
	setNum := r.PathValue("setNum")
	lk := s.legoDB.LookupSet(r.Context(), s.rebrick, setNum)
	inv := s.legoDB.LookupSetInventory(r.Context(), s.rebrick, setNum)
	if !lk.Found() && len(inv.Items) == 0 {
		notes := append(append([]string{}, lk.Notes...), inv.Notes...)
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "set not found", "notes": notes})
		return
	}
	writeJSON(w, http.StatusOK, catalogSetLookupResponse{
		Set:             lk.Set,
		Source:          lk.Source,
		Notes:           lk.Notes,
		Inventory:       inv.Items,
		InventorySource: inv.Source,
	})
}
