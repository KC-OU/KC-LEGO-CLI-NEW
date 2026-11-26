// Package jev asks TypeSafe AI's Jev (https://docs.typesafe.ai) a yes/no
// question and reads back its confidence. Jev is a decision model, not a
// calculator: it answers "how likely is this true," it doesn't compute
// numbers. Used here to flag a watched BrickLink price move that looks more
// like a data glitch than a genuine market change -- a judgment call, not
// arithmetic -- so a human knows which alerts are worth a second look.
//
// Request shape (POST .../v1/systemone, {model,state,questions:{q:{type:"noul",
// instructions}}}, answer at .answers.q.noul) follows docs.typesafe.ai and was
// confirmed against a live call.
package jev

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

type Client struct {
	Key, BaseURL string
	HTTP         *http.Client
}

// FromConfig returns the configured client (Enabled reports whether a key is set).
func FromConfig() *Client {
	base := "https://api.typesafe.ai/v1"
	if v := os.Getenv("JEV_BASE_URL"); v != "" {
		base = v // tests
	}
	return &Client{Key: strings.TrimSpace(config.Get(config.JevAPIKey)), BaseURL: base, HTTP: &http.Client{Timeout: 10 * time.Second}}
}

func (c *Client) Enabled() bool { return c != nil && c.Key != "" }

// Plausible asks a boolean question and returns Jev's probability (0-1) that
// the answer is yes. A disabled client or any request/response problem
// returns an error -- the caller decides whether that should block anything
// (for a price alert, it should not: see internal/lego.CheckWatches).
func (c *Client) Plausible(ctx context.Context, question string) (float64, error) {
	if !c.Enabled() {
		return 0, fmt.Errorf("jev: no JEV_API_KEY configured")
	}
	body, _ := json.Marshal(map[string]any{
		"model":     "jev-latest",
		"state":     question,
		"questions": map[string]any{"q": map[string]string{"type": "noul", "instructions": question}},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+"/systemone", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.Key)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, fmt.Errorf("jev: %v", strings.ReplaceAll(err.Error(), c.Key, "***"))
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("jev answered %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody[:min(len(respBody), 200)])))
	}
	var r struct {
		Answers map[string]struct {
			Noul *float64 `json:"noul"`
		} `json:"answers"`
	}
	if err := json.Unmarshal(respBody, &r); err != nil || r.Answers["q"].Noul == nil {
		return 0, fmt.Errorf("jev sent something unexpected")
	}
	return *r.Answers["q"].Noul, nil
}
