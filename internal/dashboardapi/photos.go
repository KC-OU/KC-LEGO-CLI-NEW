// Set photos -- the box, the built model, a damaged part -- independent of
// any particular check (compare proofs.go's set-check evidence). Admin-only
// per this round's scope interview. Upload is genuinely new plumbing: the
// first multipart handler in this package, since VerifyProofFile/StoreProof
// both take a file path, not raw bytes, so an uploaded body is written to a
// temp file first, same two-step dance `wms lego attach` does from a real
// file on disk.
package dashboardapi

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

const maxSetPhotoUpload = 25 << 20 // mirrors internal/lego's own maxProofSize

type setPhotoView struct {
	ID         int64  `json:"id"`
	SetNum     string `json:"set_num"`
	Kind       string `json:"kind"`
	Note       string `json:"note,omitempty"`
	By         string `json:"by"`
	UploadedAt string `json:"uploaded_at"`
}

func photoToView(p lego.SetPhoto) setPhotoView {
	return setPhotoView{ID: p.ID, SetNum: p.SetNum, Kind: p.Kind, Note: p.Note, By: p.By, UploadedAt: p.UploadedAt.Format("2006-01-02 15:04")}
}

// handleSetPhotos is GET /dashboard/sets/{setNum}/photos.
func (s *Server) handleSetPhotos(w http.ResponseWriter, r *http.Request) {
	photos, err := s.legoDB.SetPhotos(r.PathValue("setNum"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	views := make([]setPhotoView, 0, len(photos))
	for _, p := range photos {
		views = append(views, photoToView(p))
	}
	writeJSON(w, http.StatusOK, map[string]any{"photos": views})
}

// handleSetPhotoFile serves one attached photo's bytes -- same
// can't-leave-the-folder guard as proofs.go's handleProofFile.
func (s *Server) handleSetPhotoFile(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	photos, err := s.legoDB.SetPhotos(r.PathValue("setNum"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var match *lego.SetPhoto
	for i := range photos {
		if photos[i].ID == id {
			match = &photos[i]
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

// handleUploadSetPhoto is POST /dashboard/sets/{setNum}/photos, multipart:
// fields "kind", "note", "by", file field "photo". The uploaded bytes are
// written to a temp file first (VerifyProofFile/StoreProof both need a
// path), then cleaned up either way.
func (s *Server) handleUploadSetPhoto(w http.ResponseWriter, r *http.Request) {
	setNum := r.PathValue("setNum")
	if err := r.ParseMultipartForm(maxSetPhotoUpload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "could not read the upload: " + err.Error()})
		return
	}
	by := strings.TrimSpace(r.FormValue("by"))
	if by == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "by is required"})
		return
	}
	note := strings.TrimSpace(r.FormValue("note"))
	file, _, err := r.FormFile("photo")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "a photo file is required"})
		return
	}
	defer file.Close()

	tmp, err := os.CreateTemp("", "setphoto-*")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := io.Copy(tmp, file); err != nil {
		tmp.Close()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	tmp.Close()

	kind, err := lego.VerifyProofFile(tmpPath)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	stored, err := lego.StoreProof(attachmentsDir(), setNum, kind, tmpPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	photo, err := s.legoDB.AttachSetPhoto(setNum, kind, stored, note, by)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(by, "DASHBOARD_SET_PHOTO_ADDED", "SUCCESS", setNum)
	writeJSON(w, http.StatusOK, photoToView(*photo))
}

type removeSetPhotoRequest struct {
	Confirm string `json:"confirm"`
	By      string `json:"by"`
}

// handleRemoveSetPhoto is POST /dashboard/sets/{setNum}/photos/{id}/remove
// -- confirm-gated and irreversible: deletes the DB record and (best-effort)
// the stored file.
func (s *Server) handleRemoveSetPhoto(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id must be a number"})
		return
	}
	var req removeSetPhotoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed request"})
		return
	}
	req.By = strings.TrimSpace(req.By)
	if req.By == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "by is required"})
		return
	}
	if err := requireConfirm(req.Confirm); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := s.legoDB.RemoveSetPhoto(id); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.logAudit(req.By, "DASHBOARD_SET_PHOTO_REMOVED", "SUCCESS", "photo #"+strconv.FormatInt(id, 10)+", record and file both removed, irreversible")
	writeJSON(w, http.StatusOK, map[string]bool{"removed": true})
}
