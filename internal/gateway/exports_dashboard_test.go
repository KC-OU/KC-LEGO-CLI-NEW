package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/exports"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/partdb"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/wmsdb"
)

type fakeWMS struct{ ok bool }

func (f *fakeWMS) AuthenticateUser(ctx context.Context, usernameOrID, password, passwordMD5 string) (*wmsdb.UserAuthResult, error) {
	if !f.ok {
		return &wmsdb.UserAuthResult{}, nil
	}
	return &wmsdb.UserAuthResult{Found: true, ID: 1, UserName: "dave", Role: "checker", IsValid: true}, nil
}
func (f *fakeWMS) FetchUserPermissions(ctx context.Context, role string) (*wmsdb.Permissions, error) {
	return &wmsdb.Permissions{RoleName: role, Menus: []string{"*"}, CanWrite: true}, nil
}

type fakePartDB struct{}

func (f *fakePartDB) PasswordHash(name string) (string, bool, error) { return "", false, nil }
func (f *fakePartDB) GetUserByName(name string) (*partdb.PartDBUser, error) {
	return nil, nil
}

func TestExportsNeedsBasicAuth(t *testing.T) {
	rec := httptest.NewRecorder()
	exportsHandler(func() string { return t.TempDir() }, &fakeWMS{}, &fakePartDB{}, func(string, string, string) {})(
		rec, httptest.NewRequest(http.MethodGet, "/exports", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no credentials: status = %d, want 401", rec.Code)
	}
}

func TestExportsRejectsWrongPassword(t *testing.T) {
	t.Setenv(config.AccessFile, filepath.Join(t.TempDir(), "access.json"))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/exports", nil)
	req.SetBasicAuth("dave", "wrong")
	exportsHandler(func() string { return t.TempDir() }, &fakeWMS{ok: false}, &fakePartDB{}, func(string, string, string) {})(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password: status = %d, want 401", rec.Code)
	}
}

func TestExportsListsOnlyThatUsersFilesWithFreshLinks(t *testing.T) {
	t.Setenv(config.AccessFile, filepath.Join(t.TempDir(), "access.json"))
	dir := t.TempDir()
	exports.Save(dir, "dave", "missing", "1", "json", []byte(`{"a":1}`))
	exports.Save(dir, "bob", "missing", "1", "json", []byte(`{"b":1}`)) // a different user's export

	var logged []string
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/exports", nil)
	req.SetBasicAuth("dave", "secret")
	exportsHandler(func() string { return dir }, &fakeWMS{ok: true}, &fakePartDB{}, func(user, status, details string) {
		logged = append(logged, user+" "+status)
	})(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "dave-missing-1-") {
		t.Errorf("dave's own export missing from the page:\n%s", body)
	}
	if strings.Contains(body, "bob-missing-1-") {
		t.Errorf("bob's export leaked onto dave's page:\n%s", body)
	}
	if !strings.Contains(body, "/dl/") {
		t.Errorf("no download link rendered:\n%s", body)
	}
	if len(logged) != 1 || logged[0] != "dave SUCCESS" {
		t.Errorf("audit = %v", logged)
	}
}

func TestExportsNeedsThePermission(t *testing.T) {
	t.Setenv(config.AccessFile, filepath.Join(t.TempDir(), "access.json"))
	if _, err := access.Update(func(p *access.Policy) error {
		p.Users["modernwms:dave"] = &access.User{Groups: []string{"builder"}} // no exports.download
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	exports.Save(dir, "dave", "missing", "1", "json", []byte(`{}`))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/exports", nil)
	req.SetBasicAuth("dave", "secret")
	exportsHandler(func() string { return dir }, &fakeWMS{ok: true}, &fakePartDB{}, func(string, string, string) {})(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("a user without exports.download got %d", rec.Code)
	}
}

func TestExportsWithNoFilesStillRendersCleanly(t *testing.T) {
	t.Setenv(config.AccessFile, filepath.Join(t.TempDir(), "access.json"))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/exports", nil)
	req.SetBasicAuth("dave", "secret")
	exportsHandler(func() string { return t.TempDir() }, &fakeWMS{ok: true}, &fakePartDB{}, func(string, string, string) {})(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Nothing here yet") {
		t.Fatalf("empty state: status = %d, body = %s", rec.Code, rec.Body.String())
	}
}
