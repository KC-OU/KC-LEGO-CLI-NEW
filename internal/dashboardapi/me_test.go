package dashboardapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
)

func TestMyPermissionsRequiresAUsername(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	resp := getWithToken(t, srv.URL, "/dashboard/me/permissions")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestMyPermissionsDeniesEverythingForAnUngovernedUsername(t *testing.T) {
	isolateAccessStores(t)
	_, srv := testServer(t, "secret")
	defer srv.Close()

	resp := getWithToken(t, srv.URL, "/dashboard/me/permissions?username=nobody")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out struct {
		Permissions map[string]bool `json:"permissions"`
		Governed    bool            `json:"governed"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Governed {
		t.Fatal("expected ungoverned")
	}
	for perm, allowed := range out.Permissions {
		if allowed {
			t.Fatalf("expected every permission denied for an ungoverned username, got %s=true", perm)
		}
	}
	if _, ok := out.Permissions["lego.view"]; !ok {
		t.Fatal("expected every known permission to be present in the map")
	}
}

func TestMyPermissionsReflectsAGovernedUsersGroups(t *testing.T) {
	isolateAccessStores(t)
	_, srv := testServer(t, "secret")
	defer srv.Close()

	if _, err := access.Update(func(p *access.Policy) error {
		p.Groups["checker"] = &access.Group{Perms: map[string]string{"lego.view": access.Allow}}
		p.Users[access.Key("wms", "alex")] = &access.User{Groups: []string{"checker"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	resp := getWithToken(t, srv.URL, "/dashboard/me/permissions?username=alex")
	defer resp.Body.Close()
	var out struct {
		Permissions map[string]bool `json:"permissions"`
		Governed    bool            `json:"governed"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if !out.Governed {
		t.Fatal("expected governed")
	}
	if !out.Permissions["lego.view"] {
		t.Fatal("expected lego.view to be allowed via the checker group")
	}
	if out.Permissions["access.manage"] {
		t.Fatal("expected access.manage to still be denied")
	}
}
