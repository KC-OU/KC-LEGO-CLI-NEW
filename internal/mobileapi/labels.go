package mobileapi

import (
	"net/http"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/labels"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

// handleLabelPDF is "print set/order labels from the phone, not only from
// the CLI/TUI" (the roadmap's own wording) — the exact same labels.PDF /
// LabelData / LabelSets pipeline `wms lego labels` already uses
// (cmd/wms/lego_sets.go), just reached over HTTP instead of a shell. No
// client-side PDF library in the mobile app: it just opens this URL
// (Linking.openURL, already built into React Native — no new dependency)
// and lets the OS's own PDF viewer/share sheet take it from there, the
// same as tapping any other PDF link.
func (s *Server) handleLabelPDF(w http.ResponseWriter, r *http.Request, sess lego.MobileSession) {
	if !s.requireAdmin(w, sess, "ADMIN_PRINT_LABEL") {
		return
	}
	set := r.PathValue("set")
	if set == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "set number required"})
		return
	}
	sizeID := r.URL.Query().Get("size")
	if sizeID == "" {
		sizeID = "4x6"
	}
	size, err := labels.SizeByID(sizeID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	sets, err := s.legoDB.LabelSets([]string{set})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if len(sets) == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "set not found"})
		return
	}
	items := make([]labels.Data, len(sets))
	for i, n := range sets {
		items[i] = s.legoDB.LabelData(n)
	}

	body := labels.PDF(items, size)
	s.audit.Log(sess.Username, sess.Role, "LABEL_PRINTED", "SUCCESS", set+" ("+size.ID+", mobile)")
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `inline; filename="`+set+`-label.pdf"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}
