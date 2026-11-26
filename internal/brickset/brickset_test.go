package brickset

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetSetsParsesDatesPricesAndSurfacesAPIErrors(t *testing.T) {
	var gotParams string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		gotParams = r.PostForm.Get("params")
		if r.PostForm.Get("apiKey") != "k" {
			w.Write([]byte(`{"status":"error","message":"Invalid API key"}`))
			return
		}
		w.Write([]byte(`{"status":"success","matches":1,"sets":[{"number":"75192","numberVariant":1,"name":"Millennium Falcon","year":2017,"pieces":7541,
			"launchDate":"2017-10-01T00:00:00","exitDate":"2026-12-31T00:00:00","availability":"Retiring soon",
			"LEGOCom":{"UK":{"retailPrice":734.99,"dateLastAvailable":"2026-12-31T00:00:00"},"US":{"retailPrice":null}}}]}`))
	}))
	defer srv.Close()
	c := &Client{Key: "k", BaseURL: srv.URL, HTTP: srv.Client()}
	sets, err := c.GetSets(context.Background(), []string{"75192-1"})
	if err != nil || len(sets) != 1 {
		t.Fatalf("sets=%v err=%v", sets, err)
	}
	s := sets[0]
	if s.SetNum() != "75192-1" || Day(s.ExitDate) != "2026-12-31" || *s.LEGOCom.UK.RetailPrice != 734.99 || s.LEGOCom.US.RetailPrice != nil || *s.Pieces != 7541 {
		t.Fatalf("parsed wrong: %+v", s)
	}
	if !strings.Contains(gotParams, "75192-1") {
		t.Errorf("params = %s", gotParams)
	}
	bad := &Client{Key: "nope", BaseURL: srv.URL, HTTP: srv.Client()}
	if _, err := bad.GetSets(context.Background(), []string{"1-1"}); err == nil || !strings.Contains(err.Error(), "Invalid API key") {
		t.Errorf("want the API's message, got %v", err)
	}
	if _, err := (&Client{}).GetSets(context.Background(), []string{"1-1"}); err != ErrNotConfigured {
		t.Errorf("no key must be ErrNotConfigured, got %v", err)
	}
}
