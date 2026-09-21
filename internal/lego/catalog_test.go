package lego

import (
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

var catalogCSV = map[string]string{
	"parts":              "part_num,name,part_cat_id,part_material\n3001,Brick 2 x 4,11,Plastic\n3023,Plate 1 x 2,14,Plastic\n970c00,Minifig Legs,61,Plastic\n",
	"part_categories":    "id,name\n11,Bricks\n14,Plates\n61,Minifig Legs\n",
	"colors":             "id,name,rgb,is_trans,num_parts,num_sets,y1,y2\n-1,[Unknown],0033B2,f,0,0,0,0\n0,Black,05131D,f,100,100,1949,2026\n1,Blue,0055BF,f,100,100,1949,2026\n4,Red,C91A09,f,100,100,1949,2026\n47,Trans-Clear,FCFCFC,t,100,100,1949,2026\n",
	"elements":           "element_id,part_num,color_id,design_id\n300121,3001,4,3001\n300123,3001,1,3001\n302326,3023,4,3023\n",
	"sets":               "set_num,name,year,theme_id,num_parts,img_url\n75192-1,Millennium Falcon,2017,171,7541,https://cdn.rebrickable.com/media/sets/75192-1.jpg\n30008-1,Small Test Set,2010,158,44,\n71788-1,Lloyd's Ninja Bike,2020,158,64,\n",
	"themes":             "id,name,parent_id\n158,Star Wars,\n171,Ultimate Collector Series,158\n",
	"minifigs":           "fig_num,name,num_parts,img_url\nfig-000001,Toy Store Employee,4,https://cdn.rebrickable.com/media/sets/fig-000001.jpg\n",
	"inventories":        "id,version,set_num\n1,1,75192-1\n2,1,30008-1\n3,2,30008-1\n",
	"inventory_parts":    "inventory_id,part_num,color_id,quantity,is_spare,img_url\n1,3001,4,10,False,\n1,3023,1,6,False,\n1,3001,4,2,True,\n3,3001,4,4,False,\n3,3023,1,2,False,\n",
	"inventory_sets":     "inventory_id,set_num,quantity\n1,30008-1,1\n",
	"inventory_minifigs": "inventory_id,fig_num,quantity\n1,fig-000001,1\n",
	"part_relationships": "rel_type,child_part_num,parent_part_num\nM,3001a,3001\nA,3023,3023b\nP,3001pr0001,3001\n",
}

func gz(s string) []byte {
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	_, _ = w.Write([]byte(s))
	_ = w.Close()
	return b.Bytes()
}

// catalogServer serves the four CSVs with a Last-Modified header and honours
// If-Modified-Since, like Rebrickable's CDN.
type catalogServer struct {
	mu       sync.Mutex
	requests []string
	failing  string            // file name that answers 500
	notMod   bool              // answer 304 to conditional requests
	override map[string][]byte // raw bytes to serve for a file instead of the fixture (already gzipped, or deliberately broken)
}

func (c *catalogServer) start(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.mu.Lock()
		defer c.mu.Unlock()
		name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), ".csv.gz")
		c.requests = append(c.requests, name+"|"+r.Header.Get("If-Modified-Since"))
		if name == c.failing {
			w.WriteHeader(500)
			return
		}
		if c.notMod && r.Header.Get("If-Modified-Since") != "" {
			w.WriteHeader(304)
			return
		}
		if raw, ok := c.override[name]; ok {
			w.Header().Set("Last-Modified", "Sun, 20 Sep 2026 03:00:00 GMT")
			_, _ = w.Write(raw)
			return
		}
		body, ok := catalogCSV[name]
		if !ok {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Last-Modified", "Sat, 19 Sep 2026 03:00:00 GMT")
		_, _ = w.Write(gz(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCatalogRefreshLoadsAndLooksUp(t *testing.T) {
	db := openScratchDB(t)
	srv := (&catalogServer{}).start(t)
	res, err := db.RefreshCatalog(context.Background(), CatalogOptions{BaseURL: srv.URL + "/", HTTP: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped || res.Updated != len(catalogSpecs) || !strings.Contains(res.Message, "Rebrickable") {
		t.Fatalf("result = %+v (the message must carry the attribution)", res)
	}
	parts, colors, when, _ := db.CatalogStatus()
	if parts != 3 || colors != 5 || when.IsZero() {
		t.Fatalf("status = %d parts, %d colours, %v", parts, colors, when)
	}

	p, err := db.CatalogPart(" 3001 ")
	if err != nil || p == nil || p.Name != "Brick 2 x 4" || p.Category != "Bricks" {
		t.Fatalf("CatalogPart = %+v, %v", p, err)
	}
	if p, _ := db.CatalogPart("970C00"); p == nil || p.Num != "970c00" { // case-insensitive
		t.Errorf("part numbers should match case-insensitively: %+v", p)
	}
	if p, _ := db.CatalogPart("nope"); p != nil {
		t.Error("an unknown part is nil, not an error")
	}
	cols, _ := db.CatalogColorsFor("3001")
	if len(cols) != 2 || cols[0].Name != "Blue" || cols[1].Name != "Red" || cols[1].RGB != "C91A09" {
		t.Errorf("colours of 3001 = %+v", cols)
	}
	all, _ := db.CatalogColors()
	var trans bool
	for _, c := range all {
		if c.Name == "Trans-Clear" {
			trans = c.Trans
		}
	}
	if !trans {
		t.Error("Trans-Clear should be flagged transparent")
	}
}

func TestCatalogRefreshIsRateLimitedToOncePerDay(t *testing.T) {
	db := openScratchDB(t)
	cs := &catalogServer{}
	srv := cs.start(t)
	opt := CatalogOptions{BaseURL: srv.URL + "/", HTTP: srv.Client()}
	if _, err := db.RefreshCatalog(context.Background(), opt); err != nil {
		t.Fatal(err)
	}
	before := len(cs.requests)
	res, err := db.RefreshCatalog(context.Background(), opt)
	if err != nil || !res.Skipped || len(cs.requests) != before {
		t.Fatalf("a second refresh within a day must not download anything: %+v %v (%d new requests)", res, err, len(cs.requests)-before)
	}

	// Forced: asks again, but conditionally, so an unchanged file costs a 304.
	cs.notMod = true
	opt.Force = true
	res, err = db.RefreshCatalog(context.Background(), opt)
	if err != nil || res.Skipped || res.Updated != 0 {
		t.Fatalf("forced refresh of unchanged files: %+v %v", res, err)
	}
	for _, r := range cs.requests[before:] {
		if strings.HasSuffix(r, "|") {
			t.Errorf("a repeat download must send If-Modified-Since: %q", r)
		}
	}
	if p, _ := db.CatalogPart("3001"); p == nil {
		t.Error("a 304 must keep the data already loaded")
	}
}

func TestFailedDownloadLeavesTheOldCatalogAlone(t *testing.T) {
	db := openScratchDB(t)
	cs := &catalogServer{}
	srv := cs.start(t)
	opt := CatalogOptions{BaseURL: srv.URL + "/", HTTP: srv.Client(), Force: true}
	if _, err := db.RefreshCatalog(context.Background(), opt); err != nil {
		t.Fatal(err)
	}

	cs.failing = "elements" // the last file of four fails
	catalogCSV["parts"] += "9999,New Part,11,Plastic\n"
	defer func() {
		catalogCSV["parts"] = strings.Replace(catalogCSV["parts"], "9999,New Part,11,Plastic\n", "", 1)
	}()
	cs.notMod = false
	if _, err := db.RefreshCatalog(context.Background(), opt); err == nil {
		t.Fatal("a failing download must be an error")
	}
	if p, _ := db.CatalogPart("9999"); p != nil {
		t.Error("nothing from a failed refresh may be loaded (it is all-or-nothing)")
	}
	if p, _ := db.CatalogPart("3001"); p == nil {
		t.Error("the previous catalog must still be intact")
	}
}

func TestCatalogRejectsGarbage(t *testing.T) {
	db := openScratchDB(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("<html>Cloudflare</html>")) }))
	defer srv.Close()
	if _, err := db.RefreshCatalog(context.Background(), CatalogOptions{BaseURL: srv.URL + "/", HTTP: srv.Client(), Force: true}); err == nil {
		t.Fatal("an HTML challenge page is not a catalog")
	}
}
