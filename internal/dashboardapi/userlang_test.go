package dashboardapi

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

func TestSetLangWritesTheUsersLangWithoutDisturbingOtherPrefs(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	// Isolate from the real WMS_USER_PREFS_FILE -- testServer doesn't do
	// this for us since handleSetLang reads config.Get directly rather
	// than through a Server field (see userlang.go's own doc comment on
	// why it doesn't import internal/uiapp).
	path := filepath.Join(t.TempDir(), "user-prefs.json")
	t.Setenv(config.UserPrefsFile, path)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	// A pre-existing preference (as if the TUI's own theme picker wrote it)
	// must survive handleSetLang's read-modify-write untouched.
	if err := os.WriteFile(path, []byte(`{"checker1":{"theme":"dark","tour":true}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	resp := postJSON(t, srv.URL, "/dashboard/lang", `{"username":"checker1","lang":"no"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m["checker1"]["lang"] != "no" {
		t.Fatalf("lang = %v, want no", m["checker1"]["lang"])
	}
	if m["checker1"]["theme"] != "dark" || m["checker1"]["tour"] != true {
		t.Fatalf("existing prefs were disturbed: %+v", m["checker1"])
	}
}

func TestSetLangRequiresUsername(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()
	t.Setenv(config.UserPrefsFile, filepath.Join(t.TempDir(), "user-prefs.json"))
	resp := postJSON(t, srv.URL, "/dashboard/lang", `{"lang":"no"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}
