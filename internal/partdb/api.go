package partdb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

// Part-DB is written through its REST API (API Platform, bearer token), never
// by inserting into its SQLite file: the direct inserts failed on the real
// schema (categories, part_lots and users all have NOT NULL columns with no
// default that Part-DB fills in itself), skipped its validation and change
// log, and would break again on any Part-DB upgrade. Reads stay on SQLite.

// ErrNoToken means no API token is configured, so nothing can be written.
var ErrNoToken = errors.New("no Part-DB API token is set — create one in Part-DB (user settings > API tokens, Edit scope) and add it under Admin > Settings & API Keys")

// APIError is a non-2xx answer from Part-DB.
type APIError struct {
	Status int
	Detail string
}

func (e *APIError) Error() string {
	switch e.Status {
	case http.StatusUnauthorized:
		return "Part-DB rejected the API token (401) — it may be wrong or expired"
	case http.StatusForbidden:
		return "Part-DB refused this action (403) — the token needs Edit scope and its user needs the API and part permissions"
	}
	if e.Detail != "" {
		return fmt.Sprintf("Part-DB API error %d: %s", e.Status, e.Detail)
	}
	return fmt.Sprintf("Part-DB API error %d", e.Status)
}

// API is a small client for the parts of Part-DB's REST API this app writes.
type API struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
	Comment string // sent as ?_comment= so Part-DB's own change log says who did it
}

// NewAPI reads the URL and token from config each time, so a token saved on
// the Settings screen applies to the very next request without a restart.
func NewAPI() *API {
	return &API{
		BaseURL: strings.TrimRight(config.Get(config.PartDBAPIURL), "/"),
		Token:   config.Get(config.PartDBAPIToken),
		HTTP:    &http.Client{Timeout: 15 * time.Second},
		Comment: "wms-go",
	}
}

func (a *API) Enabled() bool { return a.Token != "" }

func (a *API) do(ctx context.Context, method, path string, body, out any) error {
	if !a.Enabled() {
		return ErrNoToken
	}
	u := a.BaseURL + path
	if method != http.MethodGet && a.Comment != "" {
		sep := "?"
		if strings.Contains(u, "?") {
			sep = "&"
		}
		u += sep + "_comment=" + url.QueryEscape(a.Comment)
	}
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.Token)
	req.Header.Set("Accept", "application/ld+json")
	req.Header.Set("User-Agent", "wms-go")
	if body != nil {
		if method == http.MethodPatch {
			req.Header.Set("Content-Type", "application/merge-patch+json")
		} else {
			req.Header.Set("Content-Type", "application/json")
		}
	}
	resp, err := a.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("Part-DB API unreachable at %s: %w", a.BaseURL, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &APIError{Status: resp.StatusCode, Detail: errorDetail(data)}
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("unexpected Part-DB API response: %w", err)
		}
	}
	return nil
}

// errorDetail digs a human message out of an API Platform error body: the
// RFC 7807 violations list when present, else detail / hydra:description.
func errorDetail(data []byte) string {
	var e struct {
		Detail      string `json:"detail"`
		Description string `json:"hydra:description"`
		Title       string `json:"title"`
		Violations  []struct {
			PropertyPath string `json:"propertyPath"`
			Message      string `json:"message"`
		} `json:"violations"`
	}
	if json.Unmarshal(data, &e) != nil {
		return strings.TrimSpace(string(data))
	}
	if len(e.Violations) > 0 {
		var parts []string
		for _, v := range e.Violations {
			if v.PropertyPath != "" {
				parts = append(parts, v.PropertyPath+": "+v.Message)
			} else {
				parts = append(parts, v.Message)
			}
		}
		return strings.Join(parts, "; ")
	}
	for _, s := range []string{e.Detail, e.Description, e.Title} {
		if s != "" {
			return s
		}
	}
	return strings.TrimSpace(string(data))
}

type created struct {
	ID   int    `json:"id"`
	AtID string `json:"@id"`
}

func (c created) id() int {
	if c.ID > 0 {
		return c.ID
	}
	if i := strings.LastIndex(c.AtID, "/"); i >= 0 {
		n, _ := strconv.Atoi(c.AtID[i+1:])
		return n
	}
	return 0
}

// Ping checks that the URL, token and API permission all work.
func (a *API) Ping(ctx context.Context) error {
	return a.do(ctx, http.MethodGet, "/api/parts?itemsPerPage=1", nil, nil)
}

func (a *API) CreateCategory(ctx context.Context, name string, parentID int) (int, error) {
	body := map[string]any{"name": name}
	if parentID > 0 {
		body["parent"] = fmt.Sprintf("/api/categories/%d", parentID)
	}
	var c created
	if err := a.do(ctx, http.MethodPost, "/api/categories", body, &c); err != nil {
		return 0, err
	}
	return c.id(), nil
}

// PartSpec is what gets written for a new part.
type PartSpec struct {
	Name        string
	Description string
	IPN         string
	Tags        string // comma-separated, as Part-DB stores it
	MfgPN       string
	CategoryID  int
	// MinAmount is Part-DB's "minimum amount" (its own low-stock line). nil leaves
	// it alone, so a part that is not managed with a minimum keeps whatever it has.
	MinAmount *float64
}

func (a *API) CreatePart(ctx context.Context, s PartSpec) (int, error) {
	body := map[string]any{
		"name":        s.Name,
		"description": s.Description,
		"category":    fmt.Sprintf("/api/categories/%d", s.CategoryID),
		"tags":        s.Tags,
	}
	if s.IPN != "" {
		body["ipn"] = s.IPN
	}
	if s.MfgPN != "" {
		body["manufacturer_product_number"] = s.MfgPN
	}
	if s.MinAmount != nil {
		body["minamount"] = *s.MinAmount
	}
	var c created
	if err := a.do(ctx, http.MethodPost, "/api/parts", body, &c); err != nil {
		return 0, err
	}
	return c.id(), nil
}

// CreateLot adds a stock lot. Lots can't be created inline with the part
// (their fields aren't writable through the part), so this is its own call.
func (a *API) CreateLot(ctx context.Context, partID int, amount float64, locationID int, description string) (int, error) {
	body := map[string]any{
		"part":   fmt.Sprintf("/api/parts/%d", partID),
		"amount": amount,
	}
	if locationID > 0 {
		body["storage_location"] = fmt.Sprintf("/api/storage_locations/%d", locationID)
	}
	if description != "" {
		body["description"] = description
	}
	var c created
	if err := a.do(ctx, http.MethodPost, "/api/part_lots", body, &c); err != nil {
		return 0, err
	}
	return c.id(), nil
}

func (a *API) SetLotAmount(ctx context.Context, lotID int, amount float64) error {
	return a.do(ctx, http.MethodPatch, fmt.Sprintf("/api/part_lots/%d", lotID), map[string]any{"amount": amount}, nil)
}

func (a *API) DeleteLot(ctx context.Context, lotID int) error {
	return a.do(ctx, http.MethodDelete, fmt.Sprintf("/api/part_lots/%d", lotID), nil, nil)
}

func (a *API) DeletePart(ctx context.Context, partID int) error {
	return a.do(ctx, http.MethodDelete, fmt.Sprintf("/api/parts/%d", partID), nil, nil)
}

// UpdatePart merge-patches the given fields (name, tags, description, ...).
func (a *API) UpdatePart(ctx context.Context, partID int, fields map[string]any) error {
	return a.do(ctx, http.MethodPatch, fmt.Sprintf("/api/parts/%d", partID), fields, nil)
}
