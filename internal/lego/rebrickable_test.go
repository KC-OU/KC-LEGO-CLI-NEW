package lego

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

func openScratchDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "lego.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestClientEnabled(t *testing.T) {
	c := &Client{APIKey: ""}
	if c.Enabled() {
		t.Error("expected Enabled() false with no API key")
	}
	c.APIKey = "abc"
	if !c.Enabled() {
		t.Error("expected Enabled() true with an API key set")
	}
}

func TestSearchSetsAndParts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "key testkey" {
			t.Errorf("unexpected Authorization header: %q", got)
		}
		switch {
		case strings.HasPrefix(r.URL.Path, "/sets/"):
			w.Write([]byte(`{"results":[{"set_num":"71788-1","name":"Test Set","year":2023,"num_parts":56}]}`))
		case strings.HasPrefix(r.URL.Path, "/parts/"):
			w.Write([]byte(`{"results":[{"part_num":"3001","name":"Brick 2x4","part_cat_id":11}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := &Client{APIKey: "testkey", BaseURL: srv.URL, HTTP: srv.Client()}

	sets, err := c.SearchSets(context.Background(), "test")
	if err != nil {
		t.Fatalf("SearchSets: %v", err)
	}
	if len(sets) != 1 || sets[0].SetNum != "71788-1" {
		t.Fatalf("unexpected sets: %+v", sets)
	}

	parts, err := c.SearchParts(context.Background(), "brick")
	if err != nil {
		t.Fatalf("SearchParts: %v", err)
	}
	if len(parts) != 1 || parts[0].PartNum != "3001" {
		t.Fatalf("unexpected parts: %+v", parts)
	}
}

func TestSearchSetsErrorOnNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := &Client{APIKey: "testkey", BaseURL: srv.URL, HTTP: srv.Client()}
	if _, err := c.SearchSets(context.Background(), "x"); err == nil {
		t.Error("expected an error on a non-200 response, not a panic or silent empty result")
	}
}

// fakeAPI answers a fixed set of Rebrickable paths and counts nothing else.
func fakeAPI(t *testing.T, routes map[string]string, status map[string]int) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if st, ok := status[r.URL.Path]; ok {
			w.WriteHeader(st)
			_, _ = w.Write([]byte(`{"detail":"x"}`))
			return
		}
		body, ok := routes[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return &Client{APIKey: "k", BaseURL: srv.URL, HTTP: srv.Client()}
}

func TestGetSetAddsVersionSuffixAndResolvesThemePath(t *testing.T) {
	c := fakeAPI(t, map[string]string{
		"/sets/75344-1/": `{"set_num":"75344-1","name":"Boba Fett's Starship Microfighter","year":2023,"theme_id":30,"num_parts":80}`,
		"/themes/30/":    `{"id":30,"parent_id":10,"name":"The Book of Boba Fett"}`,
		"/themes/10/":    `{"id":10,"parent_id":null,"name":"Star Wars"}`,
	}, nil)

	d, err := c.GetSet(context.Background(), "75344") // typed without "-1"
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "Boba Fett's Starship Microfighter" || d.Year != 2023 || d.Pieces != 80 {
		t.Fatalf("unexpected detail %+v", d)
	}
	theme, err := c.ThemePath(context.Background(), d.ThemeID)
	if err != nil || theme != "Star Wars - The Book of Boba Fett" {
		t.Fatalf("theme = %q, err = %v; want the legacy 'Parent - Child' style", theme, err)
	}
}

func TestGetPartAndCategory(t *testing.T) {
	c := fakeAPI(t, map[string]string{
		"/parts/3001/":         `{"part_num":"3001","name":"Brick 2 x 4","part_cat_id":11}`,
		"/part_categories/11/": `{"id":11,"name":"Bricks"}`,
	}, nil)
	p, err := c.GetPart(context.Background(), " 3001 ")
	if err != nil || p.Name != "Brick 2 x 4" || p.CatID != 11 {
		t.Fatalf("part = %+v, err = %v", p, err)
	}
	if cat, err := c.PartCategory(context.Background(), p.CatID); err != nil || cat != "Bricks" {
		t.Fatalf("category = %q, err = %v", cat, err)
	}
}

func TestLookupErrorsAreDistinguishable(t *testing.T) {
	c := fakeAPI(t, nil, map[string]int{
		"/sets/1-1/": http.StatusNotFound,
		"/sets/2-1/": http.StatusUnauthorized,
		"/sets/3-1/": http.StatusTooManyRequests,
		"/sets/4-1/": http.StatusInternalServerError,
	})
	for set, want := range map[string]error{"1": ErrNotFound, "2": ErrInvalidKey, "3": ErrRateLimited} {
		if _, err := c.GetSet(context.Background(), set); !errors.Is(err, want) {
			t.Errorf("set %s: got %v, want %v", set, err, want)
		}
	}
	if _, err := c.GetSet(context.Background(), "4"); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("a 500 must be a plain error, got %v", err)
	}
	if _, err := c.GetSet(context.Background(), "  "); !errors.Is(err, ErrNotFound) {
		t.Errorf("a blank number must fail without a request, got %v", err)
	}
}

func TestThemePathKeepsWhatItResolved(t *testing.T) {
	// Parent lookup fails (e.g. rate limit): keep the child's own name rather than nothing.
	c := fakeAPI(t, map[string]string{"/themes/30/": `{"id":30,"parent_id":10,"name":"Riding Cycle"}`},
		map[string]int{"/themes/10/": http.StatusTooManyRequests})
	if got, err := c.ThemePath(context.Background(), 30); err != nil || got != "Riding Cycle" {
		t.Fatalf("got %q, err %v", got, err)
	}
	if _, err := c.ThemePath(context.Background(), 99); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an unknown theme with nothing resolved should report it, got %v", err)
	}
}

func TestExactLookups(t *testing.T) {
	db := openScratchDB(t)
	if _, err := db.Exec(`INSERT INTO ref_sets (set_num, name, year, theme, total_pieces) VALUES ('71788-1','Street Bike','2023','Ninjago Core','56')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO ref_parts (part_num, name, category) VALUES ('3001','Brick 2 x 4','Bricks')`); err != nil {
		t.Fatal(err)
	}
	for _, in := range []string{"71788", "71788-1"} {
		if r, err := db.GetRefSet(in); err != nil || r == nil || r.Name != "Street Bike" {
			t.Errorf("GetRefSet(%q) = %+v, %v", in, r, err)
		}
	}
	if r, _ := db.GetRefSet("99999"); r != nil {
		t.Error("an unknown set must come back nil, not an error")
	}
	if r, err := db.GetRefPart("3001"); err != nil || r == nil || r.Category != "Bricks" {
		t.Errorf("GetRefPart = %+v, %v", r, err)
	}
	if p, err := db.GetOwnedPart("3001", 4, ""); err != nil || p != nil {
		t.Errorf("no owned record yet: got %+v, %v", p, err)
	}
	_ = db.AddOwnedPart(OwnedPart{PartNum: "3001", Name: "Brick 2 x 4", Category: "Bricks", ColorID: 4, ColorName: "Red", Qty: 7})
	if p, _ := db.GetOwnedPart("3001", 4, ""); p == nil || p.Qty != 7 {
		t.Errorf("GetOwnedPart after add = %+v", p)
	}
}

// countingServer answers every path with a fixed status/body and records hits,
// user agents and arrival times.
type countingServer struct {
	mu    sync.Mutex
	hits  int
	uas   []string
	times []time.Time
}

func (c *countingServer) start(t *testing.T, handler func(hit int, w http.ResponseWriter)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.mu.Lock()
		c.hits++
		hit := c.hits
		c.uas = append(c.uas, r.Header.Get("User-Agent"))
		c.times = append(c.times, time.Now())
		c.mu.Unlock()
		handler(hit, w)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCacheServesRepeatLookupsAndRemembersMisses(t *testing.T) {
	cs := &countingServer{}
	srv := cs.start(t, func(hit int, w http.ResponseWriter) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"part_num":"3001","name":"Brick 2 x 4","part_cat_id":11,"external_ids":{"BrickLink":["3001"],"LDraw":["3001"]}}`))
	})
	notFound := cs.start(t, func(hit int, w http.ResponseWriter) { w.WriteHeader(404) })

	c := &Client{APIKey: "k", BaseURL: srv.URL, HTTP: srv.Client(), Store: openScratchDB(t), UserAgent: "wms-go/test"}
	for i := 0; i < 3; i++ {
		p, err := c.GetPart(context.Background(), "3001")
		if err != nil || p.Name != "Brick 2 x 4" || p.ExternalID("BrickLink") != "3001" || p.ExternalID("BrickOwl") != "" {
			t.Fatalf("GetPart #%d: %+v %v", i, p, err)
		}
	}
	if cs.hits != 1 {
		t.Errorf("three identical lookups should cost one request, cost %d", cs.hits)
	}
	if cs.uas[0] != "wms-go/test" {
		t.Errorf("requests must identify themselves, got user agent %q", cs.uas[0])
	}

	before := cs.hits
	c.BaseURL = notFound.URL
	for i := 0; i < 3; i++ {
		if _, err := c.GetPart(context.Background(), "nope"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	}
	if cs.hits != before+1 {
		t.Errorf("a wrong number must be asked about once, not every time (%d extra requests)", cs.hits-before)
	}
}

func TestA429IsRetriedOnceAndThenReported(t *testing.T) {
	cs := &countingServer{}
	srv := cs.start(t, func(hit int, w http.ResponseWriter) {
		if hit == 1 {
			w.WriteHeader(429)
			_, _ = w.Write([]byte(`{"detail":"Request was throttled. Expected available in 0 seconds."}`))
			return
		}
		_, _ = w.Write([]byte(`{"part_num":"3001","name":"Brick","part_cat_id":1}`))
	})
	c := &Client{APIKey: "k", BaseURL: srv.URL, HTTP: srv.Client(), RetryWait: time.Second}
	if p, err := c.GetPart(context.Background(), "3001"); err != nil || p.Name != "Brick" || cs.hits != 2 {
		t.Fatalf("a short 429 should be retried once: %+v %v after %d requests", p, err, cs.hits)
	}

	long := &countingServer{}
	srv2 := long.start(t, func(hit int, w http.ResponseWriter) {
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`{"detail":"Request was throttled. Expected available in 30 seconds."}`))
	})
	c2 := &Client{APIKey: "k", BaseURL: srv2.URL, HTTP: srv2.Client(), RetryWait: time.Second}
	_, err := c2.GetPart(context.Background(), "3001")
	var rl *RateLimitError
	if !errors.Is(err, ErrRateLimited) || !errors.As(err, &rl) || rl.After < 30*time.Second {
		t.Fatalf("a long throttle must be reported with its wait, got %v", err)
	}
	if long.hits != 1 {
		t.Errorf("a 30 s throttle must not be retried, saw %d requests", long.hits)
	}
}

// Every session is its own process, so the spacing between requests is kept in
// the shared database: two clients (standing in for two processes) must not
// send at the combined rate.
func TestRequestsAreSpacedAcrossClientsSharingAStore(t *testing.T) {
	cs := &countingServer{}
	srv := cs.start(t, func(hit int, w http.ResponseWriter) { _, _ = w.Write([]byte(`{"results":[]}`)) })
	store := openScratchDB(t)
	mk := func() *Client {
		return &Client{APIKey: "k", BaseURL: srv.URL, HTTP: srv.Client(), Store: store, MinInterval: 90 * time.Millisecond, MaxWait: 5 * time.Second}
	}
	a, b := mk(), mk()

	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		c := a
		if i%2 == 1 {
			c = b
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = c.get(context.Background(), "/parts/?search=x", &pagedResults[PartInfo]{})
		}()
	}
	wg.Wait()

	sorted := append([]time.Time(nil), cs.times...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Before(sorted[j]) })
	if len(sorted) != 6 {
		t.Fatalf("expected 6 requests, saw %d", len(sorted))
	}
	// Slots are reserved 90 ms apart, but a request can reach the server later than its slot when the machine
	// is busy (the race detector, a loaded CI runner), so single gaps are not reliable. The whole run is:
	// without shared spacing all six requests would land within a few milliseconds.
	if span := sorted[len(sorted)-1].Sub(sorted[0]); span < 300*time.Millisecond {
		t.Errorf("six requests spanned only %v; the shared spacing (90 ms each, 450 ms in all) was not kept", span)
	}
}

func TestAThrottleSeenByOneClientQuietsTheOthers(t *testing.T) {
	cs := &countingServer{}
	srv := cs.start(t, func(hit int, w http.ResponseWriter) {
		if hit == 1 {
			w.WriteHeader(429)
			_, _ = w.Write([]byte(`{"detail":"Expected available in 1 seconds."}`))
			return
		}
		_, _ = w.Write([]byte(`{"results":[]}`))
	})
	store := openScratchDB(t)
	a := &Client{APIKey: "k", BaseURL: srv.URL, HTTP: srv.Client(), Store: store, MinInterval: time.Millisecond}
	b := &Client{APIKey: "k", BaseURL: srv.URL, HTTP: srv.Client(), Store: store, MinInterval: time.Millisecond, MaxWait: 3 * time.Second}

	_ = a.get(context.Background(), "/parts/?search=x", &pagedResults[PartInfo]{}) // gets the 429, no retry configured
	start := time.Now()
	if err := b.get(context.Background(), "/parts/?search=x", &pagedResults[PartInfo]{}); err != nil {
		t.Fatal(err)
	}
	if waited := time.Since(start); waited < 800*time.Millisecond {
		t.Errorf("the other process should have waited out the throttle (~1s), waited only %v", waited)
	}
}

func TestPartColorsAndColourTable(t *testing.T) {
	c := fakeAPI(t, map[string]string{
		"/parts/3001/colors/": `{"count":2,"results":[{"color_id":4,"color_name":"Red","num_sets":900},{"color_id":1,"color_name":"Blue","num_sets":700}]}`,
		"/colors/":            `{"results":[{"id":4,"name":"Red","rgb":"C91A09","is_trans":false,"external_ids":{"BrickLink":{"ext_ids":[5],"ext_descrs":[["Red"]]}}}]}`,
	}, nil)
	cols, err := c.GetPartColors(context.Background(), "3001")
	if err != nil || len(cols) != 2 || cols[0].ColorName != "Red" || cols[1].ColorID != 1 {
		t.Fatalf("colours = %+v, %v", cols, err)
	}
	all, err := c.AllColors(context.Background())
	if err != nil || len(all) != 1 || all[0].RGB != "C91A09" {
		t.Fatalf("colour table = %+v, %v", all, err)
	}
}
