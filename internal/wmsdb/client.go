// Package wmsdb is the ModernWMS access bridge. The modernwms container has
// no bind mount for wms.db and no sqlite3 CLI — only python3 — so every
// operation here runs a generated Python script through
// `docker exec -i <container> python3 -`, printing one JSON object back on
// stdout. Each exported method wraps one such script as a typed Go
// function, replacing the original's ad hoc inline-script-per-call-site
// pattern.
package wmsdb

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/docker"
)

type Client struct {
	Container string
	DBPath    string
	Timeout   time.Duration
}

func NewClient() *Client {
	return &Client{
		Container: config.Get(config.ModernWMSContainer),
		DBPath:    config.Get(config.ModernWMSDBPath),
		Timeout:   30 * time.Second,
	}
}

func (c *Client) RunScript(ctx context.Context, pythonCode string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	return docker.Exec(ctx, c.Container, pythonCode)
}

func (c *Client) RunScriptJSON(ctx context.Context, pythonCode string, out any) error {
	stdout, err := c.RunScript(ctx, pythonCode)
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(stdout), out); err != nil {
		return fmt.Errorf("parsing wmsdb script output as JSON: %w (output: %s)", err, stdout)
	}
	return nil
}

// pyStringLit safely embeds s as a Python double-quoted string literal.
// json.Marshal produces valid Python syntax for this (Python and JSON string
// escaping agree), which sidesteps hand-written escaping bugs — this must be
// used for every dynamic value interpolated into a generated script.
func pyStringLit(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func pyIntLit(n int) string {
	return fmt.Sprintf("%d", n)
}

func (c *Client) connectPrelude() string {
	return fmt.Sprintf("import sqlite3, json\ncon = sqlite3.connect(%s)\ncon.row_factory = sqlite3.Row\ncur = con.cursor()\n", pyStringLit(c.DBPath))
}
