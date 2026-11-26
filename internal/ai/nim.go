// Package ai talks to NVIDIA NIM's OpenAI-compatible chat endpoint
// (integrate.api.nvidia.com). Whatever a model returns is untrusted text: callers
// parse it strictly and bind it as data (SQL parameters, escaped display), never
// run it. Requests carry only part names, colours and counts -- no keys or user data.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

var ErrNotConfigured = errors.New("AI helpers are not set up: add NVIDIA_NIM_API_KEY to the settings")

// BaseURL is the NIM API root; tests point it at a stub server.
var BaseURL = "https://integrate.api.nvidia.com/v1"

type Client struct {
	Key     string
	Models  []string
	Timeout time.Duration
	HTTP    *http.Client
}

func FromConfig() *Client {
	var models []string
	for _, m := range strings.Split(config.Get(config.NvidiaNIMModels), ",") {
		if m = strings.TrimSpace(m); m != "" {
			models = append(models, m)
		}
	}
	return &Client{Key: config.Get(config.NvidiaNIMKey), Models: models, Timeout: 25 * time.Second, HTTP: &http.Client{}}
}

func (c *Client) Enabled() bool { return c != nil && c.Key != "" && len(c.Models) > 0 }

// Chat asks the models in order until one gives a non-empty answer (a model that is
// retired for this key answers 404, a busy one times out, a reasoning model can spend
// its budget thinking and return nothing), so one bad model never fails the feature.
func (c *Client) Chat(ctx context.Context, system, user string, maxTokens int) (string, error) {
	if !c.Enabled() {
		return "", ErrNotConfigured
	}
	var last error
	for _, m := range c.Models {
		out, err := c.once(ctx, m, system, user, maxTokens)
		if err == nil && strings.TrimSpace(out) != "" {
			return out, nil
		}
		if err == nil {
			err = fmt.Errorf("%s returned nothing", m)
		}
		last = err
		if ctx.Err() != nil {
			break
		}
	}
	return "", fmt.Errorf("no AI model answered (last: %v)", last)
}

func (c *Client) once(ctx context.Context, model, system, user string, maxTokens int) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"model": model, "temperature": 0.2, "max_tokens": maxTokens,
		"messages": []map[string]string{{"role": "system", "content": system}, {"role": "user", "content": user}},
	})
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.Key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("%s: %w", model, stripURL(err))
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s: HTTP %d", model, resp.StatusCode)
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(raw, &out) != nil || len(out.Choices) == 0 {
		return "", fmt.Errorf("%s: unreadable answer", model)
	}
	return out.Choices[0].Message.Content, nil
}

// stripURL keeps the request URL (and so nothing sensitive) out of error text.
func stripURL(err error) error {
	var ue interface{ Unwrap() error }
	if errors.As(err, &ue) && ue.Unwrap() != nil {
		return ue.Unwrap()
	}
	return err
}

// ExtractJSON returns the outermost {...} in s (models like to wrap JSON in
// prose or ``` fences), or "" if there is none.
func ExtractJSON(s string) string {
	i, j := strings.Index(s, "{"), strings.LastIndex(s, "}")
	if i < 0 || j <= i {
		return ""
	}
	return s[i : j+1]
}
