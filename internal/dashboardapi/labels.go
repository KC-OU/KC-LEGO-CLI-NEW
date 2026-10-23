// Label printing for one set -- `wms labels`'s rendering functions, called
// directly: all of PDF/HTML/PNG/ZPL return in-memory bytes (never a file
// path), so this route hands them back with the right Content-Type rather
// than writing anything to disk.
//
// Scope cut: per-bag labels (cmd/wms/bagging.go's bag-labels) and the LBX
// format stay CLI-only for now -- one set, four formats is the slice that
// covers "print a label for this set" from the dashboard.
package dashboardapi

import (
	"net/http"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/labels"
)

func (s *Server) handleLabel(w http.ResponseWriter, r *http.Request) {
	setNum := r.PathValue("setNum")
	if _, err := s.legoDB.GetSetByNum(setNum); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "set not found in the owned collection"})
		return
	}
	size, err := labels.SizeByID(firstNonEmpty(r.URL.Query().Get("size"), "4x6"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	data := s.legoDB.LabelData(setNum)
	items := []labels.Data{data}

	switch format := firstNonEmpty(r.URL.Query().Get("format"), "pdf"); format {
	case "pdf":
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write(labels.PDF(items, size))
	case "html":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(labels.HTML(items, size))
	case "png":
		png, err := labels.PNG(items, size)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(png)
	case "zpl":
		zpl, err := labels.ZPL(items, size)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write(zpl)
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": `format must be "pdf", "html", "png" or "zpl"`})
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
