package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

// DiscordBot DMs one Discord user directly with real image attachments — unlike
// the webhook provider in providers.go, which posts JSON text into a fixed
// server channel and can neither DM a person nor attach a file.
//
// Setup (once, in Discord, not here): create an application at
// discord.com/developers/applications, add a Bot under it, copy its token
// into WMS_DISCORD_BOT_TOKEN, and invite the bot to any one server you're
// both in — Discord only lets a bot open a DM with someone it shares a
// server with. Your own numeric user ID (enable Developer Mode in Discord's
// settings, then right-click your name → Copy User ID) goes in
// WMS_DISCORD_BOT_USER_ID.
type DiscordBot struct {
	Token, UserID string
	BaseURL       string // defaultDiscordAPI unless a test points it at an httptest server
}

const defaultDiscordAPI = "https://discord.com/api/v10"

// DiscordBotFromConfig reads WMS_DISCORD_BOT_TOKEN / WMS_DISCORD_BOT_USER_ID.
func DiscordBotFromConfig() *DiscordBot {
	return &DiscordBot{Token: config.Get(config.DiscordBotToken), UserID: config.Get(config.DiscordBotUserID), BaseURL: defaultDiscordAPI}
}

func (b *DiscordBot) Enabled() bool { return b != nil && b.Token != "" && b.UserID != "" }

func (b *DiscordBot) do(ctx context.Context, method, path string, body io.Reader, contentType string) (*http.Response, error) {
	base := b.BaseURL
	if base == "" {
		base = defaultDiscordAPI
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bot "+b.Token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
		return nil, fmt.Errorf("discord API %s %s: %d %s", method, path, resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	return resp, nil
}

// dmChannel opens (or reuses) the DM channel with UserID.
func (b *DiscordBot) dmChannel(ctx context.Context) (string, error) {
	body, _ := json.Marshal(map[string]string{"recipient_id": b.UserID})
	resp, err := b.do(ctx, http.MethodPost, "/users/@me/channels", bytes.NewReader(body), "application/json")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.ID, nil
}

// DM sends text as a direct message, with an optional image (png bytes,
// imageName e.g. "qr.png"; pass nil/"" for a text-only message).
func (b *DiscordBot) DM(ctx context.Context, text string, image []byte, imageName string) error {
	if !b.Enabled() {
		return fmt.Errorf("the Discord bot is not configured (WMS_DISCORD_BOT_TOKEN, WMS_DISCORD_BOT_USER_ID)")
	}
	channel, err := b.dmChannel(ctx)
	if err != nil {
		return fmt.Errorf("opening a DM: %w", err)
	}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	payload, _ := json.Marshal(map[string]any{"content": text})
	if err := w.WriteField("payload_json", string(payload)); err != nil {
		return err
	}
	if len(image) > 0 {
		fw, err := w.CreateFormFile("files[0]", imageName)
		if err != nil {
			return err
		}
		if _, err := fw.Write(image); err != nil {
			return err
		}
	}
	if err := w.Close(); err != nil {
		return err
	}
	resp, err := b.do(ctx, http.MethodPost, "/channels/"+channel+"/messages", &buf, w.FormDataContentType())
	if err != nil {
		return fmt.Errorf("sending the DM: %w", err)
	}
	return resp.Body.Close()
}
