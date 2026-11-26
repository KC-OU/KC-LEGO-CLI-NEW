package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestChatFallsThroughDeadEmptyAndSlowModelsToOneThatAnswers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, 400)
		n, _ := r.Body.Read(b)
		switch {
		case strings.Contains(string(b[:n]), "dead"):
			w.WriteHeader(404)
		case strings.Contains(string(b[:n]), "empty"):
			w.Write([]byte(`{"choices":[{"message":{"content":""}}]}`))
		case strings.Contains(string(b[:n]), "slow"):
			time.Sleep(300 * time.Millisecond)
		default:
			if r.Header.Get("Authorization") != "Bearer k" {
				w.WriteHeader(401)
				return
			}
			w.Write([]byte("{\"choices\":[{\"message\":{\"content\":\"```json\\n{\\\"words\\\":[\\\"brick\\\"]}\\n```\"}}]}"))
		}
	}))
	defer srv.Close()
	old := BaseURL
	BaseURL = srv.URL
	defer func() { BaseURL = old }()
	c := &Client{Key: "k", Models: []string{"dead", "empty", "slow", "good"}, Timeout: 100 * time.Millisecond, HTTP: srv.Client()}
	out, err := c.Chat(context.Background(), "s", "u", 50)
	if err != nil || ExtractJSON(out) != `{"words":["brick"]}` {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if _, err := (&Client{}).Chat(context.Background(), "s", "u", 5); err != ErrNotConfigured {
		t.Errorf("no key must be ErrNotConfigured, got %v", err)
	}
	bad := &Client{Key: "k", Models: []string{"dead"}, Timeout: time.Second, HTTP: srv.Client()}
	if _, err := bad.Chat(context.Background(), "s", "u", 5); err == nil || strings.Contains(err.Error(), "Bearer") {
		t.Errorf("all-dead must fail without leaking anything: %v", err)
	}
}

func TestExtractJSON(t *testing.T) {
	for in, want := range map[string]string{`x {"a":1} y`: `{"a":1}`, "none": "", "}{": ""} {
		if got := ExtractJSON(in); got != want {
			t.Errorf("ExtractJSON(%q) = %q, want %q", in, got, want)
		}
	}
}
