package dashboardapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

type oneOrder struct{}

func (oneOrder) ListOrders(context.Context) ([]lego.SalesOrder, error) {
	return []lego.SalesOrder{{OrderID: "77", Buyer: "ann", Status: "PAID"}}, nil
}
func (oneOrder) OrderLines(context.Context, string) ([]lego.SalesLine, error) {
	return []lego.SalesLine{{ItemNo: "3001", BLColor: 5, Qty: 3, Cond: "U"}}, nil
}

func TestSalesOrderListPickListAndPickedTicks(t *testing.T) {
	db, srv := testServer(t, "secret")
	defer srv.Close()
	if _, err := db.SyncSalesOrders(context.Background(), oneOrder{}); err != nil {
		t.Fatal(err)
	}
	resp := getWithToken(t, srv.URL, "/dashboard/sales")
	var list []lego.SalesOrder
	json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()
	if len(list) != 1 || list[0].OrderID != "77" {
		t.Fatalf("list = %+v", list)
	}
	resp = getWithToken(t, srv.URL, "/dashboard/sales/77")
	var o lego.SalesOrder
	json.NewDecoder(resp.Body).Decode(&o)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || len(o.Lines) != 1 || o.Lines[0].Qty != 3 {
		t.Fatalf("pick list = %d %+v", resp.StatusCode, o)
	}
	if r := postJSON(t, srv.URL, "/dashboard/sales/77/picked", `{"line":1,"picked":3}`); r.StatusCode != http.StatusBadRequest {
		t.Errorf("missing 'by' must be 400, got %d", r.StatusCode)
	}
	if r := postJSON(t, srv.URL, "/dashboard/sales/77/picked", `{"line":1,"picked":3,"by":"alice"}`); r.StatusCode != http.StatusOK {
		t.Fatalf("picked = %d", r.StatusCode)
	}
	if got, _ := db.GetSalesOrder("77"); got.PickedAt == "" {
		t.Error("order should be marked fully picked")
	}
	if r := getWithToken(t, srv.URL, "/dashboard/sales/404"); r.StatusCode != http.StatusNotFound {
		t.Errorf("unknown order = %d, want 404", r.StatusCode)
	}
}

func TestSalesSyncNeedsBrickLinkConfigured(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()
	if r := postJSON(t, srv.URL, "/dashboard/sales/sync", `{}`); r.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 with no BrickLink credentials", r.StatusCode)
	}
}

func TestAcquisitionsAddAdvanceAndList(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()
	r := postJSON(t, srv.URL, "/dashboard/acquisitions", `{"label":"Job lot","cost":40,"currency":"GBP","by":"alice"}`)
	var added map[string]int64
	json.NewDecoder(r.Body).Decode(&added)
	r.Body.Close()
	if r.StatusCode != http.StatusOK || added["id"] == 0 {
		t.Fatalf("add = %d %v", r.StatusCode, added)
	}
	if r := postJSON(t, srv.URL, "/dashboard/acquisitions/1/advance", `{"by":"alice"}`); r.StatusCode != http.StatusOK {
		t.Fatalf("advance = %d", r.StatusCode)
	}
	resp := getWithToken(t, srv.URL, "/dashboard/acquisitions?stage=received")
	var list []lego.Acquisition
	json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()
	if len(list) != 1 || list[0].Stage != "received" {
		t.Fatalf("received list = %+v", list)
	}
}

func TestMoversAndRepriceAnswerOnAnEmptyDatabase(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()
	for _, p := range []string{"/dashboard/lego/movers", "/dashboard/store/reprice"} {
		resp := getWithToken(t, srv.URL, p)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s = %d", p, resp.StatusCode)
		}
		resp.Body.Close()
	}
}

func TestBorrowedPartsAddAdvanceAndList(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()
	r := postJSON(t, srv.URL, "/dashboard/borrowed", `{"BuildSetNum":"75192-1","BuildSetName":"Falcon","NeedPartNum":"3001","NeedElementID":"300126","FromSetNum":"10696-1","FromPartNum":"3001","by":"alice"}`)
	var added map[string]int64
	json.NewDecoder(r.Body).Decode(&added)
	r.Body.Close()
	if r.StatusCode != http.StatusOK || added["id"] == 0 {
		t.Fatalf("add = %d %v", r.StatusCode, added)
	}
	if r := postJSON(t, srv.URL, "/dashboard/borrowed", `{"BuildSetNum":"75192-1","by":"alice"}`); r.StatusCode != http.StatusBadRequest {
		t.Errorf("a note without the needed part = %d, want 400", r.StatusCode)
	}
	if r := postJSON(t, srv.URL, "/dashboard/borrowed/1/advance", `{"by":"alice"}`); r.StatusCode != http.StatusOK {
		t.Fatalf("advance = %d", r.StatusCode)
	}
	resp := getWithToken(t, srv.URL, "/dashboard/borrowed")
	var list []lego.BorrowedPart
	json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()
	if len(list) != 1 || list[0].Status != "ordered" || list[0].FromSetNum != "10696-1" || list[0].CreatedBy != "alice" {
		t.Fatalf("list = %+v", list)
	}
}
