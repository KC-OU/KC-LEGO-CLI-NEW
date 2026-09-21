package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

// blEnv points the CLI at a scratch store, fake credentials and a fake BrickLink.
func blEnv(t *testing.T, routes map[string]string) (*lego.DB, *atomic.Int32) {
	t.Helper()
	db := legoEnv(t)
	seedCatalog(t, db)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if !strings.HasPrefix(r.Header.Get("Authorization"), "OAuth ") {
			w.WriteHeader(401)
			w.Write([]byte(`{"meta":{"message":"BAD_OAUTH_REQUEST","code":401}}`))
			return
		}
		if body, ok := routes[r.URL.Path]; ok {
			w.Write([]byte(body))
			return
		}
		w.WriteHeader(404)
		w.Write([]byte(`{"meta":{"message":"RESOURCE_NOT_FOUND","code":404}}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("BRICKLINK_BASE_URL", srv.URL)
	t.Setenv("BRICKLINK_CONSUMER_KEY", "ck")
	t.Setenv("BRICKLINK_CONSUMER_SECRET", "cs")
	t.Setenv("BRICKLINK_TOKEN", "tk")
	t.Setenv("BRICKLINK_TOKEN_SECRET", "ts")
	return db, &hits
}

func ok200(data string) string { return `{"meta":{"code":200,"message":"OK"},"data":` + data + `}` }

var blRoutes = map[string]string{
	"/items/PART/3001":           ok200(`{"no":"3001","name":"Brick 2 x 4","type":"PART","year_released":1958,"image_url":"//img.bricklink.com/x.png"}`),
	"/items/PART/3001/price":     ok200(`{"new_or_used":"U","currency_code":"GBP","min_price":"0.01","max_price":"0.3","avg_price":"0.0587","qty_avg_price":"0.05","unit_quantity":10,"total_quantity":900}`),
	"/items/SET/75192-1/price":   ok200(`{"new_or_used":"U","currency_code":"GBP","avg_price":"420.5","qty_avg_price":"420","unit_quantity":5,"total_quantity":5}`),
	"/items/PART/3023/price":     ok200(`{"new_or_used":"U","currency_code":"GBP","avg_price":"0","unit_quantity":0,"total_quantity":0}`),
	"/items/PART/3001/supersets": ok200(`[{"color_id":5,"entries":[{"item":{"no":"75192-1","name":"Millennium Falcon"},"quantity":10}]}]`),
	"/colors":                    ok200(`[{"color_id":5,"color_name":"Red"},{"color_id":7,"color_name":"Blue"},{"color_id":99,"color_name":"Nonexistent Sparkle"}]`),
}

func TestBrickLinkCommandsEndToEnd(t *testing.T) {
	db, hits := blEnv(t, blRoutes)

	stdout, code := run(t, "bricklink", "test")
	if code != 0 || !strings.Contains(stdout, "accepted") {
		t.Fatalf("test: code=%d %q", code, stdout)
	}
	if stdout, code := run(t, "bricklink", "item", "part", "3001", "--json"); code != 0 || !strings.Contains(stdout, `"name": "Brick 2 x 4"`) {
		t.Fatalf("item: code=%d %q", code, stdout)
	}
	if _, code := run(t, "bricklink", "item", "part", "nope"); code != exitNotFound {
		t.Errorf("unknown item = %d, want %d", code, exitNotFound)
	}
	if _, code := run(t, "bricklink", "item", "gadget", "1"); code != exitUsage {
		t.Errorf("bad type = %d", code)
	}
	if stdout, code := run(t, "bricklink", "colors", "sync"); code != 0 || !strings.Contains(stdout, "2 colours mapped, 1 BrickLink colours have no match") {
		t.Fatalf("colors sync: code=%d %q", code, stdout)
	}
	if bl, ok := db.BLColorFor(4); !ok || bl != 5 {
		t.Errorf("Red (4) should map to BrickLink 5: %d %v", bl, ok)
	}
	stdout, code = run(t, "bricklink", "price", "part", "3001", "--color", "red")
	if code != 0 || !strings.Contains(stdout, "0.0587") || !strings.Contains(stdout, "GBP") {
		t.Fatalf("price: code=%d %q", code, stdout)
	}
	stdout, code = run(t, "bricklink", "price", "part", "3023")
	if code != 0 || !strings.Contains(stdout, "no data, not because it is free") {
		t.Errorf("zero data must be explained, not shown as a bargain: %q", stdout)
	}
	if _, code := run(t, "bricklink", "price", "part", "3001", "--color", "sparkly-nothing"); code != exitUsage {
		t.Errorf("unknown colour = %d", code)
	}
	if stdout, code := run(t, "bricklink", "where-used", "3001", "--color", "red"); code != 0 || !strings.Contains(stdout, "75192-1") {
		t.Errorf("where-used: code=%d %q", code, stdout)
	}
	before := hits.Load()
	run(t, "bricklink", "price", "part", "3001", "--color", "red") // identical: served from cache
	if hits.Load() != before {
		t.Errorf("a repeated price lookup must come from the cache")
	}
	stdout, _ = run(t, "bricklink", "status", "--json")
	if strings.Contains(stdout, "ck") && strings.Contains(stdout, `"ts"`) {
		t.Error("status must not print secrets")
	}
	var st map[string]any
	if err := json.NewDecoder(strings.NewReader(stdout)).Decode(&st); err != nil || st["configured"] != true || st["calls_today"].(float64) < 1 {
		t.Errorf("status = %v %v", st, err)
	}
}

func TestBrickLinkWithoutCredentialsIsAnAuthError(t *testing.T) {
	legoEnv(t)
	t.Setenv("BRICKLINK_CONSUMER_KEY", "")
	stdout, code := run(t, "bricklink", "item", "part", "3001")
	if code != exitAuth || !strings.Contains(stdout, "not set up") {
		t.Errorf("code=%d %q", code, stdout)
	}
}

func TestBadTokenAndBudgetExhaustionMapToExitCodes(t *testing.T) {
	blEnv(t, map[string]string{"/colors": `{"meta":{"message":"TOKEN_IP_MISMATCHED","code":401}}`})
	if stdout, code := run(t, "bricklink", "test"); code != exitAuth || !strings.Contains(stdout, "whoami") {
		t.Errorf("IP mismatch: code=%d %q", code, stdout)
	}
	t.Setenv("BRICKLINK_DAILY_BUDGET", "1")
	blEnv(t, blRoutes)
	t.Setenv("BRICKLINK_DAILY_BUDGET", "1")
	run(t, "bricklink", "item", "part", "3001")
	if _, code := run(t, "bricklink", "price", "part", "3001"); code != exitNetwork {
		t.Errorf("an exhausted budget is a network-class failure, got %d", code)
	}
}

func TestValueAndWatchThroughTheCLI(t *testing.T) {
	db, _ := blEnv(t, blRoutes)
	db.SetBLColors([]lego.BLColorRow{{RBID: 4, BLID: 5, Name: "Red"}})
	_ = db.AddOwnedPart(lego.OwnedPart{PartNum: "3001", Name: "Brick 2 x 4", Category: "Bricks", ColorID: 4, ColorName: "Red", Qty: 100})
	_ = db.AddOwnedPart(lego.OwnedPart{PartNum: "3023", Name: "Plate", Category: "Plates", ColorID: 1, ColorName: "Blue", Qty: 10})

	stdout, code := run(t, "lego", "value")
	if code != 0 || !strings.Contains(stdout, "0 of 2 lines priced") {
		t.Fatalf("value before pricing: code=%d %q", code, stdout)
	}
	stdout, code = run(t, "lego", "value", "--refresh", "10")
	if code != 0 || !strings.Contains(stdout, "1 of 2 lines priced") || !strings.Contains(stdout, "5.87 GBP") || !strings.Contains(stdout, "a floor, not the full value") {
		t.Fatalf("value after pricing: code=%d %q", code, stdout)
	}

	if _, code := run(t, "lego", "watch", "add", "part", "3001", "--color", "red"); code != exitUsage {
		t.Errorf("--max is required: %d", code)
	}
	if _, code := run(t, "lego", "watch", "add", "part", "3001", "--color", "red", "--max", "0.06"); code != 0 {
		t.Fatalf("watch add failed: %d", code)
	}
	stdout, code = run(t, "lego", "watch", "check")
	if code != 0 || !strings.Contains(stdout, "3001 Red: 0.0587 GBP (your limit 0.0600)") {
		t.Fatalf("watch check: code=%d %q", code, stdout)
	}
	stdout, _ = run(t, "lego", "watch", "check")
	if strings.Contains(stdout, "0.0587 GBP") {
		t.Errorf("the same price must not alert twice: %q", stdout)
	}
	if stdout, _ := run(t, "lego", "watch", "list"); !strings.Contains(stdout, "3001") {
		t.Errorf("list: %q", stdout)
	}
	if _, code := run(t, "lego", "watch", "rm", "1"); code != 0 {
		t.Errorf("rm failed")
	}
	if _, code := run(t, "lego", "watch", "rm", "1"); code != exitNotFound {
		t.Errorf("removing a missing watch = %d", code)
	}
}
