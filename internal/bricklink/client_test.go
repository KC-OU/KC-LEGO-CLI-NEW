package bricklink

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// RFC 5849 section 3.4.1: the normative signature-base-string example.
func TestSignatureBaseMatchesRFC5849(t *testing.T) {
	u, _ := url.Parse("http://example.com/request?b5=%3D%253D&a3=a&c%40=&a2=r%20b")
	params := [][2]string{
		{"b5", "=%3D"}, {"a3", "a"}, {"c@", ""}, {"a2", "r b"},
		{"c2", ""}, {"a3", "2 q"}, // from the form body
		{"oauth_consumer_key", "9djdj82h48djs9d2"}, {"oauth_token", "kkk9d7dh3k39sjv7"},
		{"oauth_signature_method", "HMAC-SHA1"}, {"oauth_timestamp", "137131201"}, {"oauth_nonce", "7d8f3e4a"},
	}
	got := signatureBase("POST", u, params)
	want := "POST&http%3A%2F%2Fexample.com%2Frequest&a2%3Dr%2520b%26a3%3D2%2520q%26a3%3Da%26b5%3D%253D%25253D%26c%2540%3D%26c2%3D%26oauth_consumer_key%3D9djdj82h48djs9d2%26oauth_nonce%3D7d8f3e4a%26oauth_signature_method%3DHMAC-SHA1%26oauth_timestamp%3D137131201%26oauth_token%3Dkkk9d7dh3k39sjv7"
	if got != want {
		t.Fatalf("base string\n got %s\nwant %s", got, want)
	}
}

// The signing example published in Twitter's OAuth 1.0a documentation (HMAC-SHA1,
// known secrets): a fixed input with a known correct signature.
func TestSignatureMatchesAKnownAnswer(t *testing.T) {
	u, _ := url.Parse("https://api.twitter.com/1.1/statuses/update.json?include_entities=true")
	params := [][2]string{
		{"include_entities", "true"},
		{"status", "Hello Ladies + Gentlemen, a signed OAuth request!"},
		{"oauth_consumer_key", "xvz1evFS4wEEPTGEFPHBog"},
		{"oauth_nonce", "kYjzVBB8Y0ZFabxSWbWovY3uYSQ2pTgmZeNu2VS4cg"},
		{"oauth_signature_method", "HMAC-SHA1"},
		{"oauth_timestamp", "1318622958"},
		{"oauth_token", "370773112-GmHxMAgYyLbNEtIKZeRNFsMKPR9EyMZeS9weJAEb"},
		{"oauth_version", "1.0"},
	}
	got := sign(signatureBase("POST", u, params), "kAcSOqF21Fu85e7zjz7ZN2U4ZRhfV3WpwPAoE3Z7kBw", "LswwdoUaIvS8ltyTt5jkRh4J50vUPVVHtR2YPi5kE")
	if got != "hCtSmYh+iHYCEqBWrE7C7hYmtUk=" {
		t.Fatalf("signature = %s", got)
	}
}

func TestPercentEncodingIsRFC3986(t *testing.T) {
	for in, want := range map[string]string{"a b": "a%20b", "~-._": "~-._", "é": "%C3%A9", "+": "%2B", "/": "%2F", "": ""} {
		if got := pct(in); got != want {
			t.Errorf("pct(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAuthHeaderCarriesEverythingButNotTheSecrets(t *testing.T) {
	u, _ := url.Parse("https://api.bricklink.com/api/store/v1/colors?x=1")
	h := authHeader("GET", u, Credentials{"ck", "SECRET-C", "tk", "SECRET-T"}, time.Unix(1700000000, 0), "n0nce")
	for _, want := range []string{`oauth_consumer_key="ck"`, `oauth_token="tk"`, `oauth_signature_method="HMAC-SHA1"`, `oauth_timestamp="1700000000"`, `oauth_nonce="n0nce"`, `oauth_version="1.0"`, `oauth_signature="`} {
		if !strings.Contains(h, want) {
			t.Errorf("header lacks %s: %s", want, h)
		}
	}
	if strings.Contains(h, "SECRET") {
		t.Errorf("a secret leaked into the header: %s", h)
	}
}

// ---- fake store and server ----

type memStore struct {
	mu    sync.Mutex
	cache map[string]struct {
		status int
		body   []byte
		at     time.Time
	}
	used  map[string]int
	slots int
}

func newMemStore() *memStore {
	return &memStore{cache: map[string]struct {
		status int
		body   []byte
		at     time.Time
	}{}, used: map[string]int{}}
}

func (m *memStore) APICacheGet(key string, ttl time.Duration) (int, []byte, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.cache[key]
	if !ok || time.Since(e.at) > ttl {
		return 0, nil, false
	}
	return e.status, e.body, true
}
func (m *memStore) APICachePut(key string, status int, body []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cache[key] = struct {
		status int
		body   []byte
		at     time.Time
	}{status, body, time.Now()}
}
func (m *memStore) SpendAPIBudget(name string, limit int, now time.Time) (int, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := name + now.UTC().Format("2006-01-02")
	if m.used[k] >= limit {
		return m.used[k], false, nil
	}
	m.used[k]++
	return m.used[k], true, nil
}
func (m *memStore) ReserveAPISlot(string, time.Duration) (time.Duration, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.slots++
	return 0, nil
}

var testCreds = Credentials{"ck", "cs", "tk", "ts"}

type fakeBL struct {
	*httptest.Server
	mu   sync.Mutex
	hits []string
	auth []string
}

func newFake(t *testing.T, routes map[string]string) *fakeBL {
	t.Helper()
	f := &fakeBL{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.hits = append(f.hits, r.URL.RequestURI())
		f.auth = append(f.auth, r.Header.Get("Authorization"))
		f.mu.Unlock()
		body, ok := routes[r.URL.Path]
		if !ok {
			w.WriteHeader(404)
			w.Write([]byte(`{"meta":{"description":"Resource not found","message":"RESOURCE_NOT_FOUND","code":404}}`))
			return
		}
		w.Write([]byte(body))
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeBL) client(store Store) *Client {
	c := New(testCreds, store)
	c.BaseURL = f.URL + "/api/store/v1"
	c.HTTP = f.Client()
	return c
}

func envelope200(data string) string {
	return `{"meta":{"description":"OK","message":"OK","code":200},"data":` + data + `}`
}

func TestItemPriceColoursAndSupersets(t *testing.T) {
	f := newFake(t, map[string]string{
		"/api/store/v1/items/PART/3001":           envelope200(`{"no":"3001","name":"Brick 2 x 4","type":"PART","category_id":5,"year_released":1958,"weight":"2.32","is_obsolete":false}`),
		"/api/store/v1/items/PART/3001/price":     envelope200(`{"new_or_used":"U","currency_code":"GBP","min_price":"0.0100","max_price":"0.4","avg_price":"0.0587","qty_avg_price":"0.0501","unit_quantity":120,"total_quantity":9000}`),
		"/api/store/v1/items/PART/3001/colors":    envelope200(`[{"color_id":5,"quantity":1200},{"color_id":11,"quantity":900}]`),
		"/api/store/v1/items/PART/3001/supersets": envelope200(`[{"color_id":5,"entries":[{"item":{"no":"75192-1","name":"Millennium Falcon","type":"SET"},"quantity":10,"appears_as":"R"}]}]`),
		"/api/store/v1/items/SET/75192-1":         envelope200(`{"no":"75192-1","name":"Millennium Falcon","type":"SET","year_released":2017}`),
	})
	c := f.client(newMemStore())
	ctx := context.Background()

	it, err := c.GetItem(ctx, Part, "3001")
	if err != nil || it.Name != "Brick 2 x 4" || it.YearReleased != 1958 || it.Weight != 2.32 {
		t.Fatalf("item = %+v %v", it, err)
	}
	pr, err := c.PriceGuide(ctx, Part, "3001", 5, Sold, "U")
	if err != nil || pr.Avg != 0.0587 || pr.Min != 0.01 || pr.Max != 0.4 || pr.TotalQuantity != 9000 || pr.CurrencyCode != "GBP" {
		t.Fatalf("price = %+v %v", pr, err)
	}
	q := f.hits[len(f.hits)-1]
	for _, want := range []string{"color_id=5", "guide_type=sold", "new_or_used=U", "currency_code=GBP", "region=europe"} {
		if !strings.Contains(q, want) {
			t.Errorf("price request %q lacks %s", q, want)
		}
	}
	cols, err := c.KnownColors(ctx, Part, "3001")
	if err != nil || len(cols) != 2 || cols[0].ColorID != 5 {
		t.Fatalf("colours = %+v %v", cols, err)
	}
	sup, err := c.Supersets(ctx, Part, "3001", 5)
	if err != nil || len(sup) != 1 || sup[0].Entries[0].Item.No != "75192-1" || sup[0].Entries[0].Quantity != 10 {
		t.Fatalf("supersets = %+v %v", sup, err)
	}
	if set, err := c.GetItem(ctx, Set, "75192"); err != nil || set.No != "75192-1" { // the -1 suffix is added
		t.Fatalf("set = %+v %v", set, err)
	}
	for _, a := range f.auth {
		if !strings.HasPrefix(a, "OAuth ") || !strings.Contains(a, `oauth_consumer_key="ck"`) {
			t.Errorf("every request is signed: %q", a)
		}
	}
}

func TestErrorsAreTyped(t *testing.T) {
	f := newFake(t, map[string]string{
		"/api/store/v1/items/PART/ipmismatch": `{"meta":{"description":"Token is registered for a different IP","message":"TOKEN_IP_MISMATCHED","code":401}}`,
		"/api/store/v1/items/PART/badsig":     `{"meta":{"description":"bad","message":"BAD_OAUTH_REQUEST","code":401}}`,
		"/api/store/v1/items/PART/limited":    `{"meta":{"description":"slow down","message":"RATE_LIMIT_EXCEEDED","code":429}}`,
		"/api/store/v1/items/PART/odd":        `{"meta":{"description":"weird","message":"SOMETHING_ELSE","code":500}}`,
		"/api/store/v1/items/PART/html":       `<html>gateway</html>`,
	})
	c := f.client(nil)
	ctx := context.Background()
	cases := map[string]error{"ipmismatch": ErrIPMismatch, "badsig": ErrAuth, "limited": ErrRateLimited, "missing": ErrNotFound}
	for no, want := range cases {
		if _, err := c.GetItem(ctx, Part, no); !errors.Is(err, want) {
			t.Errorf("%s: %v, want %v", no, err, want)
		}
	}
	var api *APIError
	if _, err := c.GetItem(ctx, Part, "odd"); !errors.As(err, &api) || api.Code != 500 || api.Message != "SOMETHING_ELSE" {
		t.Errorf("odd: %v", err)
	}
	if _, err := c.GetItem(ctx, Part, "html"); err == nil || !strings.Contains(err.Error(), "not JSON") {
		t.Errorf("html: %v", err)
	}
	if _, err := New(Credentials{}, nil).GetItem(ctx, Part, "3001"); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("no credentials: %v", err)
	}
	if !errors.Is(ErrIPMismatch, ErrIPMismatch) || !strings.Contains(ErrIPMismatch.Error(), "whoami") {
		t.Error("the IP error should point at `wms bricklink whoami`")
	}
}

func TestCacheAndDailyBudget(t *testing.T) {
	f := newFake(t, map[string]string{"/api/store/v1/items/PART/3001": envelope200(`{"no":"3001","name":"Brick"}`)})
	st := newMemStore()
	c := f.client(st)
	c.DailyBudget = 3
	ctx := context.Background()
	for i := 0; i < 5; i++ { // five identical lookups cost one call
		if _, err := c.GetItem(ctx, Part, "3001"); err != nil {
			t.Fatal(err)
		}
	}
	if len(f.hits) != 1 {
		t.Fatalf("cached answers must not hit the API: %d hits", len(f.hits))
	}
	// a missing item is cached too (briefly), so a typo doesn't cost a call each time
	for i := 0; i < 3; i++ {
		if _, err := c.GetItem(ctx, Part, "nope"); !errors.Is(err, ErrNotFound) {
			t.Fatal(err)
		}
	}
	if len(f.hits) != 2 {
		t.Fatalf("a repeated 404 is served from cache: %d hits", len(f.hits))
	}
	// the budget (3) is hit on the 4th distinct call: refused WITHOUT a request
	for _, no := range []string{"a", "b"} {
		c.GetItem(ctx, Part, no)
	}
	before := len(f.hits)
	if _, err := c.GetItem(ctx, Part, "c"); !errors.Is(err, ErrBudget) {
		t.Fatalf("want ErrBudget, got %v", err)
	}
	if len(f.hits) != before {
		t.Error("an over-budget call must not reach BrickLink")
	}
	if _, err := c.GetItem(ctx, Part, "3001"); err != nil {
		t.Errorf("cached answers still work when the budget is spent: %v", err)
	}
}

func TestNeverCachesErrorsThatAreNotFacts(t *testing.T) {
	f := newFake(t, map[string]string{"/api/store/v1/items/PART/x": `{"meta":{"message":"TOKEN_IP_MISMATCHED","code":401}}`})
	st := newMemStore()
	c := f.client(st)
	c.GetItem(context.Background(), Part, "x")
	c.GetItem(context.Background(), Part, "x")
	if len(f.hits) != 2 {
		t.Errorf("an auth/IP error must be retried, not cached: %d hits", len(f.hits))
	}
}

func TestInputsAreValidatedBeforeAnyRequest(t *testing.T) {
	f := newFake(t, nil)
	c := f.client(nil)
	ctx := context.Background()
	for _, no := range []string{"", "  ", "a/b", "a?b", `a\b`, "a#b"} {
		if _, err := c.GetItem(ctx, Part, no); err == nil {
			t.Errorf("%q must be rejected", no)
		}
	}
	if _, err := c.PriceGuide(ctx, Part, "3001", 5, Sold, "X"); err == nil {
		t.Error("bad condition")
	}
	if _, err := c.PriceGuide(ctx, Part, "3001", 5, "avg", "N"); err == nil {
		t.Error("bad guide")
	}
	if len(f.hits) != 0 {
		t.Errorf("invalid input reached the network: %v", f.hits)
	}
}

func TestPingBypassesTheCache(t *testing.T) {
	f := newFake(t, map[string]string{"/api/store/v1/colors": envelope200(`[{"color_id":1,"color_name":"White","color_code":"FFFFFF","color_type":"Solid"}]`)})
	st := newMemStore()
	c := f.client(st)
	c.Colors(context.Background()) // warms the cache
	for i := 0; i < 2; i++ {
		if err := c.Ping(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if len(f.hits) != 3 {
		t.Errorf("Ping must always make a real call: %d hits", len(f.hits))
	}
}

func TestConnectionErrorsDoNotLeakTheURL(t *testing.T) {
	dead := httptest.NewServer(nil)
	url := dead.URL
	dead.Close()
	c := New(testCreds, nil)
	c.BaseURL = url + "/secret-path"
	_, err := c.GetItem(context.Background(), Part, "3001")
	if err == nil || strings.Contains(err.Error(), "secret-path") {
		t.Errorf("error = %v", err)
	}
}
