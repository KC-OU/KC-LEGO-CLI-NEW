package notify

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

func TestProviderFileRoutesEventsToChannels(t *testing.T) {
	var mu sync.Mutex
	got := map[string][]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		got[r.URL.Path] = append(got[r.URL.Path], string(b))
		mu.Unlock()
		w.WriteHeader(200)
	}))
	defer srv.Close()
	dir := t.TempDir()
	cfg := filepath.Join(dir, "provider-config.yaml")
	os.WriteFile(cfg, []byte(`custom:
  - id: whatsapp
    custom_webhook_url: `+srv.URL+`/whatsapp
    custom_method: POST
    custom_format: '{"text":{{dataJsonString}}}'
    custom_headers:
      Content-Type: application/json
  - id: ops
    custom_webhook_url: `+srv.URL+`/ops
    custom_method: POST
    custom_format: '{{data}}'
`), 0o600)
	t.Setenv(config.NotifyProviders, cfg)
	t.Setenv(config.NotifyURL, "")
	t.Setenv(config.AccessFile, filepath.Join(dir, "access.json"))

	chans, err := Channels()
	if err != nil || len(chans) != 2 || chans[0].ID != "ops" || chans[1].ID != "whatsapp" {
		t.Fatalf("channels %+v %v", chans, err)
	}
	// No route: every channel.
	Dispatch("order_shipped", "Order #3 has shipped", "Royal Mail AB123")
	// Route set_complete to WhatsApp only.
	if _, err := access.Update(func(p *access.Policy) error {
		p.Settings.NotifyRoutes = map[string][]string{"set_complete": {"whatsapp"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	Dispatch("set_complete", "Set 75192-1 is complete", "")
	mu.Lock()
	defer mu.Unlock()
	if len(got["/ops"]) != 1 || len(got["/whatsapp"]) != 2 {
		t.Fatalf("deliveries: %v", got)
	}
	if !strings.Contains(got["/whatsapp"][1], `"text":"Set 75192-1 is complete"`) {
		t.Errorf("whatsapp body: %s", got["/whatsapp"][1])
	}
}

func TestBrokenProviderFileFallsBackToNtfy(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "provider-config.yaml")
	os.WriteFile(cfg, []byte("custom: [this is: not valid"), 0o600)
	t.Setenv(config.NotifyProviders, cfg)
	t.Setenv(config.NotifyURL, "https://ntfy.sh/kc-test")
	t.Setenv(config.AccessFile, filepath.Join(dir, "access.json"))
	chans, err := Channels()
	if err == nil || len(chans) != 1 || chans[0].ID != "ntfy" {
		t.Fatalf("want the ntfy fallback and an error: %+v %v", chans, err)
	}
}
