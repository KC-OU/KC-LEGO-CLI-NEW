package dashboardapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/clerkbridge"
)

func TestSwitchLoginIsAdminOnlyPasswordOnlyAndLocksOut(t *testing.T) {
	isolateGraceStores(t)
	users := map[string]*clerkbridge.UserInfo{
		"7354":             {ID: "user_admin", Username: "7354", Role: "admin"},
		"boss@kcparts.xyz": {ID: "user_admin", Username: "7354", Role: "admin"},
		"other":            {ID: "user_chk2", Username: "other", Role: ""},
	}
	oldFind, oldVerify := switchFind, switchVerify
	defer func() { switchFind, switchVerify = oldFind, oldVerify }()
	switchFind = func(id string) (*clerkbridge.UserInfo, error) { return users[id], nil }
	switchVerify = func(_, pw string) (bool, error) { return pw == "right", nil }

	_, srv := testServer(t, "secret")
	defer srv.Close()
	call := func(body string) (int, switchLoginResponse) {
		resp := postJSON(t, srv.URL, "/dashboard/auth/switch-login", body)
		defer resp.Body.Close()
		var out switchLoginResponse
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}

	if c, out := call(`{"identifier":"7354","password":"right","from":"checker"}`); c != http.StatusOK || out.Username != "7354" || out.Role != "admin" {
		t.Fatalf("admin by ID: %d %+v", c, out)
	}
	if c, out := call(`{"identifier":"boss@kcparts.xyz","password":"right","from":"checker"}`); c != http.StatusOK || out.Email != "boss@kcparts.xyz" {
		t.Fatalf("admin by email: %d %+v", c, out)
	}
	if c, _ := call(`{"identifier":"7354","password":"wrong","from":"checker"}`); c != http.StatusUnauthorized {
		t.Fatalf("wrong password: %d", c)
	}
	if c, _ := call(`{"identifier":"other","password":"right","from":"checker"}`); c != http.StatusUnauthorized {
		t.Fatalf("a non-admin must never be switched to: %d", c)
	}
	if c, _ := call(`{"identifier":"nobody","password":"right","from":"checker"}`); c != http.StatusUnauthorized {
		t.Fatalf("unknown account: %d", c)
	}
	// five wrong passwords lock the identifier, even for the right one afterwards
	for i := 0; i < 5; i++ {
		call(`{"identifier":"boss@kcparts.xyz","password":"wrong","from":"checker"}`)
	}
	if c, _ := call(`{"identifier":"boss@kcparts.xyz","password":"right","from":"checker"}`); c != http.StatusUnauthorized {
		t.Fatalf("locked out identifier still got in: %d", c)
	}
}
