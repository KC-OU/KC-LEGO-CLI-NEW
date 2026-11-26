package dashboardapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

// GET /dashboard/counts/due?limit=20&days=30 -- the next lots to cycle-count.
func (s *Server) handleCountsDue(w http.ResponseWriter, r *http.Request) {
	out, err := s.legoDB.CountsDue(s.pdb, int(qFloat(r, "limit", 20)), int(qFloat(r, "days", 30)))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /dashboard/counts {"part_num","color_id","color_name","counted","by"} -- saves a count,
// corrects the stock if it differs, and reports the variance.
func (s *Server) handleCountRecord(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PartNum   string `json:"part_num"`
		ColorID   int    `json:"color_id"`
		ColorName string `json:"color_name"`
		Counted   int    `json:"counted"`
		By        string `json:"by"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || strings.TrimSpace(req.PartNum) == "" || strings.TrimSpace(req.By) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "part_num, counted and by are required"})
		return
	}
	v, err := s.legoDB.RecordCount(req.PartNum, req.ColorID, req.ColorName, req.Counted, req.By)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(req.By, "DASHBOARD_CYCLE_COUNT", "SUCCESS", req.PartNum+" "+req.ColorName+" variance "+strconv.Itoa(v))
	writeJSON(w, http.StatusOK, map[string]int{"variance": v})
}

// GET /dashboard/quality -- data problems worth fixing (read-only).
func (s *Server) handleQuality(w http.ResponseWriter, r *http.Request) {
	out, err := s.legoDB.DataQuality(s.pdb)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// GET /dashboard/bins -- every storage location with its lots and fill, fullest first.
func (s *Server) handleBins(w http.ResponseWriter, r *http.Request) {
	if s.pdb == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Part-DB is not connected"})
		return
	}
	out, err := lego.BinMap(s.pdb)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, out)
}
