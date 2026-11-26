package dashboardapi

import (
	"fmt"
	"mime"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui/img"
)

// newExportImages builds the picture fetcher an export uses. A package variable so tests can swap in one that
// never touches the network or the live picture cache.
var newExportImages = func() *img.Fetcher { return img.NewFetcher(config.Get(config.ImageDir)) }

var exportNum = regexp.MustCompile(`^[A-Za-z0-9._-]{1,40}$`)

// handleExport is the terminal's X export screen as a download: the same data builders and the same
// lego.Encode formats, so the dashboard offers exactly what the CLI does.
//
//	GET /dashboard/export/{kind}?format=<format>[&num=<set>][&sets=<a,b>]
//
// kind is set (needs num), missing or extra (optional sets), owned or build. The formats allowed per kind
// come from lego.FormatsFor. Pictures are embedded from the local cache only, as in the terminal.
func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	kind, format := r.PathValue("kind"), r.URL.Query().Get("format")
	num := strings.TrimSpace(r.URL.Query().Get("num"))
	if !slices.Contains(lego.FormatsFor(kind), format) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("format %q is not offered for %q exports", format, kind)})
		return
	}
	images := newExportImages() // the same picture cache the terminal uses
	var data *lego.ExportData
	var err error
	label := "all"
	switch kind {
	case "set":
		if !exportNum.MatchString(num) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "num must be a set number such as 75192-1"})
			return
		}
		label = num
		if data, err = s.legoDB.ExportDetail(r.Context(), s.rebrick, images, "set", num, -1); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
	case "missing":
		data, err = s.legoDB.MissingPartsReport(splitCommaList(r.URL.Query().Get("sets")))
	case "extra":
		data, err = s.legoDB.ExtraPartsReport(splitCommaList(r.URL.Query().Get("sets")))
	case "owned":
		data, err = s.legoDB.ExportOwned()
	case "build":
		var res []lego.BuildResult
		if res, err = s.legoDB.CanBuild(lego.BuildOptions{MinPercent: 50}); err == nil {
			data = lego.ExportBuild(res)
		}
	}
	if err != nil {
		status := http.StatusInternalServerError
		if kind == "build" {
			status = http.StatusConflict // "refresh the offline catalog first": something to do, not a server fault
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	if kind != "set" && kind != "build" { // the terminal adds pictures to these too
		data.AddPartImages(images)
	}
	if format == "sorting-html" {
		data.CheckerID = lego.WorkerID(r.URL.Query().Get("by")) // pre-fills the sheet's Sorted by / Checked by boxes: a numeric ID, never a name
		s.legoDB.PrepareSortingSheet(r.Context(), data, images) // every sorting sheet: element ids, swatches, embedded pictures
	}
	body, ext, _, err := lego.Encode(format, data)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	name := kind + "-" + label
	if !slices.Contains([]string{"json", "xlsx", "csv", "html"}, format) {
		name += "-" + format // sorting-html, bricklink-xml, rebrickable-csv
	}
	ct := mime.TypeByExtension("." + ext)
	if ct == "" {
		ct = "application/octet-stream"
	}
	disposition := "attachment"
	if ext == "html" {
		disposition = "inline" // opens in the browser so it can be printed
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Disposition", disposition+`; filename="`+name+"."+ext+`"`)
	_, _ = w.Write(body)
}
