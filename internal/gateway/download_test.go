package gateway

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/exports"
)

func TestDownloadOnce(t *testing.T) {
	t.Setenv(config.AccessFile, filepath.Join(t.TempDir(), "access.json"))
	dir := t.TempDir()
	path, _ := exports.Save(dir, "kc", "missing", "1", "json", []byte(`{"a":1}`))
	tok, _ := exports.NewLink(dir, path, "kc")
	var logged []string
	h := downloadHandler(func() string { return dir }, func(user, status, details string) { logged = append(logged, user+" "+status) })

	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/dl/"+tok, nil))
	if rec.Code != 200 || rec.Body.String() != `{"a":1}` || rec.Header().Get("Content-Disposition") == "" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("first download: %d %q %v", rec.Code, rec.Body.String(), rec.Header())
	}
	rec = httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/dl/"+tok, nil))
	if rec.Code != 404 {
		t.Fatalf("second download: %d", rec.Code)
	}
	if len(logged) != 2 || logged[0] != "kc SUCCESS" || logged[1] != "- DENIED" {
		t.Fatalf("audit = %v", logged)
	}
}

func TestDownloadNeedsThePermission(t *testing.T) {
	t.Setenv(config.AccessFile, filepath.Join(t.TempDir(), "access.json"))
	dir := t.TempDir()
	path, _ := exports.Save(dir, "bob", "missing", "1", "json", []byte(`{}`))
	tok, _ := exports.NewLink(dir, path, "partdb:bob")
	if _, err := access.Update(func(p *access.Policy) error {
		p.Users["partdb:bob"] = &access.User{Groups: []string{"builder"}} // no exports.download
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	downloadHandler(func() string { return dir }, func(string, string, string) {})(rec, httptest.NewRequest(http.MethodGet, "/dl/"+tok, nil))
	if rec.Code != 404 {
		t.Fatalf("a user without exports.download got %d", rec.Code)
	}
}
