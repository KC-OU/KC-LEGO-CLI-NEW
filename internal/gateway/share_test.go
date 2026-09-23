package gateway

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/exports"
)

func TestShareLinkViewedRepeatedly(t *testing.T) {
	t.Setenv(config.AccessFile, filepath.Join(t.TempDir(), "access.json"))
	dir := t.TempDir()
	path, _ := exports.Save(dir, "kc", "collection", "", "html", []byte("<html>hi</html>"))
	tok, _ := exports.NewShareLink(dir, path, "kc", time.Hour)
	var logged []string
	h := shareHandler(func() string { return dir }, func(user, status, details string) { logged = append(logged, user+" "+status) })

	for i := 0; i < 3; i++ {
		rec := httptest.NewRecorder()
		h(rec, httptest.NewRequest(http.MethodGet, "/share/"+tok, nil))
		if rec.Code != 200 || rec.Body.String() != "<html>hi</html>" {
			t.Fatalf("view #%d: %d %q", i, rec.Code, rec.Body.String())
		}
		if disp := rec.Header().Get("Content-Disposition"); disp == "" || !strings.Contains(disp, "inline") {
			t.Errorf("view #%d: Content-Disposition = %q, want inline", i, disp)
		}
	}
	if len(logged) != 3 {
		t.Fatalf("audit = %v, want 3 SUCCESS entries (never consumed)", logged)
	}
	for _, l := range logged {
		if l != "kc SUCCESS" {
			t.Errorf("unexpected log entry %q", l)
		}
	}
}

func TestShareRefusesASingleUseDownloadToken(t *testing.T) {
	t.Setenv(config.AccessFile, filepath.Join(t.TempDir(), "access.json"))
	dir := t.TempDir()
	path, _ := exports.Save(dir, "kc", "missing", "1", "json", []byte(`{}`))
	tok, _ := exports.NewLink(dir, path, "kc") // a /dl/ token, not a share link
	rec := httptest.NewRecorder()
	shareHandler(func() string { return dir }, func(string, string, string) {})(rec, httptest.NewRequest(http.MethodGet, "/share/"+tok, nil))
	if rec.Code != 404 {
		t.Fatalf("a single-use token via /share/ got %d, want 404", rec.Code)
	}
}

func TestShareNeedsThePermission(t *testing.T) {
	t.Setenv(config.AccessFile, filepath.Join(t.TempDir(), "access.json"))
	dir := t.TempDir()
	path, _ := exports.Save(dir, "bob", "collection", "", "html", []byte("<html></html>"))
	tok, _ := exports.NewShareLink(dir, path, "partdb:bob", time.Hour)
	if _, err := access.Update(func(p *access.Policy) error {
		p.Users["partdb:bob"] = &access.User{Groups: []string{"builder"}} // no exports.download
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	shareHandler(func() string { return dir }, func(string, string, string) {})(rec, httptest.NewRequest(http.MethodGet, "/share/"+tok, nil))
	if rec.Code != 404 {
		t.Fatalf("a user without exports.download got %d", rec.Code)
	}
}
