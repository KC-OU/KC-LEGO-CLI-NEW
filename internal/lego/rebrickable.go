package lego

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

const rebrickableBaseURL = "https://rebrickable.com/api/v3/lego"

// Distinct errors so callers can tell "no such set" from "your key is wrong"
// from "slow down", instead of showing a raw HTTP status and JSON body.
var (
	ErrNotFound    = errors.New("not found on Rebrickable")
	ErrInvalidKey  = errors.New("Rebrickable rejected the API key")
	ErrRateLimited = errors.New("Rebrickable is rate-limiting requests — wait a few seconds and try again")
)

// RateLimitError is ErrRateLimited plus how long Rebrickable asked us to wait.
type RateLimitError struct{ After time.Duration }

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("Rebrickable is rate-limiting requests — try again in about %d s", int(e.After.Round(time.Second)/time.Second))
}
func (e *RateLimitError) Is(target error) bool { return target == ErrRateLimited }

// Client is a small, direct net/http wrapper around the Rebrickable v3 API —
// no shell-out, no jq, replacing the legacy tool's curl+jq pipeline entirely.
//
// Rebrickable allows about one request a second and bans IPs that ignore 429
// responses, and every telnet/web session is its own process, so rate limiting
// has to be shared: with a Store attached, the next-allowed time lives in the
// lego database and all processes queue behind it. Responses that don't change
// (parts, colours, categories, themes) are cached there too. The zero value of
// the tuning fields disables spacing and retries, which is what tests want.
type Client struct {
	APIKey  string
	BaseURL string
	HTTP    *http.Client

	Store       *DB           // shared cooldown + cache; nil = per-process spacing, no cache
	MinInterval time.Duration // spacing between requests
	MaxWait     time.Duration // longest the limiter sleeps before giving up (default 5s)
	RetryWait   time.Duration // retry a 429 once when Rebrickable says to wait no longer than this
	Pad         time.Duration // safety margin added to a 429's requested wait
	UserAgent   string

	local struct {
		mu   sync.Mutex
		next time.Time
	}
}

// Cache lifetimes: catalog facts barely change; a miss is remembered briefly
// so typing a wrong number repeatedly doesn't spend requests.
const (
	partTTL     = 30 * 24 * time.Hour
	colorsTTL   = 90 * 24 * time.Hour
	categoryTTL = 90 * 24 * time.Hour
	themeTTL    = 90 * 24 * time.Hour
	setTTL      = 30 * 24 * time.Hour
	notFoundTTL = 24 * time.Hour
)

func NewClient() *Client {
	return &Client{
		APIKey:      config.Get(config.RebrickableAPIKey),
		BaseURL:     config.Env("REBRICKABLE_BASE_URL", rebrickableBaseURL), // overridden only by tests
		HTTP:        &http.Client{Timeout: 10 * time.Second},
		MinInterval: 1100 * time.Millisecond,
		MaxWait:     5 * time.Second,
		RetryWait:   10 * time.Second,
		Pad:         500 * time.Millisecond,
		UserAgent:   "wms-go/1.0 (+personal LEGO inventory)",
	}
}

// NewClientFor is NewClient sharing db's cooldown and cache with every other
// process using the same lego database.
func NewClientFor(db *DB) *Client {
	c := NewClient()
	c.Store = db
	return c
}

// Enabled reports whether an API key is configured. Callers use this to
// skip the network call entirely and go straight to the local reference-
// table fallback (SearchRefSets/SearchRefParts) when no key is set.
func (c *Client) Enabled() bool { return c.APIKey != "" }

type SetInfo struct {
	SetNum string `json:"set_num"`
	Name   string `json:"name"`
	Year   int    `json:"year"`
	Pieces int    `json:"num_parts"`
}

type PartInfo struct {
	PartNum string `json:"part_num"`
	Name    string `json:"name"`
	PartCat int    `json:"part_cat_id"`
}

type pagedResults[T any] struct {
	Results []T `json:"results"`
}

var retryAfterRE = regexp.MustCompile(`(?i)available in (\d+) second`)

// retrySeconds reads how long a 429 says to wait: the Retry-After header if
// there is one, else the "Expected available in N seconds" text Rebrickable
// puts in the body, else a cautious 2.
func retrySeconds(h http.Header, body []byte) int {
	if n, err := strconv.Atoi(strings.TrimSpace(h.Get("Retry-After"))); err == nil && n >= 0 {
		return n
	}
	if m := retryAfterRE.FindSubmatch(body); m != nil {
		n, _ := strconv.Atoi(string(m[1]))
		return n
	}
	return 2
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// reserve claims the next request slot, waiting for it if needed. A wait longer
// than MaxWait is reported as a rate-limit instead of freezing the caller (the
// TUI runs these calls synchronously).
func (c *Client) reserve(ctx context.Context) error {
	if c.MinInterval <= 0 {
		return nil
	}
	var wait time.Duration
	if c.Store != nil {
		if w, err := c.Store.reserveSlot(c.MinInterval); err == nil {
			wait = w
		} // a bookkeeping failure must never block lookups
	} else {
		c.local.mu.Lock()
		now := time.Now()
		slot := now
		if c.local.next.After(now) {
			slot = c.local.next
		}
		c.local.next = slot.Add(c.MinInterval)
		c.local.mu.Unlock()
		wait = slot.Sub(now)
	}
	maxWait := c.MaxWait
	if maxWait == 0 {
		maxWait = 5 * time.Second
	}
	if wait > maxWait {
		return &RateLimitError{After: wait}
	}
	return sleepCtx(ctx, wait)
}

func (c *Client) noteThrottled(wait time.Duration) {
	until := time.Now().Add(wait)
	if c.Store != nil {
		_ = c.Store.pushCooldown(until)
		return
	}
	c.local.mu.Lock()
	if until.After(c.local.next) {
		c.local.next = until
	}
	c.local.mu.Unlock()
}

// request performs one GET through the shared limiter, retrying a 429 once
// when the wait is short. It returns the status and body of the final answer.
func (c *Client) request(ctx context.Context, path string) (int, []byte, error) {
	for attempt := 0; ; attempt++ {
		if err := c.reserve(ctx); err != nil {
			return 0, nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
		if err != nil {
			return 0, nil, err
		}
		req.Header.Set("Authorization", "key "+c.APIKey)
		req.Header.Set("Accept", "application/json")
		if c.UserAgent != "" {
			req.Header.Set("User-Agent", c.UserAgent)
		}
		resp, err := c.HTTP.Do(req)
		if err != nil {
			return 0, nil, fmt.Errorf("rebrickable request failed: %w", err)
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		resp.Body.Close()

		if resp.StatusCode == http.StatusTooManyRequests {
			wait := time.Duration(retrySeconds(resp.Header, body))*time.Second + c.Pad
			c.noteThrottled(wait)
			if attempt == 0 && wait <= c.RetryWait {
				if err := sleepCtx(ctx, wait); err != nil {
					return 0, nil, err
				}
				continue
			}
			return resp.StatusCode, nil, &RateLimitError{After: wait}
		}
		return resp.StatusCode, body, nil
	}
}

// decode maps a final answer to a result: JSON into out on 200, otherwise the
// typed error for that status.
func decode(status int, body []byte, out any) error {
	switch status {
	case http.StatusOK:
		return json.Unmarshal(body, out)
	case http.StatusNotFound:
		return ErrNotFound
	case http.StatusUnauthorized, http.StatusForbidden:
		return ErrInvalidKey
	case http.StatusTooManyRequests:
		return ErrRateLimited
	}
	if len(body) > 4096 {
		body = body[:4096]
	}
	return fmt.Errorf("rebrickable returned HTTP %d: %s", status, body)
}

// get is one uncached request, for searches whose results change.
func (c *Client) get(ctx context.Context, path string, out any) error {
	status, body, err := c.request(ctx, path)
	if err != nil {
		return err
	}
	return decode(status, body, out)
}

// getCached serves stable lookups from the shared cache when it has a fresh
// answer (including a remembered "not found") and stores what it fetches.
func (c *Client) getCached(ctx context.Context, path string, ttl time.Duration, out any) error {
	if c.Store != nil {
		if status, body, ok := c.Store.cacheGet(path, ttl); ok {
			return decode(status, body, out)
		}
	}
	status, body, err := c.request(ctx, path)
	if err != nil {
		return err
	}
	if c.Store != nil && (status == http.StatusOK || status == http.StatusNotFound) {
		c.Store.cachePut(path, status, body)
	}
	return decode(status, body, out)
}

// SearchSets hits Rebrickable's set search endpoint, capped to 25 results —
// this is an interactive lookup screen, not a bulk export.
func (c *Client) SearchSets(ctx context.Context, term string) ([]SetInfo, error) {
	var page pagedResults[SetInfo]
	q := "?page_size=25&search=" + url.QueryEscape(term)
	if err := c.get(ctx, "/sets/"+q, &page); err != nil {
		return nil, err
	}
	return page.Results, nil
}

func (c *Client) SearchParts(ctx context.Context, term string) ([]PartInfo, error) {
	var page pagedResults[PartInfo]
	q := "?page_size=25&search=" + url.QueryEscape(term)
	if err := c.get(ctx, "/parts/"+q, &page); err != nil {
		return nil, err
	}
	return page.Results, nil
}

// SetDetail is one set as Rebrickable describes it. Rebrickable gives a theme
// id, not a name — ThemePath turns that into text — and has no minimum-age
// or instruction-book data.
type SetDetail struct {
	SetNum  string `json:"set_num"`
	Name    string `json:"name"`
	Year    int    `json:"year"`
	ThemeID int    `json:"theme_id"`
	Pieces  int    `json:"num_parts"`
}

// rebrickableSetNum adds the "-1" version suffix Rebrickable requires when
// the caller typed a bare number like "71788".
func rebrickableSetNum(n string) string {
	n = strings.TrimSpace(n)
	if n != "" && !strings.Contains(n, "-") {
		n += "-1"
	}
	return n
}

// GetSet fetches one set by number ("71788" and "71788-1" both work).
func (c *Client) GetSet(ctx context.Context, setNum string) (*SetDetail, error) {
	num := rebrickableSetNum(setNum)
	if num == "" {
		return nil, ErrNotFound
	}
	var d SetDetail
	if err := c.getCached(ctx, "/sets/"+url.PathEscape(num)+"/", setTTL, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// ThemePath resolves a theme id to the collection's naming style, parent
// first: "Star Wars - The Book of Boba Fett". At most four levels are
// followed, so a malformed cycle can't loop.
func (c *Client) ThemePath(ctx context.Context, id int) (string, error) {
	var names []string
	for depth := 0; id > 0 && depth < 4; depth++ {
		var t struct {
			Name     string `json:"name"`
			ParentID *int   `json:"parent_id"`
		}
		if err := c.getCached(ctx, fmt.Sprintf("/themes/%d/", id), themeTTL, &t); err != nil {
			if len(names) > 0 {
				break // keep what was resolved rather than lose the theme entirely
			}
			return "", err
		}
		names = append([]string{t.Name}, names...)
		if t.ParentID == nil {
			break
		}
		id = *t.ParentID
	}
	return strings.Join(names, " - "), nil
}

// PartDetail is one part as Rebrickable describes it.
type PartDetail struct {
	PartNum string `json:"part_num"`
	Name    string `json:"name"`
	CatID   int    `json:"part_cat_id"`
	// ExternalIDs maps other catalogs' numbers for this part ("BrickLink",
	// "BrickOwl", "LDraw", "LEGO") to lists of ids; entries can be missing.
	ExternalIDs map[string]any `json:"external_ids"`
}

// ExternalID returns the first id another catalog (e.g. "BrickLink") uses for this part.
func (d *PartDetail) ExternalID(system string) string {
	list, _ := d.ExternalIDs[system].([]any)
	if len(list) == 0 {
		return ""
	}
	return fmt.Sprint(list[0])
}

func (c *Client) GetPart(ctx context.Context, partNum string) (*PartDetail, error) {
	partNum = strings.TrimSpace(partNum)
	if partNum == "" {
		return nil, ErrNotFound
	}
	var d PartDetail
	if err := c.getCached(ctx, "/parts/"+url.PathEscape(partNum)+"/", partTTL, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// PartCategory resolves a part category id ("Bricks", "Technic Pins", ...).
func (c *Client) PartCategory(ctx context.Context, id int) (string, error) {
	var cat struct {
		Name string `json:"name"`
	}
	if err := c.getCached(ctx, fmt.Sprintf("/part_categories/%d/", id), categoryTTL, &cat); err != nil {
		return "", err
	}
	return cat.Name, nil
}

// PartColor is one colour a part exists in.
type PartColor struct {
	ColorID   int    `json:"color_id"`
	ColorName string `json:"color_name"`
	NumSets   int    `json:"num_sets"`
}

// GetPartColors lists the colours a part actually exists in.
func (c *Client) GetPartColors(ctx context.Context, partNum string) ([]PartColor, error) {
	partNum = strings.TrimSpace(partNum)
	if partNum == "" {
		return nil, ErrNotFound
	}
	var page pagedResults[PartColor]
	if err := c.getCached(ctx, "/parts/"+url.PathEscape(partNum)+"/colors/?page_size=1000", partTTL, &page); err != nil {
		return nil, err
	}
	return page.Results, nil
}

// ColorInfo is one entry of Rebrickable's colour table; its ExternalIDs map to
// BrickLink/LEGO/LDraw colour numbers ({"BrickLink": {"ext_ids": [5], ...}}).
type ColorInfo struct {
	ID          int                       `json:"id"`
	Name        string                    `json:"name"`
	RGB         string                    `json:"rgb"`
	IsTrans     bool                      `json:"is_trans"`
	ExternalIDs map[string]map[string]any `json:"external_ids"`
}

// AllColors fetches the whole colour table (about 275 rows, one request).
func (c *Client) AllColors(ctx context.Context) ([]ColorInfo, error) {
	var page pagedResults[ColorInfo]
	if err := c.getCached(ctx, "/colors/?page_size=1000", colorsTTL, &page); err != nil {
		return nil, err
	}
	return page.Results, nil
}
