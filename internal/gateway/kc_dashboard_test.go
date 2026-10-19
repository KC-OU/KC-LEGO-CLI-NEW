package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServeKCDashboardServesBothPagesAsHTML(t *testing.T) {
	mux := http.NewServeMux()
	serveKCDashboard(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	for path, want := range map[string]string{
		"/dashboard":  "Team accuracy",
		"/onboarding": "Welcome to the floor",
	} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s = %d, want 200", path, resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Errorf("%s Content-Type = %q, want text/html", path, ct)
		}
		if !strings.Contains(string(body), want) {
			t.Errorf("%s body missing %q", path, want)
		}
	}
}

func TestServeKCDashboardLinksBetweenTheTwoPagesOnly(t *testing.T) {
	// The production pages only ever link to each other -- Kanban/Users/
	// MissingParts/Main stay mockup-only (not deployed here), so a stray
	// href pointing at one of them would be a dead link in production.
	for name, html := range map[string]string{
		"kc_dashboard.html":  string(kcDashboardHTML),
		"kc_onboarding.html": string(kcOnboardingHTML),
	} {
		for _, dead := range []string{"Kanban.dc.html", "Users.dc.html", "MissingParts.dc.html", "Main.dc.html"} {
			if strings.Contains(html, dead) {
				t.Errorf("%s references %s, which isn't served in production", name, dead)
			}
		}
	}
}
