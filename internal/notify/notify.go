// Package notify sends the few alerts worth waking someone for (a burst of
// failed sign-ins, a broken audit chain, a stale backup, low stock) to an ntfy
// topic or a generic webhook. It is off unless NOTIFY_URL is set, and it never
// puts a password, token or 2FA code in a message.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

// Message is one alert.
type Message struct {
	Title    string
	Body     string
	Priority int    // 1 (min) .. 5 (max); 0 = default
	Tag      string // an ntfy emoji shortcode such as "warning"
}

// Sender delivers alerts to one destination.
type Sender struct {
	URL    string
	Format string // "ntfy" or "json"
	HTTP   *http.Client
}

// FromConfig returns the configured Sender, or nil when alerts are off.
func FromConfig() *Sender {
	raw := strings.TrimSpace(config.Get(config.NotifyURL))
	if raw == "" {
		return nil
	}
	s := &Sender{URL: raw, Format: strings.ToLower(strings.TrimSpace(config.Get(config.NotifyFormat)))}
	if s.Format == "" {
		if u, err := url.Parse(raw); err == nil && strings.Contains(strings.ToLower(u.Host), "ntfy") {
			s.Format = "ntfy"
		} else {
			s.Format = "json"
		}
	}
	return s
}

// Validate rejects a URL that is not plain http(s).
func (s *Sender) Validate() error {
	u, err := url.Parse(s.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("NOTIFY_URL must be an http:// or https:// URL")
	}
	if s.Format != "ntfy" && s.Format != "json" {
		return fmt.Errorf("NOTIFY_FORMAT must be ntfy or json, not %q", s.Format)
	}
	return nil
}

func (s *Sender) client() *http.Client {
	if s.HTTP != nil {
		return s.HTTP
	}
	return &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(_ *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return errors.New("too many redirects")
		}
		return nil
	}}
}

// Send delivers m. ntfy gets the plain body with Title/Priority/Tags headers;
// anything else gets one JSON object that Slack (text), Discord (content) and
// generic receivers can all read.
func (s *Sender) Send(ctx context.Context, m Message) error {
	if err := s.Validate(); err != nil {
		return err
	}
	var body []byte
	headers := map[string]string{}
	if s.Format == "ntfy" {
		body = []byte(m.Body)
		headers["Title"] = m.Title
		if m.Priority > 0 {
			headers["Priority"] = fmt.Sprint(m.Priority)
		}
		if m.Tag != "" {
			headers["Tags"] = m.Tag
		}
	} else {
		var err error
		body, err = json.Marshal(map[string]any{
			"title": m.Title, "message": m.Body, "priority": m.Priority,
			"text": m.Title + "\n" + m.Body, "content": m.Title + "\n" + m.Body,
		})
		if err != nil {
			return err
		}
		headers["Content-Type"] = "application/json"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "wms-go")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := s.client().Do(req)
	if err != nil {
		return fmt.Errorf("sending alert: %w", stripURL(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("the alert endpoint answered %s", resp.Status)
	}
	return nil
}

// stripURL keeps the destination (which can carry a secret topic or token) out of error text.
func stripURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}
