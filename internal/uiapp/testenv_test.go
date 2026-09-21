package uiapp

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/audit"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/auth"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/partdb"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/partdb/partdbtest"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/wmsdb"
)

// testFakeToken is the bearer token the fake Part-DB API accepts.
const testFakeToken = "test-token"

// newTestEnv builds an App that can touch nothing real: a scratch Part-DB with
// the live schema behind a fake REST API, a scratch lego.db, and temp settings,
// 2FA and audit files. The ModernWMS client points at a container that does not
// exist, so its calls fail fast instead of reaching the live warehouse. A test
// that ends up on a live path fails rather than skips.
func newTestEnv(t *testing.T) (*App, *partdbtest.Fake) {
	t.Helper()
	dir := t.TempDir()
	dbPath := partdbtest.New(t)
	partdbtest.Guard(t, dbPath)
	fake := partdbtest.NewFake(t, dbPath, testFakeToken)

	t.Setenv(config.PartDBDBPath, dbPath)
	t.Setenv(config.PartDBAPIURL, fake.URL())
	t.Setenv(config.PartDBAPIToken, testFakeToken)
	t.Setenv(config.SettingsFile, filepath.Join(dir, "settings.json"))
	t.Setenv(config.TwoFAFile, filepath.Join(dir, "2fa.json"))
	t.Setenv(config.AuditLogFile, filepath.Join(dir, "audit.log"))
	t.Setenv(config.LegoDBPath, filepath.Join(dir, "lego.db"))
	t.Setenv(config.ImageDir, filepath.Join(dir, "imgcache"))
	t.Setenv(config.PluginDir, filepath.Join(dir, "plugins"))
	t.Setenv(config.RebrickableAPIKey, "")
	// Nothing in a test may reach a real service, whatever the machine's environment holds.
	t.Setenv("REBRICKABLE_BASE_URL", "http://127.0.0.1:1")
	t.Setenv("BRICKLINK_BASE_URL", "http://127.0.0.1:1")
	for _, k := range []string{config.BricklinkConsumerKey, config.BricklinkConsumerSecret, config.BricklinkToken, config.BricklinkTokenSecret} {
		t.Setenv(k, "")
	}

	pdb, err := partdb.Open("")
	if err != nil {
		t.Fatalf("opening the scratch Part-DB: %v", err)
	}
	t.Cleanup(func() { pdb.Close() })
	legoDB, err := lego.Open("")
	if err != nil {
		t.Fatalf("opening scratch lego db: %v", err)
	}
	t.Cleanup(func() { legoDB.Close() })

	wms := &wmsdb.Client{Container: "no-such-container", DBPath: "/nonexistent.db", Timeout: 5 * time.Second}
	app := NewApp(wms, pdb, legoDB, audit.New(), false, false)
	app.rebrick = &lego.Client{} // no key, no network; tests that need Rebrickable set a fake
	app.hooksSync = true
	app.images.Offline = true // pictures only from the cache; tests seed it
	app.session = &auth.Session{
		Source: "modernwms", Username: "admin", Role: "admin",
		Permissions: &wmsdb.Permissions{IsAdmin: true, CanWrite: true, Menus: []string{"*"}},
	}
	app.authed = true
	return app, fake
}
