// Admin re-verification of a set's paper-copy proof: view what was
// attached (via `wms lego attach` or the Discord /wms proof command, see
// internal/botapi/proofs.go) and flag the check disputed if the photo
// doesn't actually back up the count. Mirrors lego.DB.FlagDispute/ProofsFor
// directly -- no new business logic, just a thin HTTP skin, same as every
// other handler in this package.
package dashboardapi

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

// attachmentsDir mirrors internal/botapi/proofs.go's own helper of the same
// name -- duplicated rather than exported across an unrelated package
// boundary for four lines of config lookup, same trade-off as this
// package's own ticketLabel/admin.go precedent.
func attachmentsDir() string {
	if dir := config.Get(config.LegoAttachmentsDir); dir != "" {
		return dir
	}
	return filepath.Join(filepath.Dir(config.Get(config.LegoDBPath)), "attachments")
}

type proofRow struct {
	ID         int64  `json:"id"`
	Kind       string `json:"kind"`
	Note       string `json:"note,omitempty"`
	By         string `json:"by"`
	UploadedAt string `json:"uploaded_at"`
}

// handleProofsForSet is the admin re-verification screen's data: the set's
// current (or most recent) check plus every proof attached to it.
func (s *Server) handleProofsForSet(w http.ResponseWriter, r *http.Request) {
	setNum := r.PathValue("setNum")
	check, err := s.legoDB.CurrentOrLastCheck(setNum)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if check == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "set " + setNum + " has no check yet"})
		return
	}
	proofs, err := s.legoDB.ProofsFor(check.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	rows := make([]proofRow, len(proofs))
	for i, p := range proofs {
		rows[i] = proofRow{ID: p.ID, Kind: p.Kind, Note: p.Note, By: p.By, UploadedAt: p.UploadedAt.Format("2006-01-02 15:04")}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"check_id": check.ID, "disputed": check.Disputed, "dispute_note": check.DisputeNote,
		"proofs": rows,
	})
}

// handleProofFile serves one attached proof's actual bytes -- the admin
// needs to actually look at the photo to decide whether to dispute it.
// Only ever opened by proof ID within attachmentsDir(), via os.OpenInRoot,
// the same can't-leave-the-folder guard internal/gateway's downloadHandler
// already uses for exports.
func (s *Server) handleProofFile(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("proofID"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	setNum := r.PathValue("setNum")
	check, err := s.legoDB.CurrentOrLastCheck(setNum)
	if err != nil || check == nil {
		http.NotFound(w, r)
		return
	}
	proofs, err := s.legoDB.ProofsFor(check.ID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var match *lego.Proof
	for i := range proofs {
		if proofs[i].ID == id {
			match = &proofs[i]
			break
		}
	}
	if match == nil {
		http.NotFound(w, r)
		return
	}
	f, err := os.OpenInRoot(attachmentsDir(), filepath.Base(match.FilePath))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	name := filepath.Base(match.FilePath)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, name, fi.ModTime(), f)
}

type flagDisputeRequest struct {
	SetNum string `json:"set_num"`
	Note   string `json:"note"`
	By     string `json:"by"`
}

// handleFlagDispute is the admin's "this doesn't match" action -- the same
// effect as the CLI's `wms lego attach --flag` / `wms lego flag`, applied
// after review instead of at upload time. Never automatic.
func (s *Server) handleFlagDispute(w http.ResponseWriter, r *http.Request) {
	var req flagDisputeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	req.SetNum, req.Note, req.By = strings.TrimSpace(req.SetNum), strings.TrimSpace(req.Note), strings.TrimSpace(req.By)
	if req.SetNum == "" || req.By == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "set_num and by are required"})
		return
	}
	check, err := s.legoDB.CurrentOrLastCheck(req.SetNum)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if check == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "set " + req.SetNum + " has no check yet"})
		return
	}
	if err := s.legoDB.FlagDispute(check.ID, req.Note); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(req.By, "DASHBOARD_PROOF_DISPUTED", "SUCCESS", req.SetNum+" (check #"+strconv.FormatInt(check.ID, 10)+"): "+req.Note)
	writeJSON(w, http.StatusOK, map[string]any{"set": req.SetNum, "check_id": check.ID, "disputed": true})
}
