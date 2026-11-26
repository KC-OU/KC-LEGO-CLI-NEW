package dashboardapi

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

func TestGenerateGuestUploadLinkFailsWithoutTheSecretConfigured(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	resp := postJSON(t, srv.URL, "/dashboard/onboarding/guest-link", `{"set_num":"75192-1","by":"dave"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (no ONBOARDING_GUEST_LINK_SECRET set)", resp.StatusCode)
	}
}

func withGuestLinkSecret(t *testing.T, secret string) {
	t.Helper()
	t.Setenv("ONBOARDING_GUEST_LINK_SECRET", secret)
}

func TestGenerateGuestUploadLinkReturnsASignedToken(t *testing.T) {
	withGuestLinkSecret(t, "test-secret-value")
	_, srv := testServer(t, "secret")
	defer srv.Close()

	resp := postJSON(t, srv.URL, "/dashboard/onboarding/guest-link", `{"set_num":"75192-1","by":"dave"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Token == "" {
		t.Fatal("expected a non-empty token")
	}
	payload, err := verifyGuestLinkToken(out.Token)
	if err != nil {
		t.Fatal(err)
	}
	if payload.SetNum != "75192-1" {
		t.Fatalf("set_num = %q", payload.SetNum)
	}
}

func TestVerifyGuestLinkTokenRejectsATamperedToken(t *testing.T) {
	withGuestLinkSecret(t, "test-secret-value")
	token, err := signGuestLinkToken("75192-1")
	if err != nil {
		t.Fatal(err)
	}
	// Flip the signature's last hex digit to something guaranteed
	// different (not just "0", which is a no-op if that's what it
	// already was).
	last := token[len(token)-1]
	replacement := byte('0')
	if last == '0' {
		replacement = '1'
	}
	tampered := token[:len(token)-1] + string(replacement)
	if _, err := verifyGuestLinkToken(tampered); err == nil {
		t.Fatal("expected a tampered token to be rejected")
	}
}

func multipartGuestUpload(t *testing.T, token, fullName, userID, date string, includeFile bool) (string, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("token", token)
	_ = mw.WriteField("full_name", fullName)
	_ = mw.WriteField("user_id", userID)
	_ = mw.WriteField("date", date)
	if includeFile {
		fw, err := mw.CreateFormFile("file", "sheet.pdf")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = fw.Write([]byte("%PDF-1.4 fake pdf bytes"))
	}
	_ = mw.Close()
	return mw.FormDataContentType(), &buf
}

func TestGuestUploadRejectsAnExpiredOrInvalidToken(t *testing.T) {
	withGuestLinkSecret(t, "test-secret-value")
	_, srv := testServer(t, "secret")
	defer srv.Close()

	contentType, body := multipartGuestUpload(t, "not-a-real-token", "Dave Smith", "7354", "2026-10-24", true)
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/dashboard/onboarding/guest-upload", body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

func TestGuestUploadSucceedsOnceThenRejectsReuse(t *testing.T) {
	withGuestLinkSecret(t, "test-secret-value")
	t.Setenv(config.LegoAttachmentsDir, t.TempDir())
	db, srv := testServer(t, "secret")
	defer srv.Close()

	token, err := signGuestLinkToken("75192-1")
	if err != nil {
		t.Fatal(err)
	}

	postOnce := func() int {
		contentType, body := multipartGuestUpload(t, token, "Dave Smith", "7354", "2026-10-24", true)
		req, err := http.NewRequest(http.MethodPost, srv.URL+"/dashboard/onboarding/guest-upload", body)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", contentType)
		req.Header.Set("Authorization", "Bearer secret")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}

	if code := postOnce(); code != http.StatusOK {
		t.Fatalf("first upload status = %d, want 200", code)
	}
	if f, _ := db.FlowFor("75192-1", "7354"); f == nil || f.Status != lego.FlowAwaitingVerify {
		t.Fatalf("the guest upload should wait in the verify queue under their ID, got %+v", f)
	}
	if code := postOnce(); code != http.StatusForbidden {
		t.Fatalf("second upload (same token) status = %d, want 403", code)
	}
}

func TestGuestUploadRequiresAFile(t *testing.T) {
	withGuestLinkSecret(t, "test-secret-value")
	_, srv := testServer(t, "secret")
	defer srv.Close()

	token, err := signGuestLinkToken("75192-1")
	if err != nil {
		t.Fatal(err)
	}
	contentType, body := multipartGuestUpload(t, token, "Dave Smith", "7354", "2026-10-24", false)
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/dashboard/onboarding/guest-upload", body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (no file)", resp.StatusCode)
	}
}
