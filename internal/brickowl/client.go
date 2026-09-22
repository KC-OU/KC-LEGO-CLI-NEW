// Package brickowl reads prices from BrickOwl's API (https://www.brickowl.com/api_docs):
// an item's BOID is looked up from a LEGO element id (colour-specific) or a design
// number, then its live availability gives the lowest and average asking price.
// It needs BRICKOWL_API_KEY; the availability call also needs BrickOwl to have
// approved the key for catalog access.
package brickowl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

// ErrNotFound means BrickOwl does not know the item (or has none for sale).
var ErrNotFound = errors.New("not on BrickOwl")

type Client struct {
	Key, Country, BaseURL string
	HTTP                  *http.Client
}

// FromConfig returns the configured client (Enabled reports whether a key is set).
func FromConfig() *Client {
	base := "https://api.brickowl.com/v1"
	if v := os.Getenv("BRICKOWL_BASE_URL"); v != "" {
		base = v // tests
	}
	country := strings.ToUpper(strings.TrimSpace(config.Get(config.BrickOwlCountry)))
	return &Client{Key: strings.TrimSpace(config.Get(config.BrickOwlAPIKey)), Country: country, BaseURL: base, HTTP: &http.Client{Timeout: 15 * time.Second}}
}

func (c *Client) Enabled() bool { return c != nil && c.Key != "" }

func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	q.Set("key", c.Key)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.BaseURL, "/")+path+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("BrickOwl: %v", strings.ReplaceAll(err.Error(), c.Key, "***"))
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return ErrNotFound
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized:
		return fmt.Errorf("BrickOwl refused the key (%d): check BRICKOWL_API_KEY and that it is approved for catalog access", resp.StatusCode)
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("BrickOwl answered %d: %s", resp.StatusCode, strings.TrimSpace(string(body[:min(len(body), 200)])))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("BrickOwl sent something unexpected: %v", err)
	}
	return nil
}

// Lookup finds the BOID for an element id (preferred: it names the colour) or,
// failing that, a design / part number.
func (c *Client) Lookup(ctx context.Context, elementID, designID string) (string, error) {
	try := func(id, idType string) (string, error) {
		var r struct {
			BOIDs []string `json:"boids"`
		}
		if err := c.get(ctx, "/catalog/id_lookup", url.Values{"id": {id}, "type": {"Part"}, "id_type": {idType}}, &r); err != nil {
			return "", err
		}
		if len(r.BOIDs) == 0 {
			return "", ErrNotFound
		}
		return r.BOIDs[0], nil
	}
	if elementID != "" {
		if b, err := try(elementID, "element_id"); err == nil {
			return b, nil
		} else if !errors.Is(err, ErrNotFound) {
			return "", err
		}
	}
	if designID == "" {
		return "", ErrNotFound
	}
	return try(designID, "design_id")
}

// Price is what BrickOwl sellers ask for an item right now.
type Price struct {
	BOID          string
	Low, Avg      float64
	Currency      string
	Listings, Qty int
}

// Availability summarises the live listings for a BOID (new and used together).
func (c *Client) Availability(ctx context.Context, boid string) (*Price, error) {
	q := url.Values{"boid": {boid}}
	if c.Country != "" {
		q.Set("country", c.Country)
	}
	var raw map[string]struct {
		Price    json.RawMessage `json:"price"`
		Qty      json.RawMessage `json:"qty"`
		Currency string          `json:"base_currency"`
	}
	if err := c.get(ctx, "/catalog/availability", q, &raw); err != nil {
		return nil, err
	}
	p := &Price{BOID: boid}
	var prices []float64
	var weighted, units float64
	for _, l := range raw {
		pr, _ := strconv.ParseFloat(strings.Trim(string(l.Price), `"`), 64)
		qty, _ := strconv.Atoi(strings.Trim(string(l.Qty), `"`))
		if pr <= 0 {
			continue
		}
		qty = max(qty, 1)
		prices = append(prices, pr)
		weighted += pr * float64(qty)
		units += float64(qty)
		p.Qty += qty
		if p.Currency == "" {
			p.Currency = l.Currency
		}
	}
	if len(prices) == 0 {
		return nil, ErrNotFound
	}
	sort.Float64s(prices)
	p.Low, p.Avg, p.Listings = prices[0], weighted/units, len(prices)
	return p, nil
}

// PartURL is the BrickOwl page for a BOID (or a search for a part number).
func PartURL(boid, partNum string) string {
	if boid != "" {
		return "https://www.brickowl.com/catalog/" + url.PathEscape(boid)
	}
	return "https://www.brickowl.com/search/catalog?query=" + url.QueryEscape(partNum)
}
