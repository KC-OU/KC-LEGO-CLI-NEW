package gateway

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// proxyFor builds a real proxy in front of backend via newInjectedProxy, the
// same way web.go wires one, so these tests exercise the exact code path a
// request through the gateway takes.
func proxyFor(t *testing.T, backend *httptest.Server) *httptest.Server {
	t.Helper()
	target, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatal(err)
	}
	return httptest.NewServer(newInjectedProxy(target))
}

func TestInjectPWATagsAddsTheManifestLinkToARealHTMLPage(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><head><title>ttyd</title></head><body>terminal</body></html>"))
	}))
	defer backend.Close()
	front := proxyFor(t, backend)
	defer front.Close()

	resp, err := http.Get(front.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	page := string(body)

	if !strings.Contains(page, `<link rel="manifest" href="/manifest.json">`) {
		t.Errorf("manifest link missing: %s", page)
	}
	if !strings.Contains(page, "navigator.serviceWorker.register('/sw.js')") {
		t.Errorf("service worker registration missing: %s", page)
	}
	// The original content must still be there, untouched aside from the injection.
	if !strings.Contains(page, "<title>ttyd</title>") || !strings.Contains(page, "terminal") {
		t.Errorf("original page content was lost: %s", page)
	}
	if n := strings.Count(page, "</head>"); n != 1 {
		t.Errorf("expected exactly one </head> after injection, got %d: %s", n, page)
	}
}

func TestInjectPWATagsLeavesNonHTMLResponsesAlone(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer backend.Close()
	front := proxyFor(t, backend)
	defer front.Close()

	resp, err := http.Get(front.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != `{"ok":true}` {
		t.Errorf("a non-HTML response must pass through byte-for-byte: %q", body)
	}
}

func TestInjectPWATagsLeavesAPageWithNoSingleHeadAlone(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body>no head tag here</body></html>"))
	}))
	defer backend.Close()
	front := proxyFor(t, backend)
	defer front.Close()

	resp, err := http.Get(front.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "<html><body>no head tag here</body></html>" {
		t.Errorf("a page with no exactly-one </head> must pass through untouched: %q", body)
	}
}

// A real browser always sends its own Accept-Encoding header -- which is
// exactly what used to make injection silently never fire in production,
// since ttyd compresses whenever that header is present and ModifyResponse
// never rewrites a compressed body. This reproduces that real-world request
// shape against a backend that only compresses when asked to, the same as
// ttyd, and checks the injection still happens.
func TestInjectPWATagsStripsAcceptEncodingSoARealBrowserRequestStillGetsInjected(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		page := "<html><head></head><body>hi</body></html>"
		if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			w.Header().Set("Content-Encoding", "gzip")
			gz := gzip.NewWriter(w)
			_, _ = gz.Write([]byte(page))
			_ = gz.Close()
			return
		}
		_, _ = w.Write([]byte(page))
	}))
	defer backend.Close()
	front := proxyFor(t, backend)
	defer front.Close()

	req, err := http.NewRequest(http.MethodGet, front.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept-Encoding", "gzip, deflate, br") // what a real browser sends
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	page := string(body)

	if resp.Header.Get("Content-Encoding") != "" {
		t.Fatalf("expected an uncompressed response (Accept-Encoding should have been stripped before reaching the backend), got Content-Encoding=%q", resp.Header.Get("Content-Encoding"))
	}
	if !strings.Contains(page, `<link rel="manifest" href="/manifest.json">`) {
		t.Errorf("manifest link missing from a real-browser-shaped request: %s", page)
	}
}

func TestServePWAAssetsManifestLinksTheIconsItActuallyServes(t *testing.T) {
	mux := http.NewServeMux()
	servePWAAssets(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	for _, path := range []string{"/manifest.json", "/sw.js", "/icon-192.png", "/icon-512.png"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s = %d, want 200", path, resp.StatusCode)
		}
	}

	resp, _ := http.Get(srv.URL + "/manifest.json")
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	manifest := string(body)
	if !strings.Contains(manifest, `"display":"standalone"`) {
		t.Errorf("expected standalone display mode: %s", manifest)
	}
	if !strings.Contains(manifest, `"/icon-192.png"`) || !strings.Contains(manifest, `"/icon-512.png"`) {
		t.Errorf("manifest doesn't reference the icons servePWAAssets actually serves: %s", manifest)
	}
}
