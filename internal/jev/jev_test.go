package jev

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPlausibleReadsProbabilityAndRejectsAMissingKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer k" {
			w.WriteHeader(401)
			return
		}
		w.Write([]byte(`{"answers":{"q":{"type":"noul","noul":0.82}}}`))
	}))
	defer srv.Close()

	c := &Client{Key: "k", BaseURL: srv.URL, HTTP: srv.Client()}
	p, err := c.Plausible(context.Background(), "is this genuine?")
	if err != nil || p != 0.82 {
		t.Fatalf("Plausible = %v, %v", p, err)
	}

	noKey := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	if _, err := noKey.Plausible(context.Background(), "is this genuine?"); err == nil {
		t.Fatal("expected an error with no key configured")
	}
}
