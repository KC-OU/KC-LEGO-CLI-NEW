// The web dashboard's first access to a set's most recently finished parts
// check, line by line (Need/Have/Extra/Damaged) -- the TUI and mobile app
// have had this for a while (lego.CheckLine, internal/uiapp/set_check.go,
// internal/mobileapi/check_walk.go), the web dashboard never has.
// Deliberately read-only: /onboarding is a separate pre-check confirmation
// checklist, not this.
package dashboardapi

import (
	"net/http"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

type checkLineView struct {
	PartNum   string `json:"part_num"`
	PartName  string `json:"part_name"`
	ColorID   int    `json:"color_id"`
	ColorName string `json:"color_name"`
	Need      int    `json:"need"`
	Have      int    `json:"have"`
	Missing   int    `json:"missing"`
	Extra     int    `json:"extra"`
	Damaged   int    `json:"damaged"`
	Optional  bool   `json:"optional"`
	// Borrowed counts as present (BorrowedFrom names the donor set); Lent is how many of this set's own are
	// out on loan; Note is the checker's text on the line.
	Borrowed     int    `json:"borrowed"`
	BorrowedFrom string `json:"borrowed_from,omitempty"`
	Lent         int    `json:"lent"`
	Note         string `json:"note,omitempty"`
	// ElementID/ImageURL are "" when neither an override nor the catalog
	// has one for this part+colour -- same as InvItem's (see enrichElementIDs).
	ElementID string `json:"element_id,omitempty"`
	ImageURL  string `json:"image_url,omitempty"`
}

type checkView struct {
	SetNum       string          `json:"set_num"`
	Kind         string          `json:"kind"`
	Status       string          `json:"status"`
	CheckedBy    string          `json:"checked_by"`
	FinishedAt   string          `json:"finished_at"`
	Pieces       int             `json:"pieces"`
	Have         int             `json:"have"`
	Missing      int             `json:"missing"`
	Extra        int             `json:"extra"`
	MissingLines int             `json:"missing_lines"`
	Lines        []checkLineView `json:"lines"`
}

// handleSetCheck is GET /dashboard/checks/{setNum}?sort=part_id|color|qty.
// It reads the already-finished check GetSetState points at (DB.GetCheck, a
// plain SELECT) -- it never opens or mutates a check, unlike DB.NewCheck,
// which is why this doesn't reuse that: a GET must never have that side
// effect. 404 when the set has never had a check finished.
func (s *Server) handleSetCheck(w http.ResponseWriter, r *http.Request) {
	setNum := r.PathValue("setNum")
	state := s.legoDB.GetSetState(setNum)
	if state.LastCheck == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no finished check for this set yet"})
		return
	}
	c, err := s.legoDB.GetCheck(state.LastCheck.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	lego.SortBy(c.Lines, r.URL.Query().Get("sort"),
		func(l lego.CheckLine) string { return l.PartNum },
		func(l lego.CheckLine) string { return l.ColorName },
		func(l lego.CheckLine) int { return l.Need })

	lines := make([]checkLineView, len(c.Lines))
	for i, l := range c.Lines {
		elementID := s.legoDB.ElementFor(l.PartNum, l.ColorID)
		imageURL := s.legoDB.ImageOverrideFor(l.PartNum, l.ColorID)
		if imageURL == "" && elementID != "" {
			imageURL = "https://cdn.rebrickable.com/media/parts/elements/" + elementID + ".jpg"
		}
		lines[i] = checkLineView{
			PartNum: l.PartNum, PartName: l.PartName, ColorID: l.ColorID, ColorName: l.ColorName,
			Need: l.Need, Have: l.Have, Missing: l.Missing(), Extra: l.Extra, Damaged: l.Damaged, Optional: l.Optional,
			Borrowed: l.Borrowed, BorrowedFrom: l.BorrowedFrom, Lent: l.Lent, Note: l.Note,
			ElementID: elementID, ImageURL: imageURL,
		}
	}
	pieces, have, missing, extra, missingLines := c.Totals()
	writeJSON(w, http.StatusOK, checkView{
		SetNum: c.SetNum, Kind: c.Kind, Status: c.Status, CheckedBy: c.CheckedBy,
		FinishedAt: c.FinishedAt.Format(time.RFC3339),
		Pieces:     pieces, Have: have, Missing: missing, Extra: extra, MissingLines: missingLines,
		Lines: lines,
	})
}
