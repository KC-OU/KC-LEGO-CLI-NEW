package dashboardapi

import (
	"net/http"
	"strings"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

type catalogSetRow struct {
	lego.SetHit
	Owned int `json:"owned"` // copies in the owned collection, 0 when none
}

type catalogSetSearchResponse struct {
	Sets   []catalogSetRow `json:"sets"`
	Source string          `json:"source"`
	Notes  []string        `json:"notes,omitempty"`
}

// handleCatalogSetSearch is GET /dashboard/catalog/sets?q=...&sort=year|year_asc|num|num_desc.
// q is what the TUI's Search Sets takes: a name, a set number, a theme (lists every set in it) or
// a comma list of set numbers (shows only those).
func (s *Server) handleCatalogSetSearch(w http.ResponseWriter, r *http.Request) {
	res := s.legoDB.FindSets(r.Context(), s.rebrick, r.URL.Query().Get("q"))
	lego.SortSetHits(res.Hits, r.URL.Query().Get("sort"))
	rows := make([]catalogSetRow, len(res.Hits))
	for i, h := range res.Hits {
		rows[i].SetHit = h
		if mine, err := s.legoDB.GetSetByNum(strings.TrimSuffix(h.Num, "-1")); err == nil && mine != nil {
			rows[i].Owned = mine.Qty
		}
	}
	writeJSON(w, http.StatusOK, catalogSetSearchResponse{Sets: rows, Source: res.Source, Notes: res.Notes})
}
