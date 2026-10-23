// Reports hub: missing/extra parts, an order-list report, check-history
// search, retiring-soon sets, and a stock-take checklist -- the read-only
// internal/lego report generators cmd/wms/lego_report.go already wraps for
// the CLI's HTML/PDF/CSV output. This exposes the same underlying structs
// as plain JSON; rendering them as a page/printable is the Rust side's job.
package dashboardapi

import (
	"encoding/csv"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

// writeCSVOrJSON serves data as JSON by default, or as CSV (via
// lego.Encode's own "csv" format, same bytes `wms lego report ... --format
// csv` would produce) when ?format=csv is given.
func writeExportDataCSVOrJSON(w http.ResponseWriter, data *lego.ExportData, filename string) {
	body, _, _, err := lego.Encode("csv", data)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	_, _ = w.Write(body)
}

// handleMissingReport is `wms report missing` -- ?sets=71788,10312 (comma
// separated set numbers), empty means every owned set; ?format=csv for a
// download instead of JSON.
func (s *Server) handleMissingReport(w http.ResponseWriter, r *http.Request) {
	data, err := s.legoDB.MissingPartsReport(splitCommaList(r.URL.Query().Get("sets")))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if r.URL.Query().Get("format") == "csv" {
		writeExportDataCSVOrJSON(w, data, "missing-parts.csv")
		return
	}
	writeJSON(w, http.StatusOK, data)
}

// handleExtraReport is `wms report extra` (spares beyond what's needed);
// ?format=csv for a download instead of JSON.
func (s *Server) handleExtraReport(w http.ResponseWriter, r *http.Request) {
	data, err := s.legoDB.ExtraPartsReport(splitCommaList(r.URL.Query().Get("sets")))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if r.URL.Query().Get("format") == "csv" {
		writeExportDataCSVOrJSON(w, data, "extra-parts.csv")
		return
	}
	writeJSON(w, http.StatusOK, data)
}

// handleOrderListReport is `wms report orders` -- a formatted view over
// the same order data the Orders workflow (orders.go) manages; ?status=
// filters, empty means every status.
func (s *Server) handleOrderListReport(w http.ResponseWriter, r *http.Request) {
	data, err := s.legoDB.OrderListReport(r.URL.Query().Get("status"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, data)
}

// handleCheckHistory is `wms report history` -- ?set=&by=&limit= (default 50).
func (s *Server) handleCheckHistory(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}
	history, err := s.legoDB.CheckHistory(r.URL.Query().Get("set"), r.URL.Query().Get("by"), limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"history": history})
}

// handleRetiringSoon is `wms retirement list` -- ?days= how soon counts as
// "soon" (default 90).
func (s *Server) handleRetiringSoon(w http.ResponseWriter, r *http.Request) {
	days := 90
	if raw := r.URL.Query().Get("days"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			days = n
		}
	}
	sets, err := s.legoDB.RetiringSoon(time.Duration(days) * 24 * time.Hour)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sets": sets, "data_age": s.legoDB.RetirementDataAge()})
}

// handleStockSheet is `wms report stocktake` for one set -- a physical
// count checklist. Uses s.rebrick the same nil-tolerant way the catalog
// lookup does (an offline-only checklist if no live client is configured).
func (s *Server) handleStockSheet(w http.ResponseWriter, r *http.Request) {
	setNum := r.PathValue("setNum")
	title, lines, meta, err := s.legoDB.StockSheet(r.Context(), s.rebrick, setNum)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if r.URL.Query().Get("format") == "csv" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="`+setNum+`-stock-sheet.csv"`)
		cw := csv.NewWriter(w)
		_ = cw.Write([]string{"Part", "Name", "Category", "Colour", "Expected", "Optional"})
		for _, l := range lines {
			_ = cw.Write([]string{l.PartNum, l.PartName, l.Category, l.ColorName, strconv.Itoa(l.Expected), strconv.FormatBool(l.Optional)})
		}
		cw.Flush()
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"title": title, "lines": lines, "meta": meta})
}

func splitCommaList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
