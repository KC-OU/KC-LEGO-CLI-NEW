package notify

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

// Channels are read from a projectdiscovery/notify provider-config.yaml (the
// same file its CLI uses: https://github.com/projectdiscovery/notify) at
// WMS_NOTIFY_PROVIDERS: Discord, Slack, Telegram, Teams, Google Chat, email over
// SMTP, Pushover, Gotify and custom webhooks (WhatsApp through Twilio or
// CallMeBot is a custom webhook). Each entry's id is a channel; the access policy
// routes each event to channel ids.
//
// The messages are sent by the small senders below rather than by notify's Go
// packages: every one of those imports a logger that pulls in archive libraries
// with vulnerabilities that have no fixed release, which an internet-facing
// gateway should not carry. The file format, field names and {{data}} /
// {{dataJsonString}} placeholders are notify's.
//
// NOTIFY_URL (an ntfy topic or a JSON webhook) is the channel "ntfy", and is
// what everything falls back to when there is no provider file or it can't be read.

// Channel is one place alerts can go.
type Channel struct {
	ID, Kind string
	send     func(ctx context.Context, msg string) error
}

// Send delivers one message.
func (c Channel) Send(msg string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return c.send(ctx, msg)
}

type providerFile struct {
	Discord []struct {
		ID       string `yaml:"id"`
		URL      string `yaml:"discord_webhook_url"`
		Username string `yaml:"discord_username"`
		Avatar   string `yaml:"discord_avatar"`
		Format   string `yaml:"discord_format"`
	} `yaml:"discord"`
	Slack []struct {
		ID       string `yaml:"id"`
		URL      string `yaml:"slack_webhook_url"`
		Username string `yaml:"slack_username"`
		Channel  string `yaml:"slack_channel"`
		Format   string `yaml:"slack_format"`
	} `yaml:"slack"`
	Telegram []struct {
		ID        string `yaml:"id"`
		APIKey    string `yaml:"telegram_api_key"`
		ChatID    string `yaml:"telegram_chat_id"`
		Format    string `yaml:"telegram_format"`
		ParseMode string `yaml:"telegram_parsemode"`
	} `yaml:"telegram"`
	Teams []struct {
		ID     string `yaml:"id"`
		URL    string `yaml:"teams_webhook_url"`
		Format string `yaml:"teams_format"`
	} `yaml:"teams"`
	GoogleChat []struct {
		ID     string `yaml:"id"`
		Space  string `yaml:"space"`
		Key    string `yaml:"key"`
		Token  string `yaml:"token"`
		Format string `yaml:"google_chat_format"`
	} `yaml:"googlechat"`
	SMTP []struct {
		ID              string   `yaml:"id"`
		Server          string   `yaml:"smtp_server"`
		Username        string   `yaml:"smtp_username"`
		Password        string   `yaml:"smtp_password"`
		From            string   `yaml:"from_address"`
		CC              []string `yaml:"smtp_cc"`
		Format          string   `yaml:"smtp_format"`
		Subject         string   `yaml:"subject"`
		HTML            bool     `yaml:"smtp_html"`
		DisableStartTLS bool     `yaml:"smtp_disable_starttls"`
	} `yaml:"smtp"`
	Pushover []struct {
		ID      string   `yaml:"id"`
		Token   string   `yaml:"pushover_api_token"`
		User    string   `yaml:"pushover_user_key"`
		Devices []string `yaml:"pushover_devices"`
		Format  string   `yaml:"pushover_format"`
	} `yaml:"pushover"`
	Gotify []struct {
		ID         string `yaml:"id"`
		Host       string `yaml:"gotify_host"`
		Port       string `yaml:"gotify_port"`
		Token      string `yaml:"gotify_token"`
		Format     string `yaml:"gotify_format"`
		DisableTLS bool   `yaml:"gotify_disabletls"`
		Title      string `yaml:"gotify_title"`
	} `yaml:"gotify"`
	Custom []struct {
		ID      string            `yaml:"id"`
		URL     string            `yaml:"custom_webhook_url"`
		Method  string            `yaml:"custom_method"`
		Headers map[string]string `yaml:"custom_headers"`
		Format  string            `yaml:"custom_format"`
	} `yaml:"custom"`
}

// format fills notify's placeholders: {{data}} (the text) and {{dataJsonString}}
// (the text as a JSON string, quotes included).
func format(tmpl, msg string) string {
	if tmpl == "" {
		return msg
	}
	j, _ := json.Marshal(msg)
	return strings.NewReplacer("{{dataJsonString}}", string(j), "{{data}}", msg).Replace(tmpl)
}

var httpClient = &http.Client{Timeout: 15 * time.Second}

func post(ctx context.Context, method, target string, headers map[string]string, body []byte) error {
	if method == "" {
		method = http.MethodPost
	}
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		return err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return stripURL(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return fmt.Errorf("answered %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

func postJSON(ctx context.Context, target string, v any) error {
	b, _ := json.Marshal(v)
	return post(ctx, http.MethodPost, target, map[string]string{"Content-Type": "application/json"}, b)
}

// Channels lists every configured channel (provider-file entries, then ntfy),
// and an error describing a provider file that could not be used.
func Channels() ([]Channel, error) {
	var out []Channel
	var fileErr error
	path := config.Get(config.NotifyProviders)
	if b, err := os.ReadFile(path); err == nil {
		var f providerFile
		if err := yaml.Unmarshal(b, &f); err != nil {
			fileErr = fmt.Errorf("%s: %w", path, err)
		} else {
			out = append(out, fromFile(&f)...)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		fileErr = err
	}
	if s := FromConfig(); s != nil {
		out = append(out, Channel{ID: "ntfy", Kind: s.Format, send: func(ctx context.Context, msg string) error {
			title, body, _ := strings.Cut(msg, "\n")
			return s.Send(ctx, Message{Title: title, Body: body})
		}})
	}
	return out, fileErr
}

func fromFile(f *providerFile) []Channel {
	var out []Channel
	add := func(kind, id string, send func(ctx context.Context, msg string) error) {
		if id == "" {
			id = kind
		}
		out = append(out, Channel{ID: id, Kind: kind, send: send})
	}
	for _, p := range f.Discord {
		add("discord", p.ID, func(ctx context.Context, msg string) error {
			return postJSON(ctx, p.URL, map[string]string{"content": format(p.Format, msg), "username": p.Username, "avatar_url": p.Avatar})
		})
	}
	for _, p := range f.Slack {
		add("slack", p.ID, func(ctx context.Context, msg string) error {
			return postJSON(ctx, p.URL, map[string]string{"text": format(p.Format, msg), "username": p.Username, "channel": p.Channel})
		})
	}
	for _, p := range f.Telegram {
		add("telegram", p.ID, func(ctx context.Context, msg string) error {
			body := map[string]string{"chat_id": p.ChatID, "text": format(p.Format, msg)}
			if p.ParseMode != "" && !strings.EqualFold(p.ParseMode, "none") {
				body["parse_mode"] = p.ParseMode
			}
			return postJSON(ctx, "https://api.telegram.org/bot"+p.APIKey+"/sendMessage", body)
		})
	}
	for _, p := range f.Teams {
		add("teams", p.ID, func(ctx context.Context, msg string) error {
			return postJSON(ctx, p.URL, map[string]string{"text": format(p.Format, msg)})
		})
	}
	for _, p := range f.GoogleChat {
		add("googlechat", p.ID, func(ctx context.Context, msg string) error {
			u := "https://chat.googleapis.com/v1/spaces/" + url.PathEscape(p.Space) + "/messages?key=" + url.QueryEscape(p.Key) + "&token=" + url.QueryEscape(p.Token)
			return postJSON(ctx, u, map[string]string{"text": format(p.Format, msg)})
		})
	}
	for _, p := range f.Pushover {
		add("pushover", p.ID, func(ctx context.Context, msg string) error {
			v := url.Values{"token": {p.Token}, "user": {p.User}, "message": {format(p.Format, msg)}}
			if len(p.Devices) > 0 {
				v.Set("device", strings.Join(p.Devices, ","))
			}
			return post(ctx, http.MethodPost, "https://api.pushover.net/1/messages.json", map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, []byte(v.Encode()))
		})
	}
	for _, p := range f.Gotify {
		add("gotify", p.ID, func(ctx context.Context, msg string) error {
			scheme := "https"
			if p.DisableTLS {
				scheme = "http"
			}
			host := p.Host
			if p.Port != "" {
				host = net.JoinHostPort(p.Host, p.Port)
			}
			title := p.Title
			if title == "" {
				title = "KC-PARTS"
			}
			return postJSON(ctx, scheme+"://"+host+"/message?token="+url.QueryEscape(p.Token), map[string]any{"title": title, "message": format(p.Format, msg), "priority": 5})
		})
	}
	for _, p := range f.SMTP {
		add("smtp", p.ID, func(ctx context.Context, msg string) error {
			return sendMail(p.Server, p.Username, p.Password, p.From, p.CC, p.Subject, format(p.Format, msg), p.HTML, p.DisableStartTLS)
		})
	}
	for _, p := range f.Custom {
		add("custom", p.ID, func(ctx context.Context, msg string) error {
			return post(ctx, p.Method, p.URL, p.Headers, []byte(format(p.Format, msg)))
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// sendMail sends to the smtp_cc list (notify's email provider has no separate
// "to": the cc list is the recipients), with STARTTLS unless disabled.
func sendMail(server, user, pass, from string, to []string, subject, body string, html, noTLS bool) error {
	if len(to) == 0 {
		return errors.New("smtp: no recipients (smtp_cc)")
	}
	host := server
	if h, _, err := net.SplitHostPort(server); err == nil {
		host = h
	} else {
		server = net.JoinHostPort(server, "587")
	}
	if subject == "" {
		title, _, _ := strings.Cut(body, "\n")
		subject = "KC-PARTS: " + title
	}
	ctype := "text/plain; charset=utf-8"
	if html {
		ctype = "text/html; charset=utf-8"
	}
	msg := "From: " + from + "\r\nTo: " + strings.Join(to, ", ") + "\r\nSubject: " + strings.NewReplacer("\r", " ", "\n", " ").Replace(subject) +
		"\r\nMIME-Version: 1.0\r\nContent-Type: " + ctype + "\r\n\r\n" + strings.ReplaceAll(body, "\n", "\r\n")
	c, err := smtp.Dial(server)
	if err != nil {
		return err
	}
	defer c.Close()
	if !noTLS {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
				return err
			}
		}
	}
	if user != "" {
		if err := c.Auth(smtp.PlainAuth("", user, pass, host)); err != nil {
			return err
		}
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	for _, r := range to {
		if err := c.Rcpt(r); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte(msg)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// Route is the channels an event goes to: the policy's route, or every channel
// when the event has none.
func Route(event string, all []Channel) []Channel {
	var ids []string
	if p, err := access.Load(); err == nil {
		ids = p.Settings.NotifyRoutes[event]
	}
	if len(ids) == 0 {
		return all
	}
	var out []Channel
	for _, c := range all {
		for _, id := range ids {
			if c.ID == id {
				out = append(out, c)
			}
		}
	}
	return out
}

func init() {
	routes = func(event string) []func(context.Context, Message) error {
		chans, _ := Channels()
		var out []func(context.Context, Message) error
		for _, c := range Route(event, chans) {
			out = append(out, func(ctx context.Context, m Message) error {
				return c.send(ctx, strings.TrimSpace(m.Title+"\n"+m.Body))
			})
		}
		return out
	}
}
