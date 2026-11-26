package dashboardapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/mobileapi"
)

func TestMobileSessionBridge(t *testing.T) {
	t.Setenv(config.AccessFile, filepath.Join(t.TempDir(), "access.json"))
	if _, err := access.Update(func(p *access.Policy) error {
		p.Users[access.Key("wms", "boss")] = &access.User{Groups: []string{"admin"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	db, err := lego.Open(filepath.Join(t.TempDir(), "lego.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := NewServer(db, nil, nil, "secret", nil, nil, nil, nil, nil)
	s.SetMobile(mobileapi.NewServer(nil, nil, db, nil))
	mux := http.NewServeMux()
	mux.HandleFunc("POST /dashboard/mobile-session", s.withToken(s.handleMobileSession))
	s.mountMobile(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	do := func(method, path, body string, hdr map[string]string) (int, map[string]any) {
		req, _ := http.NewRequest(method, ts.URL+path, strings.NewReader(body))
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(res.Body).Decode(&out)
		return res.StatusCode, out
	}
	auth := map[string]string{"Authorization": "Bearer secret"}

	if code, _ := do("POST", "/dashboard/mobile-session", `{"username":"nobody"}`, auth); code != http.StatusForbidden {
		t.Fatalf("an ungoverned account must be refused, got %d", code)
	}
	if code, _ := do("POST", "/dashboard/mobile-session", `{"username":"boss"}`, nil); code != http.StatusUnauthorized {
		t.Fatalf("no shared token must be refused, got %d", code)
	}
	code, out := do("POST", "/dashboard/mobile-session", `{"username":"boss"}`, auth)
	if code != http.StatusOK || out["role"] != "Admin" || out["token"] == "" {
		t.Fatalf("session: %d %v", code, out)
	}
	tok, _ := out["token"].(string)
	if code, _ := do("GET", "/mobile/me", "", map[string]string{"Authorization": "Bearer " + tok}); code != http.StatusUnauthorized {
		t.Fatalf("/mobile/ without X-KC-Token must be refused, got %d", code)
	}
	code, me := do("GET", "/mobile/me", "", map[string]string{"Authorization": "Bearer " + tok, "X-KC-Token": "secret"})
	if code != http.StatusOK || me["username"] != "boss" {
		t.Fatalf("/mobile/me: %d %v", code, me)
	}
}
