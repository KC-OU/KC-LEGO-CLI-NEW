package publicstock

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

func TestPublicStockAnswersOnlyStatusValidatesInputAndRateLimits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lego.db")
	w, err := lego.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	w.AddOwnedPart(lego.OwnedPart{PartNum: "3001", ColorID: 4, ColorName: "Red", Qty: 40})
	w.AddOwnedPart(lego.OwnedPart{PartNum: "3023", ColorID: 4, ColorName: "Red", Qty: 3})
	w.Close()
	db, err := OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	srv := httptest.NewServer(Handler(db, 4))
	defer srv.Close()
	get := func(q string) (int, string) {
		resp, err := http.Get(srv.URL + "/stock" + q)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b := make([]byte, 512)
		n, _ := resp.Body.Read(b)
		return resp.StatusCode, string(b[:n])
	}
	if c, b := get("?part=3001"); c != 200 || !strings.Contains(b, `"in stock"`) || strings.Contains(b, "40") {
		t.Errorf("3001 = %d %s (must say in stock and never the quantity)", c, b)
	}
	if _, b := get("?part=3023"); !strings.Contains(b, `"low stock"`) {
		t.Errorf("3023 = %s", b)
	}
	if _, b := get("?part=99999"); !strings.Contains(b, `"out of stock"`) {
		t.Errorf("unknown part must read as out of stock, got %s", b)
	}
	if c, _ := get("?part=1;DROP"); c != http.StatusBadRequest {
		t.Errorf("junk part = %d, want 400", c)
	}
	if c, _ := get("?part=3001"); c != http.StatusTooManyRequests { // 5th request in the minute, limit 4
		t.Errorf("5th request = %d, want 429", c)
	}
	// the handle really is read-only
	if _, err := db.Exec(`DELETE FROM owned_parts`); err == nil {
		t.Error("the public-stock database handle must refuse writes")
	}
}
