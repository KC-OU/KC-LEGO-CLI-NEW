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

func TestDiscordBotDMOpensChannelThenPostsEmbedWithAttachment(t *testing.T) {
	var gotAuth, gotRecipient string
	var gotEmbed struct {
		Title, URL, Description string
		Color                   int
		Image                   struct{ URL string } `json:"image"`
	}
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
						Embeds []struct {
							Title       string `json:"title"`
							URL         string `json:"url"`
							Description string `json:"description"`
							Color       int    `json:"color"`
							Image       struct {
								URL string `json:"url"`
							} `json:"image"`
						} `json:"embeds"`
					}
					if err := json.Unmarshal(b, &payload); err != nil {
						t.Fatal(err)
					}
					if len(payload.Embeds) != 1 {
						t.Fatalf("embeds = %+v", payload.Embeds)
					}
					e := payload.Embeds[0]
					gotEmbed.Title, gotEmbed.URL, gotEmbed.Description, gotEmbed.Color = e.Title, e.URL, e.Description, e.Color
					gotEmbed.Image.URL = e.Image.URL
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
	card := DMCard{
		Title: "Missing parts for 75192", URL: "https://example.com/DL/TOKEN",
		Description: "kc · expires 14:30", Image: []byte("fake-png-bytes"), ImageName: "qr.png",
	}
	if err := b.DM(context.Background(), card); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bot tok" {
		t.Errorf("Authorization = %q, want \"Bot tok\"", gotAuth)
	}
	if gotRecipient != "999" {
		t.Errorf("recipient_id = %q, want 999", gotRecipient)
	}
	if gotEmbed.Title != card.Title || gotEmbed.URL != card.URL || gotEmbed.Description != card.Description {
		t.Errorf("embed = %+v", gotEmbed)
	}
	if gotEmbed.Image.URL != "attachment://qr.png" {
		t.Errorf("embed image url = %q, want attachment://qr.png", gotEmbed.Image.URL)
	}
	// the long link must never appear as visible message text, only behind the embed's title link
	if strings.Contains(gotEmbed.Description, "https://") {
		t.Errorf("description should not contain the raw URL: %q", gotEmbed.Description)
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
	if err := b.DM(context.Background(), DMCard{Title: "text only", URL: "https://example.com"}); err != nil {
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
	if err := b.DM(context.Background(), DMCard{Title: "x"}); err == nil {
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
	err := b.DM(context.Background(), DMCard{Title: "x"})
	if err == nil || !strings.Contains(err.Error(), "403") || !strings.Contains(err.Error(), "Missing Access") {
		t.Fatalf("expected an error mentioning 403 and the body, got %v", err)
	}
}
