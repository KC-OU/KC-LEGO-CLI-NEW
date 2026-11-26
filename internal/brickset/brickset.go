// Package brickset reads set data (retail prices, launch and exit dates,
// LEGO.com availability by region) from Brickset's web-services API v3. It is
// the structured source for what the LEGO.com product pages show -- those
// pages sit behind a bot challenge, this API does not. Needs a free API key
// (brickset.com/tools/webservices/requestkey) in BRICKSET_API_KEY.
package brickset

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

var ErrNotConfigured = errors.New("Brickset is not set up: request a free API key at brickset.com/tools/webservices/requestkey and set BRICKSET_API_KEY")

type Client struct {
	Key     string
	BaseURL string
	HTTP    *http.Client
}

func FromConfig() *Client {
	return &Client{Key: config.Get(config.BricksetAPIKey), BaseURL: "https://brickset.com/api/v3.asmx", HTTP: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Client) Enabled() bool { return c != nil && c.Key != "" }

// Region is LEGO.com's own listing in one market.
type Region struct {
	RetailPrice        *float64 `json:"retailPrice"`
	DateFirstAvailable string   `json:"dateFirstAvailable"`
	DateLastAvailable  string   `json:"dateLastAvailable"`
}

type Set struct {
	Number        string `json:"number"`
	NumberVariant int    `json:"numberVariant"`
	Name          string `json:"name"`
	Year          int    `json:"year"`
	Theme         string `json:"theme"`
	Pieces        *int   `json:"pieces"`
	LaunchDate    string `json:"launchDate"`
	ExitDate      string `json:"exitDate"`
	Availability  string `json:"availability"`
	LEGOCom       struct {
		US, UK, CA, DE Region
	} `json:"LEGOCom"`
}

// SetNum is the "{number}-{variant}" form used everywhere else.
func (s Set) SetNum() string { return fmt.Sprintf("%s-%d", s.Number, max(s.NumberVariant, 1)) }

// Day trims Brickset's timestamps ("2026-12-31T00:00:00") to a date, "" if unset.
func Day(s string) string {
	if len(s) >= 10 && s[4] == '-' {
		return s[:10]
	}
	return ""
}

// GetSets looks up to 100 sets by number ("75192-1"); one API call.
func (c *Client) GetSets(ctx context.Context, setNumbers []string) ([]Set, error) {
	if !c.Enabled() {
		return nil, ErrNotConfigured
	}
	if len(setNumbers) == 0 {
		return nil, nil
	}
	params, _ := json.Marshal(map[string]any{"setNumber": strings.Join(setNumbers, ","), "pageSize": 100})
	form := url.Values{"apiKey": {c.Key}, "userHash": {""}, "params": {string(params)}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+"/getSets", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// Brickset's Cloudflare returns a 403 HTML page to Go's default User-Agent.
	req.Header.Set("User-Agent", "kc-parts/1.0 (private inventory tool)")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Brickset request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	var out struct {
		Status  string `json:"status"`
		Message string `json:"message"`
		Sets    []Set  `json:"sets"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("Brickset sent something unreadable (HTTP %d)", resp.StatusCode)
	}
	if out.Status != "success" {
		return nil, fmt.Errorf("Brickset: %s", out.Message) // e.g. "Invalid API key", "API limit exceeded"
	}
	return out.Sets, nil
}
