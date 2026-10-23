package dashboardapi

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

func postJSON(t *testing.T, srv string, path, body string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, srv+path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func getWithToken(t *testing.T, srv string, path string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, srv+path, nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestActivityListsAndClears(t *testing.T) {
	db, srv := testServer(t, "secret")
	defer srv.Close()

	if err := db.LogEvent("test", "admin", "dave", "did a thing"); err != nil {
		t.Fatal(err)
	}

	resp := getWithToken(t, srv.URL, "/dashboard/admin/activity")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out struct {
		Events []map[string]any `json:"events"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(out.Events))
	}

	clearResp := postJSON(t, srv.URL, "/dashboard/admin/activity/clear", `{"by":"admin"}`)
	defer clearResp.Body.Close()
	if clearResp.StatusCode != http.StatusOK {
		t.Fatalf("clear status = %d", clearResp.StatusCode)
	}

	after := getWithToken(t, srv.URL, "/dashboard/admin/activity")
	defer after.Body.Close()
	var out2 struct {
		Events []map[string]any `json:"events"`
	}
	_ = json.NewDecoder(after.Body).Decode(&out2)
	if len(out2.Events) != 0 {
		t.Fatalf("expected the feed to be empty after clearing, got %d", len(out2.Events))
	}
}

func TestClearActivityRequiresBy(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()
	resp := postJSON(t, srv.URL, "/dashboard/admin/activity/clear", `{}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestHandoverRoundTrips(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	empty := getWithToken(t, srv.URL, "/dashboard/admin/handover")
	defer empty.Body.Close()
	var emptyOut map[string]string
	_ = json.NewDecoder(empty.Body).Decode(&emptyOut)
	if emptyOut["body"] != "" {
		t.Fatalf("expected no note yet, got %+v", emptyOut)
	}

	setResp := postJSON(t, srv.URL, "/dashboard/admin/handover", `{"body":"bins need restocking","by":"dave"}`)
	defer setResp.Body.Close()
	if setResp.StatusCode != http.StatusOK {
		t.Fatalf("set status = %d", setResp.StatusCode)
	}

	got := getWithToken(t, srv.URL, "/dashboard/admin/handover")
	defer got.Body.Close()
	var out map[string]string
	_ = json.NewDecoder(got.Body).Decode(&out)
	if out["body"] != "bins need restocking" || out["created_by"] != "dave" {
		t.Fatalf("got %+v", out)
	}
}

func TestRotaSetOverrideAndClear(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	set := postJSON(t, srv.URL, "/dashboard/admin/rota/set", `{"username":"dave","date":"2026-02-02","start":"09:00","end":"17:00","by":"admin"}`)
	defer set.Body.Close()
	if set.StatusCode != http.StatusOK {
		t.Fatalf("set status = %d", set.StatusCode)
	}

	week := getWithToken(t, srv.URL, "/dashboard/admin/rota?start=2026-02-02")
	defer week.Body.Close()
	var out struct {
		Days map[string][]map[string]any `json:"days"`
	}
	if err := json.NewDecoder(week.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Days["2026-02-02"]) != 1 {
		t.Fatalf("expected dave's shift on 2026-02-02, got %+v", out.Days)
	}

	clear := postJSON(t, srv.URL, "/dashboard/admin/rota/clear", `{"username":"dave","date":"2026-02-02","by":"admin"}`)
	defer clear.Body.Close()
	if clear.StatusCode != http.StatusOK {
		t.Fatalf("clear status = %d", clear.StatusCode)
	}
}

func TestRotaWeekRequiresAValidStartDate(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()
	resp := getWithToken(t, srv.URL, "/dashboard/admin/rota?start=not-a-date")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestAdjustAccuracyDocksAndCredits(t *testing.T) {
	db, srv := testServer(t, "secret")
	defer srv.Close()

	dock := postJSON(t, srv.URL, "/dashboard/admin/accuracy/adjust", `{"username":"sam","role":"picker","kind":"dock","amount":15,"reason":"too many mis-picks","by":"admin"}`)
	defer dock.Body.Close()
	if dock.StatusCode != http.StatusOK {
		t.Fatalf("dock status = %d", dock.StatusCode)
	}
	today, err := db.AccuracyToday("sam", "picker")
	if err != nil {
		t.Fatal(err)
	}
	if today != 85 {
		t.Fatalf("expected 85 after a 15-point dock, got %v", today)
	}
}

func TestAdjustAccuracyRejectsAnUnknownKind(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()
	resp := postJSON(t, srv.URL, "/dashboard/admin/accuracy/adjust", `{"username":"sam","kind":"sideways","amount":5,"reason":"x","by":"admin"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestAccessGroupsListsTheSeededGroupsAndPermissionCatalog(t *testing.T) {
	t.Setenv(config.AccessFile, filepath.Join(t.TempDir(), "access.json"))
	_, srv := testServer(t, "secret")
	defer srv.Close()
	resp := getWithToken(t, srv.URL, "/dashboard/admin/access/groups")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out struct {
		Groups      []map[string]any `json:"groups"`
		Permissions []string         `json:"permissions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Groups) == 0 {
		t.Fatal("expected at least the seeded starter groups")
	}
	if len(out.Permissions) == 0 {
		t.Fatal("expected a non-empty permission catalog")
	}
}

func TestSetGroupPermRejectsAnUnknownGroup(t *testing.T) {
	t.Setenv(config.AccessFile, filepath.Join(t.TempDir(), "access.json"))
	_, srv := testServer(t, "secret")
	defer srv.Close()
	resp := postJSON(t, srv.URL, "/dashboard/admin/access/groups/perm", `{"group":"no-such-group","perm":"exports.download","value":"allow","by":"admin"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestAccessEffectiveRequiresAUsername(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()
	resp := getWithToken(t, srv.URL, "/dashboard/admin/access/effective")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}
