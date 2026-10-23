package dashboardapi

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

// isolateAccessStores points every file internal/access, internal/twofa and
// internal/loginlock touch at a fresh tempdir -- never the real production
// files at their package defaults.
func isolateAccessStores(t *testing.T) {
	t.Helper()
	t.Setenv(config.AccessFile, filepath.Join(t.TempDir(), "access.json"))
	t.Setenv(config.TwoFAFile, filepath.Join(t.TempDir(), "2fa.json"))
	t.Setenv(config.LoginLockFile, filepath.Join(t.TempDir(), "login-lock.json"))
}

func TestAccessUserGroupsRoundTrips(t *testing.T) {
	isolateAccessStores(t)
	_, srv := testServer(t, "secret")
	defer srv.Close()

	resp := postJSON(t, srv.URL, "/dashboard/admin/access/user/groups", `{"username":"alex","groups":["checker","picker"],"by":"admin"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("set-groups status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	show := getWithToken(t, srv.URL, "/dashboard/admin/access/user/alex")
	defer show.Body.Close()
	var out struct {
		User accessUserView `json:"user"`
	}
	if err := json.NewDecoder(show.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if !out.User.Governed {
		t.Fatal("expected the user to now be governed")
	}
	if len(out.User.Groups) != 2 || out.User.Groups[0] != "checker" {
		t.Fatalf("groups = %v", out.User.Groups)
	}
}

// Regression test: dashboard linking must NEVER touch access.Policy.
// internal/uiapp's own governed()/legacyCan() treat the mere existence of
// an access.Policy entry as "governed" (only explicit groups/perms apply,
// no more legacy ModernWMS/Part-DB role fallback) -- an earlier version of
// this feature stored the dashboard flag ON access.User and would have
// silently stripped an ungoverned Picker/Checker of their existing full
// legacy telnet access the moment an admin turned dashboard access on for
// them. Turning dashboard off here must leave the user exactly as
// ungoverned as they started.
func TestSetDashboardOffNeverCreatesAnAccessPolicyEntry(t *testing.T) {
	isolateAccessStores(t)
	_, srv := testServer(t, "secret")
	defer srv.Close()

	resp := postJSON(t, srv.URL, "/dashboard/admin/access/user/dashboard", `{"username":"77777","on":false,"by":"admin"}`)
	resp.Body.Close()

	show := getWithToken(t, srv.URL, "/dashboard/admin/access/user/77777")
	defer show.Body.Close()
	var out struct {
		User accessUserView `json:"user"`
	}
	if err := json.NewDecoder(show.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.User.Governed {
		t.Fatal("expected the user to remain ungoverned -- dashboard linking must not create an access.Policy entry")
	}
}

func TestSetDashboardOffClearsTheFlagWithoutNeedingUsersService(t *testing.T) {
	isolateAccessStores(t)
	_, srv := testServer(t, "secret")
	defer srv.Close()

	resp := postJSON(t, srv.URL, "/dashboard/admin/access/user/dashboard", `{"username":"12345","on":false,"by":"admin"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

// No real wmsdb/partdb connection exists in this sandbox (same constraint
// dashboardapi_test.go's own create-account tests document), so usersSvc
// is always nil here -- these tests cover the "not available"/"not
// configured" refusals, not a real ResetUnified+provision round trip.
// That round trip is covered live against the isolated verify.env
// instance plus a throwaway kc-parts-auth-node, per this session's
// established pattern for anything crossing a process boundary.

func TestSetDashboardOnWithNoUsersServiceIs503(t *testing.T) {
	isolateAccessStores(t)
	_, srv := testServer(t, "secret")
	defer srv.Close()

	resp := postJSON(t, srv.URL, "/dashboard/admin/access/user/dashboard", `{"username":"12345","on":true,"by":"admin"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}

func TestAccessUserShowOnAnUngovernedUserSaysSo(t *testing.T) {
	isolateAccessStores(t)
	_, srv := testServer(t, "secret")
	defer srv.Close()

	resp := getWithToken(t, srv.URL, "/dashboard/admin/access/user/nobody")
	defer resp.Body.Close()
	var out struct {
		User accessUserView `json:"user"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.User.Governed {
		t.Fatal("expected an unconfigured user to not be governed")
	}
}

func TestAccessUserSetPasswordRejectsWithoutConfirm(t *testing.T) {
	isolateAccessStores(t)
	_, srv := testServer(t, "secret")
	defer srv.Close()

	resp := postJSON(t, srv.URL, "/dashboard/admin/access/user/password", `{"username":"alex","generate":true,"by":"admin"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 without confirm", resp.StatusCode)
	}
}

func TestAccessUserSetPasswordReturnsAGeneratedPasswordOnce(t *testing.T) {
	isolateAccessStores(t)
	_, srv := testServer(t, "secret")
	defer srv.Close()

	resp := postJSON(t, srv.URL, "/dashboard/admin/access/user/password", `{"username":"alex","generate":true,"confirm":"yes","by":"admin"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	pw, _ := out["password"].(string)
	if pw == "" {
		t.Fatal("expected a generated password in the response")
	}

	show := getWithToken(t, srv.URL, "/dashboard/admin/access/user/alex")
	defer show.Body.Close()
	body, _ := decodeJSONBody(show)
	if _, leaked := body["password_hash"]; leaked {
		t.Fatal("password_hash must never be serialized")
	}
	if hasPW, _ := body["user"].(map[string]any)["has_password"].(bool); !hasPW {
		t.Fatal("expected has_password to be true after setting one")
	}
}

func TestAccessUserBadgeReturnsTheTokenOnlyFromRegenerate(t *testing.T) {
	isolateAccessStores(t)
	_, srv := testServer(t, "secret")
	defer srv.Close()

	resp := postJSON(t, srv.URL, "/dashboard/admin/access/user/badge", `{"username":"alex","action":"regenerate","confirm":"yes","by":"admin"}`)
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if token, _ := out["badge_token"].(string); token == "" {
		t.Fatal("expected a badge token from a regenerate action")
	}

	show := getWithToken(t, srv.URL, "/dashboard/admin/access/user/alex")
	defer show.Body.Close()
	body, _ := decodeJSONBody(show)
	if _, leaked := body["badge_token"]; leaked {
		t.Fatal("a show response must never carry the live badge token")
	}
}

func TestAccessUserRenameRequiresANumericNewID(t *testing.T) {
	isolateAccessStores(t)
	_, srv := testServer(t, "secret")
	defer srv.Close()

	postJSON(t, srv.URL, "/dashboard/admin/access/user/groups", `{"username":"7354","groups":["picker"],"by":"admin"}`).Body.Close()

	resp := postJSON(t, srv.URL, "/dashboard/admin/access/user/rename", `{"old_id":"7354","new_id":"not-a-number","confirm":"yes","by":"admin"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for a non-numeric new id", resp.StatusCode)
	}
}

func TestAccessUserRenameMovesTheEntryAndWarnsAboutTheAuditLog(t *testing.T) {
	isolateAccessStores(t)
	_, srv := testServer(t, "secret")
	defer srv.Close()

	postJSON(t, srv.URL, "/dashboard/admin/access/user/groups", `{"username":"7354","groups":["picker"],"by":"admin"}`).Body.Close()

	resp := postJSON(t, srv.URL, "/dashboard/admin/access/user/rename", `{"old_id":"7354","new_id":"7421","confirm":"yes","by":"admin"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body, _ := decodeJSONBody(resp)
	if warning, _ := body["warning"].(string); warning == "" {
		t.Fatal("expected the audit-log-never-rewritten warning in the response")
	}

	oldShow := getWithToken(t, srv.URL, "/dashboard/admin/access/user/7354")
	defer oldShow.Body.Close()
	oldBody, _ := decodeJSONBody(oldShow)
	if governed, _ := oldBody["user"].(map[string]any)["governed"].(bool); governed {
		t.Fatal("expected the old id to no longer have an entry")
	}

	newShow := getWithToken(t, srv.URL, "/dashboard/admin/access/user/7421")
	defer newShow.Body.Close()
	newBody, _ := decodeJSONBody(newShow)
	if governed, _ := newBody["user"].(map[string]any)["governed"].(bool); !governed {
		t.Fatal("expected the new id to now have the entry")
	}
}

func TestAccessUserRemoveRequiresConfirm(t *testing.T) {
	isolateAccessStores(t)
	_, srv := testServer(t, "secret")
	defer srv.Close()

	postJSON(t, srv.URL, "/dashboard/admin/access/user/groups", `{"username":"alex","groups":["picker"],"by":"admin"}`).Body.Close()

	resp := postJSON(t, srv.URL, "/dashboard/admin/access/user/remove", `{"username":"alex","by":"admin"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 without confirm", resp.StatusCode)
	}
}

func decodeJSONBody(resp *http.Response) (map[string]any, error) {
	var out map[string]any
	err := json.NewDecoder(resp.Body).Decode(&out)
	return out, err
}
