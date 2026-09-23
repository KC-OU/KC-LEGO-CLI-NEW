package notify

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDiscordBotDMOpensChannelThenPostsWithAttachment(t *testing.T) {
	var gotAuth, gotRecipient string
	var gotContent string
	var gotFileBytes []byte
	var gotFileName string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/users/@me/channels":
			var body struct {
				RecipientID string `json:"recipient_id"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			gotRecipient = body.RecipientID
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"chan123"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/channels/chan123/messages":
			_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil {
				t.Errorf("bad content-type: %v", err)
			}
			mr := multipart.NewReader(r.Body, params["boundary"])
			for {
				part, err := mr.NextPart()
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				switch part.FormName() {
				case "payload_json":
					b, _ := io.ReadAll(part)
					var payload struct {
						Content string `json:"content"`
					}
					_ = json.Unmarshal(b, &payload)
					gotContent = payload.Content
				case "files[0]":
					gotFileName = part.FileName()
					gotFileBytes, _ = io.ReadAll(part)
				}
			}
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	b := &DiscordBot{Token: "tok", UserID: "999", BaseURL: srv.URL}
	err := b.DM(context.Background(), "missing parts for 75192, expires 14:30", []byte("fake-png-bytes"), "qr.png")
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bot tok" {
		t.Errorf("Authorization = %q, want \"Bot tok\"", gotAuth)
	}
	if gotRecipient != "999" {
		t.Errorf("recipient_id = %q, want 999", gotRecipient)
	}
	if gotContent != "missing parts for 75192, expires 14:30" {
		t.Errorf("content = %q", gotContent)
	}
	if gotFileName != "qr.png" || string(gotFileBytes) != "fake-png-bytes" {
		t.Errorf("attachment = %q %q", gotFileName, gotFileBytes)
	}
}

func TestDiscordBotDMWithoutImage(t *testing.T) {
	var sawFile bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/users/@me/channels" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"chan123"}`))
			return
		}
		_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			part, err := mr.NextPart()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			if part.FormName() == "files[0]" {
				sawFile = true
			}
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	b := &DiscordBot{Token: "tok", UserID: "999", BaseURL: srv.URL}
	if err := b.DM(context.Background(), "text only", nil, ""); err != nil {
		t.Fatal(err)
	}
	if sawFile {
		t.Error("no image should mean no file part")
	}
}

func TestDiscordBotNotConfigured(t *testing.T) {
	b := &DiscordBot{}
	if b.Enabled() {
		t.Error("an empty bot should not be Enabled")
	}
	if err := b.DM(context.Background(), "x", nil, ""); err == nil {
		t.Error("DM on an unconfigured bot should error, not silently do nothing")
	}
}

func TestDiscordBotAPIErrorSurfacesStatusAndBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"Missing Access"}`))
	}))
	defer srv.Close()
	b := &DiscordBot{Token: "tok", UserID: "999", BaseURL: srv.URL}
	err := b.DM(context.Background(), "x", nil, "")
	if err == nil || !strings.Contains(err.Error(), "403") || !strings.Contains(err.Error(), "Missing Access") {
		t.Fatalf("expected an error mentioning 403 and the body, got %v", err)
	}
}
