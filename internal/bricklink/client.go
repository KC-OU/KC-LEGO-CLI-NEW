package bricklink

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const defaultBaseURL = "https://api.bricklink.com/api/store/v1"

// Credentials are the four values BrickLink issues (consumer key/secret for the
// API consumer, token value/secret for the IP address it is bound to).
type Credentials struct {
	ConsumerKey, ConsumerSecret, Token, TokenSecret string
}

// Complete reports whether all four values are present.
func (c Credentials) Complete() bool {
	return c.ConsumerKey != "" && c.ConsumerSecret != "" && c.Token != "" && c.TokenSecret != ""
}

// Store is the shared bookkeeping the client needs: a response cache, a daily call
// budget and request spacing. lego.DB provides it, so every session (each telnet
// user is its own process) counts against one budget.
type Store interface {
	APICacheGet(key string, ttl time.Duration) (status int, body []byte, ok bool)
	APICachePut(key string, status int, body []byte)
	// SpendAPIBudget adds one call to today's count unless limit is reached.
	SpendAPIBudget(name string, limit int, now time.Time) (used int, allowed bool, err error)
	// ReserveAPISlot returns how long to wait before the next request may go out.
	ReserveAPISlot(name string, interval time.Duration) (time.Duration, error)
}

// Client talks to BrickLink. The zero HTTP client and BaseURL are filled in by New.
type Client struct {
	Creds       Credentials
	Currency    string // price guide currency, e.g. "GBP"
	Region      string // price guide region, e.g. "europe"; "" = worldwide
	BaseURL     string
	HTTP        *http.Client
	Store       Store
	DailyBudget int           // hard cap on real requests per UTC day (BrickLink's own limit is 5,000)
	MinInterval time.Duration // spacing between requests
	Now         func() time.Time
}

// New returns a client with safe defaults (4,500 calls a day, 250 ms apart).
func New(creds Credentials, store Store) *Client {
	return &Client{
		Creds: creds, Currency: "GBP", Region: "europe", BaseURL: defaultBaseURL,
		HTTP: &http.Client{Timeout: 20 * time.Second}, Store: store,
		DailyBudget: 4500, MinInterval: 250 * time.Millisecond, Now: time.Now,
	}
}

// Enabled reports whether credentials are configured.
func (c *Client) Enabled() bool { return c != nil && c.Creds.Complete() }

// Errors are typed so callers (and exit codes) can tell them apart.
var (
	ErrNotConfigured = errors.New("BrickLink is not set up: add the four API values under Admin > Settings & API Keys > BrickLink API")
	ErrNotFound      = errors.New("not found on BrickLink")
	ErrAuth          = errors.New("BrickLink rejected the credentials (wrong key, secret, token or token secret)")
	ErrIPMismatch    = errors.New("BrickLink refused the token because it is bound to a different IP address than this server's (run `wms bricklink whoami`, then update the registration at bricklink.com)")
	ErrRateLimited   = errors.New("BrickLink is rate-limiting requests — try again later")
	ErrBudget        = errors.New("today's BrickLink call budget is used up (it resets at midnight UTC); results already cached still work")
)

// APIError is any other error BrickLink reports.
type APIError struct {
	Code        int
	Message     string
	Description string
}

func (e *APIError) Error() string {
	if e.Description != "" {
		return fmt.Sprintf("BrickLink: %s (%d): %s", e.Message, e.Code, e.Description)
	}
	return fmt.Sprintf("BrickLink: %s (%d)", e.Message, e.Code)
}

type envelope struct {
	Meta struct {
		Description string `json:"description"`
		Message     string `json:"message"`
		Code        int    `json:"code"`
	} `json:"meta"`
	Data json.RawMessage `json:"data"`
}

// classify turns an envelope's meta into a typed error (nil for success).
func classify(status int, m struct {
	Description string `json:"description"`
	Message     string `json:"message"`
	Code        int    `json:"code"`
}) error {
	code := m.Code
	if code == 0 {
		code = status
	}
	if code >= 200 && code < 300 {
		return nil
	}
	switch msg := strings.ToUpper(m.Message); {
	case strings.Contains(msg, "TOKEN_IP_MISMATCHED"):
		return ErrIPMismatch
	case code == 404 || strings.Contains(msg, "RESOURCE_NOT_FOUND"):
		return ErrNotFound
	case code == 401 || code == 403 || strings.Contains(msg, "TOKEN") || strings.Contains(msg, "OAUTH") || strings.Contains(msg, "SIGNATURE") || strings.Contains(msg, "CONSUMER"):
		return ErrAuth
	case code == 429 || strings.Contains(msg, "RATE"):
		return ErrRateLimited
	}
	return &APIError{Code: code, Message: m.Message, Description: m.Description}
}

// Cache lifetimes: catalogue facts rarely change, prices move daily.
const (
	ItemTTL  = 30 * 24 * time.Hour
	PriceTTL = 24 * time.Hour
	notFound = 24 * time.Hour
)

// get performs (or serves from cache) one GET and decodes the "data" part into out.
func (c *Client) get(ctx context.Context, path string, q url.Values, ttl time.Duration, out any) error {
	if !c.Enabled() {
		return ErrNotConfigured
	}
	key := "bl:" + path
	if len(q) > 0 {
		keys := make([]string, 0, len(q))
		for k := range q {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for i, k := range keys {
			sep := "&"
			if i == 0 {
				sep = "?"
			}
			key += sep + k + "=" + q.Get(k)
		}
	}
	if c.Store != nil {
		if status, body, ok := c.Store.APICacheGet(key, ttl); ok {
			return decodeBody(status, body, out)
		}
		used, allowed, err := c.Store.SpendAPIBudget("bricklink", c.DailyBudget, c.Now())
		if err == nil && !allowed {
			_ = used
			return ErrBudget
		}
		if wait, err := c.Store.ReserveAPISlot("bricklink", c.MinInterval); err == nil && wait > 0 {
			if wait > 10*time.Second {
				return ErrRateLimited
			}
			t := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				t.Stop()
				return ctx.Err()
			case <-t.C:
			}
		}
	}

	u, err := url.Parse(strings.TrimRight(c.BaseURL, "/") + path)
	if err != nil {
		return err
	}
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", authHeader(http.MethodGet, u, c.Creds, c.Now(), nonce()))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "wms-go/1.0 (+personal LEGO inventory)")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("BrickLink request failed: %w", stripURL(err))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if c.Store != nil && (resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNotFound) {
		var env envelope
		// only cache well-formed answers, and only success or "not found" (never an auth or budget error)
		if json.Unmarshal(body, &env) == nil && (classify(resp.StatusCode, env.Meta) == nil || errors.Is(classify(resp.StatusCode, env.Meta), ErrNotFound)) {
			c.Store.APICachePut(key, resp.StatusCode, body)
		}
	}
	return decodeBody(resp.StatusCode, body, out)
}

func stripURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

func decodeBody(status int, body []byte, out any) error {
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		if status >= 400 {
			return &APIError{Code: status, Message: http.StatusText(status)}
		}
		return fmt.Errorf("BrickLink sent something that is not JSON (HTTP %d)", status)
	}
	if err := classify(status, env.Meta); err != nil {
		return err
	}
	if out == nil || len(env.Data) == 0 {
		return nil
	}
	return json.Unmarshal(env.Data, out)
}

// flexFloat reads a number BrickLink sends either as a JSON number or as a string ("0.0100").
type flexFloat float64

func (f *flexFloat) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return err
	}
	*f = flexFloat(v)
	return nil
}

// ItemType is a BrickLink catalogue type.
type ItemType string

const (
	Part    ItemType = "PART"
	Set     ItemType = "SET"
	Minifig ItemType = "MINIFIG"
)

// Item is a catalogue entry.
type Item struct {
	No           string    `json:"no"`
	Name         string    `json:"name"`
	Type         string    `json:"type"`
	CategoryID   int       `json:"category_id"`
	ImageURL     string    `json:"image_url"`
	ThumbnailURL string    `json:"thumbnail_url"`
	YearReleased int       `json:"year_released"`
	Weight       flexFloat `json:"weight"`
	IsObsolete   bool      `json:"is_obsolete"`
	AlternateNo  string    `json:"alternate_no"`
}

// SetNo gives a set number BrickLink's form ("75192" -> "75192-1").
func SetNo(n string) string {
	n = strings.TrimSpace(n)
	if n != "" && !strings.Contains(n, "-") {
		n += "-1"
	}
	return n
}

func itemPath(t ItemType, no string) (string, error) {
	no = strings.TrimSpace(no)
	if no == "" || strings.ContainsAny(no, "/?#\\") {
		return "", fmt.Errorf("%q is not a valid BrickLink item number", no)
	}
	if t == Set {
		no = SetNo(no)
	}
	return "/items/" + string(t) + "/" + url.PathEscape(no), nil
}

// GetItem looks up a part, set or minifigure by its BrickLink number.
func (c *Client) GetItem(ctx context.Context, t ItemType, no string) (*Item, error) {
	p, err := itemPath(t, no)
	if err != nil {
		return nil, err
	}
	var it Item
	if err := c.get(ctx, p, nil, ItemTTL, &it); err != nil {
		return nil, err
	}
	return &it, nil
}

// KnownColor is a colour an item exists in, and how many sets contain it in that colour.
type KnownColor struct {
	ColorID  int `json:"color_id"`
	Quantity int `json:"quantity"`
}

// KnownColors lists the colours BrickLink knows a part in.
func (c *Client) KnownColors(ctx context.Context, t ItemType, no string) ([]KnownColor, error) {
	p, err := itemPath(t, no)
	if err != nil {
		return nil, err
	}
	var out []KnownColor
	if err := c.get(ctx, p+"/colors", nil, ItemTTL, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Price is BrickLink's price guide for one item (and colour), in the client's currency.
type Price struct {
	NewOrUsed     string    `json:"new_or_used"`
	CurrencyCode  string    `json:"currency_code"`
	Min           flexFloat `json:"min_price"`
	Max           flexFloat `json:"max_price"`
	Avg           flexFloat `json:"avg_price"`
	QtyAvg        flexFloat `json:"qty_avg_price"`
	UnitQuantity  int       `json:"unit_quantity"`
	TotalQuantity int       `json:"total_quantity"`
}

// Guide selects the price guide: "sold" (last six months) or "stock" (for sale now).
type Guide string

const (
	Sold  Guide = "sold"
	Stock Guide = "stock"
)

// PriceGuide fetches the price guide for a part or set. condition is "N" (new) or
// "U" (used); colorID 0 means no colour (sets, or a part with no colour).
func (c *Client) PriceGuide(ctx context.Context, t ItemType, no string, colorID int, g Guide, condition string) (*Price, error) {
	p, err := itemPath(t, no)
	if err != nil {
		return nil, err
	}
	if condition != "N" && condition != "U" {
		return nil, fmt.Errorf("condition must be N (new) or U (used), not %q", condition)
	}
	if g != Sold && g != Stock {
		return nil, fmt.Errorf("guide must be sold or stock, not %q", g)
	}
	q := url.Values{"guide_type": {string(g)}, "new_or_used": {condition}}
	if c.Currency != "" {
		q.Set("currency_code", c.Currency)
	}
	if c.Region != "" {
		q.Set("region", c.Region)
	}
	if colorID > 0 {
		q.Set("color_id", strconv.Itoa(colorID))
	}
	var pr Price
	if err := c.get(ctx, p+"/price", q, PriceTTL, &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

// SupersetEntry is one set (or other item) that contains the part.
type SupersetEntry struct {
	Item      Item   `json:"item"`
	Quantity  int    `json:"quantity"`
	AppearsAs string `json:"appears_as"`
}

// Superset groups entries by the colour the part appears in.
type Superset struct {
	ColorID int             `json:"color_id"`
	Entries []SupersetEntry `json:"entries"`
}

// Supersets lists what contains a part ("where used"). colorID 0 means every colour.
func (c *Client) Supersets(ctx context.Context, t ItemType, no string, colorID int) ([]Superset, error) {
	p, err := itemPath(t, no)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	if colorID > 0 {
		q.Set("color_id", strconv.Itoa(colorID))
	}
	var out []Superset
	if err := c.get(ctx, p+"/supersets", q, ItemTTL, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Color is a BrickLink colour.
type Color struct {
	ColorID   int    `json:"color_id"`
	ColorName string `json:"color_name"`
	ColorCode string `json:"color_code"`
	ColorType string `json:"color_type"`
}

// Colors fetches BrickLink's whole colour table (one call, cached for a month).
func (c *Client) Colors(ctx context.Context) ([]Color, error) {
	var out []Color
	if err := c.get(ctx, "/colors", nil, ItemTTL, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Ping makes the cheapest authenticated call, to check credentials and IP binding.
// It bypasses the cache so a stale success can't hide a broken token.
func (c *Client) Ping(ctx context.Context) error {
	if !c.Enabled() {
		return ErrNotConfigured
	}
	cc := *c // a copy without the cache, so a stale success can't hide a broken token
	cc.Store = nil
	var out []Color
	return cc.get(ctx, "/colors", nil, 0, &out)
}
