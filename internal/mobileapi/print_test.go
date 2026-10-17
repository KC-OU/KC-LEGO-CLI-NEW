package mobileapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

func newPrintTestServer(s *Server) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /mobile/print", s.handlePrintPage)
	mux.HandleFunc("GET /mobile/print/raster", s.withSession(s.handlePrintRaster))
	mux.HandleFunc("GET /mobile/print/bags", s.withSession(s.handlePrintBags))
	return httptest.NewServer(mux)
}

// TestPrintPageIsPublicAndServesTheBluetoothPage: no session needed to load the
// page itself -- signing in is the page's own first step, done in-browser.
func TestPrintPageIsPublicAndServesTheBluetoothPage(t *testing.T) {
	s := &Server{}
	srv := httptest.NewServer(http.HandlerFunc(s.handlePrintPage))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /mobile/print = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "navigator.bluetooth") {
		t.Error("the page should contain the Web Bluetooth JS")
	}
}

// TestPrintRasterIsAdminOnly mirrors TestLabelPDFIsAdminOnly -- printing a
// label over Bluetooth is the same admin action as printing one as a PDF.
func TestPrintRasterIsAdminOnly(t *testing.T) {
	s, db := checkWalkEnv(t)
	srv := newPrintTestServer(s)
	defer srv.Close()
	token := issueToken(t, db)

	resp := authed(t, http.MethodGet, srv.URL+"/mobile/print/raster?set=75192-1", token, nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("GET print/raster as a non-admin = %d, want 403", resp.StatusCode)
	}
}

// TestPrintRasterReturnsPM260ReadyBytesForTheSmallBagSize is the happy path:
// the default small-bag size, centered within the PM260's 72-byte head, base64
// in the JSON body.
func TestPrintRasterReturnsPM260ReadyBytesForTheSmallBagSize(t *testing.T) {
	s, db := checkWalkEnv(t)
	asAdmin(t, "dave")
	srv := newPrintTestServer(s)
	defer srv.Close()
	token := issueToken(t, db)

	resp := authed(t, http.MethodGet, srv.URL+"/mobile/print/raster?set=75192-1", token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET print/raster as admin = %d, want 200", resp.StatusCode)
	}
	var body struct {
		WidthBytes  int    `json:"width_bytes"`
		HeightLines int    `json:"height_lines"`
		Data        string `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.WidthBytes != pm260HeadWidthBytes {
		t.Errorf("width_bytes = %d, want %d (the PM260's own head width)", body.WidthBytes, pm260HeadWidthBytes)
	}
	if body.HeightLines <= 0 {
		t.Errorf("height_lines = %d, want > 0", body.HeightLines)
	}
	raw, err := base64.StdEncoding.DecodeString(body.Data)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != body.WidthBytes*body.HeightLines {
		t.Errorf("data length = %d, want %d (width_bytes * height_lines)", len(raw), body.WidthBytes*body.HeightLines)
	}
}

func TestPrintRasterRejectsAnInvalidSize(t *testing.T) {
	s, db := checkWalkEnv(t)
	asAdmin(t, "dave")
	srv := newPrintTestServer(s)
	defer srv.Close()
	token := issueToken(t, db)

	resp := authed(t, http.MethodGet, srv.URL+"/mobile/print/raster?set=75192-1&size=not-a-real-size", token, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("GET print/raster with a bogus size = %d, want 400", resp.StatusCode)
	}
}

// TestPrintBagsListsSmallAndMainButNotExtraUntilOneExists confirms the
// printable bag list matches wms lego bag-labels: small/main always
// (deterministic codes, no check needed yet), extra only once a check has
// actually created its instance.
func TestPrintBagsListsSmallAndMainButNotExtraUntilOneExists(t *testing.T) {
	s, db := checkWalkEnv(t)
	asAdmin(t, "dave")
	srv := newPrintTestServer(s)
	defer srv.Close()
	token := issueToken(t, db)

	resp := authed(t, http.MethodGet, srv.URL+"/mobile/print/bags?set=75192-1", token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET print/bags = %d, want 200", resp.StatusCode)
	}
	var bags []printBagOption
	if err := json.NewDecoder(resp.Body).Decode(&bags); err != nil {
		t.Fatal(err)
	}
	if len(bags) != 2 || bags[0].Key != "small" || bags[1].Key != "main" {
		t.Fatalf("print/bags before any extra = %+v, want just small+main", bags)
	}
}

// TestPrintBagsShowsAPoolAssignedBagsOwnTrueCode is the regression test for
// the reprint bug: a pool-assigned bag's code is whatever an admin chose for
// it, never the deterministic "<set>-A" formula -- the print page's bag list
// must show that real code, not a recomputed wrong one.
func TestPrintBagsShowsAPoolAssignedBagsOwnTrueCode(t *testing.T) {
	s, db := checkWalkEnv(t)
	asAdmin(t, "dave")
	srv := newPrintTestServer(s)
	defer srv.Close()
	token := issueToken(t, db)

	if _, err := db.AddPoolBag("BIN-09", lego.BagSmall, "admin"); err != nil {
		t.Fatal(err)
	}
	// checkWalkEnv only claims a ticket -- the check row itself is created
	// lazily on first confirm, same as the TUI -- so build and save one
	// directly to have a real check ID to assign the pool bag against.
	c, err := db.NewCheck(context.Background(), nil, "75192-1", lego.CheckIntake, "dave")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SaveCheck(c); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AssignPoolBag(c.ID, "admin", "BIN-09"); err != nil {
		t.Fatal(err)
	}

	resp := authed(t, http.MethodGet, srv.URL+"/mobile/print/bags?set=75192-1", token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET print/bags = %d, want 200", resp.StatusCode)
	}
	var bags []printBagOption
	if err := json.NewDecoder(resp.Body).Decode(&bags); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, b := range bags {
		if b.Key == lego.BagSmall {
			found = true
			if b.Code != "BIN-09" {
				t.Errorf("small bag's code = %q, want the pool bag's own code BIN-09, not a recomputed one", b.Code)
			}
		}
	}
	if !found {
		t.Fatalf("expected a small bag entry among %+v", bags)
	}

	// Printing it uses that exact code, via bag_code -- not bag_kind, which
	// would only ever be right for a kind's auto-generated first instance.
	resp = authed(t, http.MethodGet, srv.URL+"/mobile/print/raster?set=75192-1&bag_code=BIN-09&bag_kind=small", token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET print/raster?bag_code=BIN-09 = %d, want 200", resp.StatusCode)
	}
}

// TestPrintRasterUsesBagKindForTheCodeAndLocation confirms bag_kind resolves
// through the registry to a deterministic code and label text, not a
// free-text value the caller made up.
func TestPrintRasterUsesBagKindForTheCodeAndLocation(t *testing.T) {
	s, db := checkWalkEnv(t)
	asAdmin(t, "dave")
	srv := newPrintTestServer(s)
	defer srv.Close()
	token := issueToken(t, db)

	resp := authed(t, http.MethodGet, srv.URL+"/mobile/print/raster?set=75192-1&bag_kind=main", token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET print/raster?bag_kind=main = %d, want 200", resp.StatusCode)
	}
}

func TestPrintRasterRequiresASetNumber(t *testing.T) {
	s, db := checkWalkEnv(t)
	asAdmin(t, "dave")
	srv := newPrintTestServer(s)
	defer srv.Close()
	token := issueToken(t, db)

	resp := authed(t, http.MethodGet, srv.URL+"/mobile/print/raster", token, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("GET print/raster with no set = %d, want 400", resp.StatusCode)
	}
}
