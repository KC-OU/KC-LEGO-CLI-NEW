package dashboardapi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/brickset"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/notify"
)

// RefreshRetail asks Brickset (LEGO.com's retail prices and first/last-available
// dates) about every tracked set -- one API call per 100 sets -- stores the answers
// and returns what changed.
func RefreshRetail(ctx context.Context, db *lego.DB, bs *brickset.Client) (int, []lego.RetailChange, error) {
	nums, err := db.TrackedSetNums()
	if err != nil {
		return 0, nil, err
	}
	var all []lego.RetailChange
	total := 0
	for start := 0; start < len(nums); start += 100 {
		sets, err := bs.GetSets(ctx, nums[start:min(start+100, len(nums))])
		if err != nil {
			return total, all, err
		}
		rows := make([]lego.SetRetail, 0, len(sets))
		for _, s := range sets {
			r := lego.SetRetail{SetNum: s.SetNum(), Name: s.Name, Theme: s.Theme, Year: s.Year, LaunchDate: brickset.Day(s.LaunchDate),
				ExitDate: brickset.Day(s.ExitDate), Availability: s.Availability,
				LastUK: brickset.Day(s.LEGOCom.UK.DateLastAvailable), LastUS: brickset.Day(s.LEGOCom.US.DateLastAvailable)}
			if s.Pieces != nil {
				r.Pieces = *s.Pieces
			}
			if p := s.LEGOCom.UK.RetailPrice; p != nil {
				r.RetailUK = *p
			}
			if p := s.LEGOCom.US.RetailPrice; p != nil {
				r.RetailUS = *p
			}
			rows = append(rows, r)
		}
		ch, err := db.SaveSetRetail(rows)
		all = append(all, ch...)
		total += len(rows)
		if err != nil {
			return total, all, err
		}
	}
	return total, all, nil
}

// NotifyRetailChanges sends one alert summarising what changed (the "security"-style
// fallback does not apply: this is a normal routed event, quiet if no channel is set).
func NotifyRetailChanges(changes []lego.RetailChange) {
	if len(changes) == 0 {
		return
	}
	var b strings.Builder
	for i, c := range changes {
		if i == 8 {
			fmt.Fprintf(&b, "...and %d more\n", len(changes)-8)
			break
		}
		fmt.Fprintf(&b, "%s %s: %s\n", c.SetNum, c.Name, c.What)
	}
	notify.Dispatch("price_drop", fmt.Sprintf("Retiring sets: %d change(s)", len(changes)), b.String())
}

// GET /dashboard/retired?days=365
func (s *Server) handleRetired(w http.ResponseWriter, r *http.Request) {
	days := 365
	if n, err := strconv.Atoi(r.URL.Query().Get("days")); err == nil && n > 0 {
		days = n
	}
	rows, err := s.legoDB.RetiredView(time.Duration(days) * 24 * time.Hour)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rows": rows, "brickset_configured": brickset.FromConfig().Enabled(), "sheet_age": s.legoDB.RetirementDataAge()})
}

// POST /dashboard/retired/refresh -- pull fresh dates/prices from Brickset now.
func (s *Server) handleRetiredRefresh(w http.ResponseWriter, r *http.Request) {
	bs := brickset.FromConfig()
	if !bs.Enabled() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": brickset.ErrNotConfigured.Error()})
		return
	}
	n, changes, err := RefreshRetail(r.Context(), s.legoDB, bs)
	NotifyRetailChanges(changes)
	resp := map[string]any{"sets": n, "changes": len(changes)}
	if err != nil {
		resp["error"] = err.Error()
	}
	writeJSON(w, http.StatusOK, resp)
}

// POST /dashboard/retired/import?name=list.xlsx -- the request body is the file (max 10 MB).
func (s *Server) handleRetiredImport(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(io.LimitReader(r.Body, 10<<20+1))
	if err != nil || len(data) > 10<<20 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file unreadable or over 10 MB"})
		return
	}
	n, err := s.legoDB.ImportRetirementFile(r.URL.Query().Get("name"), data)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"imported": n})
}
