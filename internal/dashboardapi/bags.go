// Bag labels for the dashboard -- small/main/extra only (bag-pool and the
// LBX format were explicitly declined: the user only uses the three
// built-in kinds, not reusable pool bags, and PDF/PNG/ZPL already cover
// their printer). Reuses labels.go's handleLabel's own format switch
// pattern and internal/lego's PrintableBags, the same source the CLI's
// `wms lego bag-labels` command reads, so a reprint from here is identical
// to one from the terminal.
package dashboardapi

import (
	"net/http"
	"strings"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/labels"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

type checkBagView struct {
	ID            int64  `json:"id"`
	CheckID       int64  `json:"check_id"`
	BagKindKey    string `json:"bag_kind_key"`
	BagKindName   string `json:"bag_kind_name"`
	Code          string `json:"code"`
	InstanceLabel string `json:"instance_label"`
	Scanned       bool   `json:"scanned"`
	CreatedBy     string `json:"created_by"`
	CreatedAt     string `json:"created_at"`
}

func (s *Server) bagToView(b lego.CheckBag) checkBagView {
	v := checkBagView{
		ID: b.ID, CheckID: b.CheckID, BagKindKey: b.BagKindKey, BagKindName: b.BagKindKey,
		Code: b.Code, InstanceLabel: b.InstanceLabel, Scanned: !b.ScannedAt.IsZero(),
		CreatedBy: b.CreatedBy, CreatedAt: b.CreatedAt,
	}
	if k, err := s.legoDB.BagKindByKey(b.BagKindKey); err == nil && k != nil {
		v.BagKindName = k.Name
	}
	return v
}

// handlePrintableBags is GET /dashboard/bags/{setNum} -- every bag label
// the set currently has (small/main deterministic even pre-check, extra
// once a check has found a spare).
func (s *Server) handlePrintableBags(w http.ResponseWriter, r *http.Request) {
	setNum := r.PathValue("setNum")
	bags, err := s.legoDB.PrintableBags(setNum)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	views := make([]checkBagView, 0, len(bags))
	for _, b := range bags {
		views = append(views, s.bagToView(b))
	}
	writeJSON(w, http.StatusOK, map[string]any{"bags": views})
}

// handleBagLabel is GET /dashboard/labels/bags/{setNum}/{code}?format=&size=
// -- one bag's label, matched by its own code among PrintableBags (a pool-
// assigned or second-instance bag's real code, not a recomputed one).
func (s *Server) handleBagLabel(w http.ResponseWriter, r *http.Request) {
	setNum, code := r.PathValue("setNum"), r.PathValue("code")
	bags, err := s.legoDB.PrintableBags(setNum)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	var match *lego.CheckBag
	for i := range bags {
		if bags[i].Code == code {
			match = &bags[i]
			break
		}
	}
	if match == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no bag with that code on this set"})
		return
	}
	k, err := s.legoDB.BagKindByKey(match.BagKindKey)
	if err != nil || k == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "unknown bag kind"})
		return
	}
	size, err := labels.SizeByID(firstNonEmpty(r.URL.Query().Get("size"), "50x30"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	d := s.legoDB.LabelData(setNum)
	d.Location = strings.ToUpper(k.Name)
	d.Barcode = match.Code
	items := []labels.Data{d}

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
