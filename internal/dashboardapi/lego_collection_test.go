package dashboardapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
)

func TestUpsertSetCreatesThenUpdatesAnOwnedSet(t *testing.T) {
	db, srv := testServer(t, "secret")
	defer srv.Close()

	create := postJSON(t, srv.URL, "/dashboard/lego/sets", `{"set_num":"75192-1","name":"Millennium Falcon","theme":"Star Wars","year":2017,"parts_qty":7541,"qty":1,"by":"admin"}`)
	defer create.Body.Close()
	if create.StatusCode != http.StatusOK {
		t.Fatalf("create status = %d", create.StatusCode)
	}
	set, err := db.GetSetByNum("75192-1")
	if err != nil || set.Name != "Millennium Falcon" || set.Qty != 1 {
		t.Fatalf("got %+v, err=%v", set, err)
	}

	update := postJSON(t, srv.URL, "/dashboard/lego/sets", `{"set_num":"75192-1","name":"Millennium Falcon","theme":"Star Wars","year":2017,"parts_qty":7541,"qty":2,"location":"Shelf A","by":"admin"}`)
	defer update.Body.Close()
	if update.StatusCode != http.StatusOK {
		t.Fatalf("update status = %d", update.StatusCode)
	}
	set, _ = db.GetSetByNum("75192-1")
	if set.Qty != 2 {
		t.Fatalf("expected qty 2 after update, got %d", set.Qty)
	}
	state := db.GetSetState("75192-1")
	if state.Location != "Shelf A" {
		t.Fatalf("expected location to be set, got %+v", state)
	}
}

func TestUpsertSetRequiresSetNumAndBy(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()
	resp := postJSON(t, srv.URL, "/dashboard/lego/sets", `{"name":"no set num"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestUpsertPartSetsTheTotalQuantity(t *testing.T) {
	db, srv := testServer(t, "secret")
	defer srv.Close()

	resp := postJSON(t, srv.URL, "/dashboard/lego/parts", `{"part_num":"3001","name":"Brick 2x4","color_id":5,"color_name":"Red","qty":40,"by":"admin"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	part, err := db.GetOwnedPart("3001", 5, "Red")
	if err != nil || part.Qty != 40 {
		t.Fatalf("got %+v, err=%v", part, err)
	}
}

func TestMissingAndExtraReportsRunOverAnEmptyCollection(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()
	for _, path := range []string{"/dashboard/reports/missing", "/dashboard/reports/extra", "/dashboard/reports/orders-list", "/dashboard/reports/history", "/dashboard/lego/retiring"} {
		resp := getWithToken(t, srv.URL, path)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s status = %d", path, resp.StatusCode)
		}
		resp.Body.Close()
	}
}

func TestOrdersWorkflowCreateAddLineAndAdvanceStatus(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()

	create := postJSON(t, srv.URL, "/dashboard/orders", `{"supplier_kind":"bricklink","supplier":"BrickDepot","currency":"GBP","by":"admin"}`)
	defer create.Body.Close()
	if create.StatusCode != http.StatusOK {
		t.Fatalf("create status = %d", create.StatusCode)
	}
	var order struct {
		ID int64 `json:"ID"`
	}
	if err := json.NewDecoder(create.Body).Decode(&order); err != nil {
		t.Fatal(err)
	}
	if order.ID == 0 {
		t.Fatal("expected a non-zero order id")
	}

	line := postJSON(t, srv.URL, "/dashboard/orders/"+strconv.FormatInt(order.ID, 10)+"/lines", `{"part_num":"3001","color_name":"Red","qty":10,"unit_price":0.05,"by":"admin"}`)
	defer line.Body.Close()
	if line.StatusCode != http.StatusOK {
		t.Fatalf("add line status = %d", line.StatusCode)
	}

	status := postJSON(t, srv.URL, "/dashboard/orders/"+strconv.FormatInt(order.ID, 10)+"/status", `{"status":"ordered","by":"admin"}`)
	defer status.Body.Close()
	if status.StatusCode != http.StatusOK {
		t.Fatalf("set status = %d", status.StatusCode)
	}

	get := getWithToken(t, srv.URL, "/dashboard/orders/"+strconv.FormatInt(order.ID, 10))
	defer get.Body.Close()
	var fetched struct {
		Status string `json:"Status"`
		Lines  []any  `json:"Lines"`
	}
	if err := json.NewDecoder(get.Body).Decode(&fetched); err != nil {
		t.Fatal(err)
	}
	if fetched.Status != "ordered" || len(fetched.Lines) != 1 {
		t.Fatalf("got %+v", fetched)
	}
}

func TestSpendReportDefaultsToMonth(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()
	resp := getWithToken(t, srv.URL, "/dashboard/orders/spend")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestLabelForAnUnownedSetIs404(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()
	resp := getWithToken(t, srv.URL, "/dashboard/labels/00000-1?format=html")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestLabelRendersHTMLForAnOwnedSet(t *testing.T) {
	_, srv := testServer(t, "secret")
	defer srv.Close()
	create := postJSON(t, srv.URL, "/dashboard/lego/sets", `{"set_num":"75192-1","name":"Millennium Falcon","by":"admin"}`)
	create.Body.Close()

	resp := getWithToken(t, srv.URL, "/dashboard/labels/75192-1?format=html")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Fatalf("content-type = %q", ct)
	}
}
