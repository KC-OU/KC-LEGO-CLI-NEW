package brickowl

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLookupAndAvailability(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") != "k" {
			w.WriteHeader(403)
			return
		}
		switch r.URL.Path {
		case "/catalog/id_lookup":
			if r.URL.Query().Get("id_type") == "element_id" && r.URL.Query().Get("id") == "300121" {
				w.Write([]byte(`{"boids":["771344-39"]}`))
				return
			}
			w.WriteHeader(404)
		case "/catalog/availability":
			w.Write([]byte(`{"1":{"price":"0.10","qty":"10","base_currency":"GBP"},"2":{"price":"0.04","qty":"30","base_currency":"GBP"},"3":{"price":"0","qty":"5"}}`))
		}
	}))
	defer srv.Close()
	c := &Client{Key: "k", BaseURL: srv.URL, HTTP: srv.Client()}
	boid, err := c.Lookup(context.Background(), "300121", "3001")
	if err != nil || boid != "771344-39" {
		t.Fatalf("lookup %q %v", boid, err)
	}
	if _, err := c.Lookup(context.Background(), "", "9999"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown design: %v", err)
	}
	p, err := c.Availability(context.Background(), boid)
	if err != nil || p.Low != 0.04 || p.Listings != 2 || p.Currency != "GBP" || p.Avg < 0.054 || p.Avg > 0.056 {
		t.Fatalf("availability %+v %v", p, err)
	}
	bad := &Client{Key: "wrong", BaseURL: srv.URL, HTTP: srv.Client()}
	if _, err := bad.Lookup(context.Background(), "1", ""); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("a refused key must be an error, not not-found: %v", err)
	}
}
