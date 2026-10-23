package dashboardapi

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

func testPNGBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func uploadPhoto(t *testing.T, srv *httptest.Server, setNum, kind, note, by string) *http.Response {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("note", note)
	_ = mw.WriteField("by", by)
	part, err := mw.CreateFormFile("photo", "test.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(testPNGBytes(t)); err != nil {
		t.Fatal(err)
	}
	_ = mw.Close()
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/dashboard/sets/"+setNum+"/photos", &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestSetPhotoUploadListFileAndRemove(t *testing.T) {
	// attachmentsDir() falls back to LEGO_DB_PATH's own directory -- point
	// that at a fresh tempdir so the uploaded file never lands anywhere near
	// a real attachments folder.
	dir := t.TempDir()
	t.Setenv(config.LegoDBPath, filepath.Join(dir, "lego.db"))
	db, srv := testServer(t, "secret")
	defer srv.Close()
	if err := db.UpsertSet(lego.Set{SetNum: "75192-1", Name: "Millennium Falcon", Theme: "Star Wars", Qty: 1}); err != nil {
		t.Fatal(err)
	}

	// Empty before upload.
	resp := getWithToken(t, srv.URL, "/dashboard/sets/75192-1/photos")
	var out struct {
		Photos []setPhotoView `json:"photos"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(out.Photos) != 0 {
		t.Fatalf("expected no photos yet, got %d", len(out.Photos))
	}

	// Upload.
	upResp := uploadPhoto(t, srv, "75192-1", "", "the box, unopened", "admin")
	defer upResp.Body.Close()
	if upResp.StatusCode != http.StatusOK {
		t.Fatalf("upload status = %d", upResp.StatusCode)
	}
	var uploaded setPhotoView
	if err := json.NewDecoder(upResp.Body).Decode(&uploaded); err != nil {
		t.Fatal(err)
	}
	if uploaded.Kind != "png" {
		t.Fatalf("kind = %q, want png", uploaded.Kind)
	}

	// Now listed.
	resp2 := getWithToken(t, srv.URL, "/dashboard/sets/75192-1/photos")
	defer resp2.Body.Close()
	var out2 struct {
		Photos []setPhotoView `json:"photos"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&out2); err != nil {
		t.Fatal(err)
	}
	if len(out2.Photos) != 1 {
		t.Fatalf("expected 1 photo, got %d", len(out2.Photos))
	}

	// The actual file serves.
	fileResp := getWithToken(t, srv.URL, "/dashboard/sets/75192-1/photos/"+strconv.FormatInt(uploaded.ID, 10)+"/file")
	defer fileResp.Body.Close()
	if fileResp.StatusCode != http.StatusOK {
		t.Fatalf("file status = %d", fileResp.StatusCode)
	}

	// Remove without confirm is rejected.
	badRemove := postJSON(t, srv.URL, "/dashboard/sets/75192-1/photos/"+strconv.FormatInt(uploaded.ID, 10)+"/remove", `{"by":"admin"}`)
	defer badRemove.Body.Close()
	if badRemove.StatusCode != http.StatusBadRequest {
		t.Fatalf("remove without confirm status = %d, want 400", badRemove.StatusCode)
	}

	// Remove with confirm succeeds and the file is actually gone.
	goodRemove := postJSON(t, srv.URL, "/dashboard/sets/75192-1/photos/"+strconv.FormatInt(uploaded.ID, 10)+"/remove", `{"confirm":"yes","by":"admin"}`)
	defer goodRemove.Body.Close()
	if goodRemove.StatusCode != http.StatusOK {
		t.Fatalf("remove status = %d", goodRemove.StatusCode)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "attachments"))
	if len(entries) != 0 {
		t.Fatalf("expected the stored file to be removed too, found %v", entries)
	}
}

func TestUploadSetPhotoRejectsAMissingByField(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(config.LegoDBPath, filepath.Join(dir, "lego.db"))
	_, srv := testServer(t, "secret")
	defer srv.Close()
	resp := uploadPhoto(t, srv, "75192-1", "", "note", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}
