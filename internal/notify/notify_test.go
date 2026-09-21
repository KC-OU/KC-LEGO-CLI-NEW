package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/audit"
)

type seen struct {
	header http.Header
	body   string
}

func capture(t *testing.T, status int) (*httptest.Server, *[]seen) {
	t.Helper()
	var got []seen
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = append(got, seen{r.Header.Clone(), string(b)})
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func TestNtfyGetsPlainBodyAndHeaders(t *testing.T) {
	srv, got := capture(t, 200)
	s := &Sender{URL: srv.URL + "/mytopic", Format: "ntfy"}
	if err := s.Send(context.Background(), Message{Title: "Backup is stale", Body: "9 days old", Priority: 4, Tag: "floppy_disk"}); err != nil {
		t.Fatal(err)
	}
	g := (*got)[0]
	if g.body != "9 days old" || g.header.Get("Title") != "Backup is stale" || g.header.Get("Priority") != "4" || g.header.Get("Tags") != "floppy_disk" {
		t.Errorf("ntfy request = %+v", g)
	}
}

func TestWebhookGetsOneJSONObjectSlackAndDiscordCanRead(t *testing.T) {
	srv, got := capture(t, 204)
	s := &Sender{URL: srv.URL, Format: "json"}
	if err := s.Send(context.Background(), Message{Title: "T", Body: "B", Priority: 3}); err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte((*got)[0].body), &doc); err != nil || doc["title"] != "T" || doc["message"] != "B" || doc["text"] != "T\nB" || doc["content"] != "T\nB" {
		t.Errorf("webhook body = %q (%v)", (*got)[0].body, err)
	}
	if (*got)[0].header.Get("Content-Type") != "application/json" {
		t.Error("content type")
	}
}

func TestSendFailuresAreReportedWithoutLeakingTheURL(t *testing.T) {
	srv, _ := capture(t, 500)
	if err := (&Sender{URL: srv.URL + "/secret-topic", Format: "json"}).Send(context.Background(), Message{Title: "x"}); err == nil || strings.Contains(err.Error(), "secret-topic") {
		t.Errorf("a 5xx must be an error that does not repeat the URL: %v", err)
	}
	dead := httptest.NewServer(nil)
	url := dead.URL + "/secret-topic"
	dead.Close()
	err := (&Sender{URL: url, Format: "json"}).Send(context.Background(), Message{Title: "x"})
	if err == nil || strings.Contains(err.Error(), "secret-topic") {
		t.Errorf("a connection failure must not repeat the URL: %v", err)
	}
}

func TestOnlyHTTPURLsAndKnownFormatsAreAccepted(t *testing.T) {
	for _, u := range []string{"file:///etc/passwd", "ftp://x/y", "javascript:alert(1)", "", "http://"} {
		if err := (&Sender{URL: u, Format: "json"}).Validate(); err == nil {
			t.Errorf("%q must be rejected", u)
		}
	}
	if err := (&Sender{URL: "https://ntfy.sh/x", Format: "carrier-pigeon"}).Validate(); err == nil {
		t.Error("unknown format must be rejected")
	}
}

func TestFromConfigIsOffByDefaultAndPicksNtfyByHost(t *testing.T) {
	t.Setenv("NOTIFY_URL", "")
	t.Setenv("WMS_SETTINGS_FILE", filepath.Join(t.TempDir(), "s.json"))
	if FromConfig() != nil {
		t.Fatal("alerts must be off unless NOTIFY_URL is set")
	}
	t.Setenv("NOTIFY_FORMAT", "")
	t.Setenv("NOTIFY_URL", "https://ntfy.sh/example-topic")
	if s := FromConfig(); s == nil || s.Format != "ntfy" {
		t.Errorf("ntfy host should select ntfy: %+v", s)
	}
	t.Setenv("NOTIFY_URL", "https://hooks.example.com/abc")
	if s := FromConfig(); s.Format != "json" {
		t.Errorf("other hosts get json: %+v", s)
	}
	t.Setenv("NOTIFY_FORMAT", "ntfy")
	if s := FromConfig(); s.Format != "ntfy" {
		t.Errorf("NOTIFY_FORMAT wins: %+v", s)
	}
}

// ---- monitor ----

type rig struct {
	m     *Monitor
	sent  *[]Message
	clock *time.Time
	log   *audit.Logger
	dir   string
}

func newRig(t *testing.T) *rig {
	t.Helper()
	var sent []Message
	clock := time.Now()
	dir := t.TempDir()
	r := &rig{sent: &sent, clock: &clock, dir: dir, log: &audit.Logger{Path: filepath.Join(dir, "audit.log")}}
	r.m = NewMonitor(func(_ context.Context, m Message) error { sent = append(sent, m); return nil }, r.log, dir)
	r.m.Now = func() time.Time { return clock }
	fresh := filepath.Join(dir, "modernwms_db_20260920_120000.db")
	os.WriteFile(fresh, []byte("x"), 0600)
	os.Chtimes(fresh, clock, clock)
	return r
}

func (r *rig) titles() string {
	var ts []string
	for _, m := range *r.sent {
		ts = append(ts, m.Title)
	}
	return strings.Join(ts, "|")
}

func TestQuietWhenNothingIsWrong(t *testing.T) {
	r := newRig(t)
	r.log.Log("admin", "Admin", "LOGIN_MODERNWMS", "SUCCESS", "")
	r.m.Check(context.Background())
	if len(*r.sent) != 0 {
		t.Fatalf("no alert expected, got %s", r.titles())
	}
}

func TestFailedLoginBurstAlertsOnceThenCoolsDown(t *testing.T) {
	r := newRig(t)
	for i := 0; i < 10; i++ {
		r.log.Log(fmt.Sprintf("bot%d", i), "", "LOGIN", "FAILED_INVALID_CREDENTIALS", "invalid username or password")
	}
	r.m.Check(context.Background())
	r.m.Check(context.Background())
	if r.titles() != "Failed sign-ins" {
		t.Fatalf("want exactly one burst alert, got %q", r.titles())
	}
	if (*r.sent)[0].Priority < 4 || strings.Contains((*r.sent)[0].Body, "bot1") {
		t.Errorf("message = %+v", (*r.sent)[0])
	}
	*r.clock = r.clock.Add(61 * time.Minute)
	r.m.Check(context.Background())
	if len(*r.sent) != 1 { // the burst has aged out of the window, so nothing new
		t.Fatalf("an old burst must not alert again: %s", r.titles())
	}
}

func TestNineFailuresIsNotABurst(t *testing.T) {
	r := newRig(t)
	for i := 0; i < 9; i++ {
		r.log.Log("bot", "", "LOGIN", "FAILED_INVALID_CREDENTIALS", "")
	}
	r.m.Check(context.Background())
	if len(*r.sent) != 0 {
		t.Fatalf("got %s", r.titles())
	}
}

func TestBrokenChainAndStaleBackupAlert(t *testing.T) {
	r := newRig(t)
	r.log.Log("a", "r", "X", "S", "one")
	r.log.Log("a", "r", "X", "S", "two")
	data, _ := os.ReadFile(r.log.Path)
	os.WriteFile(r.log.Path, []byte(strings.Replace(string(data), "DETAILS:one", "DETAILS:ONE", 1)), 0600)
	*r.clock = r.clock.Add(8 * 24 * time.Hour) // and the backup is now 8 days old
	r.m.Check(context.Background())
	if r.titles() != "Audit log was changed|Backup is stale" {
		t.Fatalf("alerts = %q", r.titles())
	}
	if (*r.sent)[0].Priority != 5 {
		t.Error("a tampered audit log is the highest priority")
	}
}

func TestNoBackupAtAllAlerts(t *testing.T) {
	r := newRig(t)
	os.Remove(filepath.Join(r.dir, "modernwms_db_20260920_120000.db"))
	r.m.Check(context.Background())
	if r.titles() != "No backups" {
		t.Fatalf("alerts = %q", r.titles())
	}
}

func TestLowStockIsListedAndCapped(t *testing.T) {
	r := newRig(t)
	r.m.LowStock = func() []string {
		var l []string
		for i := 0; i < 20; i++ {
			l = append(l, fmt.Sprintf("part %d: 1 (min 5)", i))
		}
		return l
	}
	r.m.Check(context.Background())
	if r.titles() != "Low stock: 20 part(s)" || !strings.Contains((*r.sent)[0].Body, "and 5 more") {
		t.Fatalf("alerts = %q body=%q", r.titles(), (*r.sent)[0].Body)
	}
}

func TestHeartbeatCarriesTheChainHead(t *testing.T) {
	r := newRig(t)
	r.log.Log("a", "r", "X", "S", "")
	head, _ := r.log.Verify()
	r.m.Heartbeat(context.Background())
	if len(*r.sent) != 1 || !strings.Contains((*r.sent)[0].Body, head.Head) {
		t.Fatalf("heartbeat = %+v want head %s", *r.sent, head.Head)
	}
}

func TestAFailingEndpointNeverPanicsTheMonitor(t *testing.T) {
	r := newRig(t)
	r.m.Send = func(context.Context, Message) error { return fmt.Errorf("endpoint down") }
	os.Remove(filepath.Join(r.dir, "modernwms_db_20260920_120000.db"))
	r.m.Check(context.Background())
	r.m.Heartbeat(context.Background())
}

func TestPriceDropsAlertOnceADayAndOnlyWhenThereAreHits(t *testing.T) {
	r := newRig(t)
	calls := 0
	r.m.PriceDrops = func(context.Context) []string {
		calls++
		if calls == 1 {
			return nil // nothing at the limit
		}
		return []string{"3001 Red: 0.0550 GBP (your limit 0.0600)"}
	}
	r.m.Check(context.Background())
	r.m.Check(context.Background()) // within a day: not asked again
	if calls != 1 || len(*r.sent) != 0 {
		t.Fatalf("calls=%d sent=%s", calls, r.titles())
	}
	*r.clock = r.clock.Add(25 * time.Hour)
	os.Chtimes(filepath.Join(r.dir, "modernwms_db_20260920_120000.db"), *r.clock, *r.clock) // keep the backup fresh
	r.m.Check(context.Background())
	if calls != 2 || r.titles() != "Price drop: 1 item(s)" || !strings.Contains((*r.sent)[0].Body, "3001 Red") {
		t.Fatalf("calls=%d alerts=%q", calls, r.titles())
	}
}
