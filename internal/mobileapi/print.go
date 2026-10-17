package mobileapi

import (
	_ "embed"
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/labels"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

//go:embed printpage.html
var printPageHTML []byte

// pm260HeadWidthBytes is the PM260's (and compatible M-series Phomemo/Labelnize
// printers') own full print-head width: 72mm / 576px at 203dpi = 72 bytes. A
// narrower label (down to the small-bag sizes, 30-50mm) is centered within it by
// labels.Raster1bpp, the same way the printer's own app lays out anything
// narrower than the head.
const pm260HeadWidthBytes = 72

// handlePrintPage serves the Bluetooth print page itself: a static, self-contained
// page (no build step, no app-store install) that signs in through the same
// /mobile/login the app uses, then talks Web Bluetooth directly to a PM260 in
// range of whatever phone or laptop has it open -- this Go server never touches
// Bluetooth itself, since it's rarely anywhere near the printer. Public (no
// session) because signing in is the page's own first step, done in-browser.
func (s *Server) handlePrintPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(printPageHTML)
}

// handlePrintRaster builds one label as PM260-ready raster bytes (base64, for the
// print page's fetch to decode and send over Bluetooth) -- the same LabelData/
// Location-override pipeline `wms lego small-bag-label` uses, just reached over
// HTTP. Gated the same as handleLabelPDF: printing a label is an admin action.
func (s *Server) handlePrintRaster(w http.ResponseWriter, r *http.Request, sess lego.MobileSession) {
	if !s.requireAdmin(w, sess, "ADMIN_PRINT_LABEL") {
		return
	}
	set := r.URL.Query().Get("set")
	if set == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "set number required"})
		return
	}
	sizeID := r.URL.Query().Get("size")
	if sizeID == "" {
		sizeID = "50x30"
	}
	size, err := labels.SizeByID(sizeID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	d := s.legoDB.LabelData(set)
	kindKey := r.URL.Query().Get("bag_kind")
	switch {
	case r.URL.Query().Get("bag_code") != "":
		// The preferred path: the print page's dropdown now sends a bag's own
		// true code (see handlePrintBags/DB.PrintableBags) -- printed as-is,
		// never recomputed, so a pool-assigned bag or a second same-kind
		// instance reprints correctly instead of silently getting the wrong
		// (first-instance) code.
		d.Barcode = r.URL.Query().Get("bag_code")
		if kindKey == "" {
			if b, _ := s.legoDB.ResolveBagCode(d.Barcode); b != nil {
				kindKey = b.BagKindKey
			}
		}
		if kindKey != "" {
			if k, _ := s.legoDB.BagKindByKey(kindKey); k != nil {
				d.Location = strings.ToUpper(k.Name)
			}
		}
	case kindKey != "":
		// Back-compat: a kind alone, with no specific code -- only ever
		// right for that kind's very first instance (see BagCode's own
		// doc comment). Callers that know which instance they mean should
		// send bag_code instead.
		k, err := s.legoDB.BagKindByKey(kindKey)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if k == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown bag kind: " + kindKey})
			return
		}
		d.Location = strings.ToUpper(k.Name)
		d.Barcode = lego.BagCode(set, kindKey)
	default:
		// Back-compat path from before bag kinds existed: a free-text barcode.
		d.Location = "SMALL PARTS"
		if bag := r.URL.Query().Get("bag"); bag != "" {
			d.Barcode = bag
		}
	}
	data, widthBytes, heightLines, err := labels.Raster1bpp(d, size, pm260HeadWidthBytes)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.audit.Log(sess.Username, sess.Role, "LABEL_PRINTED", "SUCCESS", d.SetNum+" ("+size.ID+", bluetooth)")
	writeJSON(w, http.StatusOK, map[string]any{
		"width_bytes":  widthBytes,
		"height_lines": heightLines,
		"data":         base64.StdEncoding.EncodeToString(data),
	})
}

type printBagOption struct {
	Key, Name, Code string
}

// handlePrintBags lists which bags the print page can offer for a set, each
// with its own true code (see DB.PrintableBags) -- small and main always
// (deterministic, not-yet-printed codes, same as `wms lego bag-labels`),
// plus whatever a check has actually registered: extra once one's been
// created, a pool-assigned bag's real code, additional instances, custom
// kinds. Never recomputed from kind alone, so picking one from here and
// printing it (see handlePrintRaster's bag_code) reprints exactly what's
// actually in play, not just a kind's first instance.
func (s *Server) handlePrintBags(w http.ResponseWriter, r *http.Request, sess lego.MobileSession) {
	if !s.requireAdmin(w, sess, "ADMIN_PRINT_LABEL") {
		return
	}
	set := r.URL.Query().Get("set")
	if set == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "set number required"})
		return
	}
	bags, err := s.legoDB.PrintableBags(set)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	var out []printBagOption
	for _, b := range bags {
		k, err := s.legoDB.BagKindByKey(b.BagKindKey)
		if err != nil || k == nil {
			continue
		}
		out = append(out, printBagOption{Key: k.Key, Name: k.Name, Code: b.Code})
	}
	writeJSON(w, http.StatusOK, out)
}
