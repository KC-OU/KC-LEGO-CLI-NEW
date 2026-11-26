package dashboardapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

func TestCollectionValueReturnsCurrentAndHistory(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()
	resp := getWithToken(t, srv.URL, "/dashboard/lego/value")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out struct {
		Current map[string]any   `json:"current"`
		History []map[string]any `json:"history"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Current == nil {
		t.Fatal("expected a current value object")
	}
}

func TestSystemHealthReportsConfiguredFlags(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()
	resp := getWithToken(t, srv.URL, "/dashboard/admin/system-health")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	// testServer builds a Server with no bl/bo clients, so both must read false.
	if v, _ := out["bricklink_configured"].(bool); v {
		t.Fatal("expected bricklink_configured=false with no client injected")
	}
}

func TestSystemHealthReportsVersionInfoOnceSet(t *testing.T) {
	// handleSystemHealth needs no auth/token checking of its own (withToken
	// wraps it at the mux level, not inside the handler) -- calling it
	// directly avoids needing testServer to also expose its *Server.
	s := &Server{}
	s.SetVersion("1.2.3", "abc1234", "2026-11-03")

	req := httptest.NewRequest(http.MethodGet, "/dashboard/admin/system-health", nil)
	rec := httptest.NewRecorder()
	s.handleSystemHealth(rec, req)

	var out map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out["version"] != "1.2.3" || out["commit"] != "abc1234" || out["build_date"] != "2026-11-03" {
		t.Fatalf("got %+v", out)
	}
}

func TestDevHeadCommitIsBestEffortWithNoRepo(t *testing.T) {
	t.Setenv(config.RepoPath, t.TempDir()) // a real dir, but not a git repo
	if got := devHeadCommit(); got != "" {
		t.Fatalf("devHeadCommit() = %q, want \"\" when the repo isn't reachable", got)
	}
}
