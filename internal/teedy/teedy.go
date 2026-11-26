// Package teedy is a small client for a self-hosted Teedy
// (github.com/sismics/docs) instance -- deployed separately
// (docs.kcparts.xyz) for general document storage, not part of this
// project's own services. Used here only for the set-check onboarding
// flow: confirming a user's checklist/parts-sheet upload actually landed,
// and proxying a one-off upload for someone with no Teedy account of
// their own (see internal/dashboardapi/onboarding.go and
// guest_upload.go).
//
// Teedy's own `search` query parameter is parsed as a Lucene query, where
// `-` means "exclude the following term" -- a set number like "75192-1"
// searched literally returns zero results (confirmed live against a real
// instance), not "no match", so this package never relies on it. Instead
// it lists the configured instance's documents and matches substrings in
// Go, which is both correct and plenty fast at the scale a small resale
// business's document store actually reaches.
package teedy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

// Configured reports whether a Teedy instance is set up at all -- callers
// use this to skip the verify-upload/guest-upload features entirely
// rather than let every call fail with the same "not configured" error.
func Configured() bool {
	return config.Get(config.TeedyBaseURL) != "" && config.Get(config.TeedyUsername) != "" && config.Get(config.TeedyPassword) != ""
}

// Client holds one cached session cookie, re-logging in on first use and
// whenever a call comes back 401 (the session expired or was never
// established). One Client is meant to be reused across requests -- the
// mutex just protects the cached cookie from a concurrent re-login race,
// it's not a per-call lock.
type Client struct {
	baseURL  string
	username string
	password string
	http     *http.Client

	mu     sync.Mutex
	cookie string
}

// New builds a Client from the configured TEEDY_* values. Returns nil if
// Configured() is false -- every method on a nil *Client returns the same
// "not configured" error, so callers can construct one unconditionally
// and only check the error on the call they actually make.
func New() *Client {
	if !Configured() {
		return nil
	}
	return &Client{
		baseURL:  strings.TrimRight(config.Get(config.TeedyBaseURL), "/"),
		username: config.Get(config.TeedyUsername),
		password: config.Get(config.TeedyPassword),
		http:     &http.Client{Timeout: 20 * time.Second},
	}
}

func (c *Client) login() (string, error) {
	form := url.Values{"username": {c.username}, "password": {c.password}}
	resp, err := c.http.PostForm(c.baseURL+"/api/user/login", form)
	if err != nil {
		return "", fmt.Errorf("could not reach Teedy: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Teedy login failed (%d)", resp.StatusCode)
	}
	for _, ck := range resp.Cookies() {
		if ck.Name == "auth_token" {
			return ck.Value, nil
		}
	}
	return "", fmt.Errorf("Teedy login succeeded but set no auth_token cookie")
}

// sessionCookie returns the cached cookie, logging in first if there
// isn't one yet.
func (c *Client) sessionCookie() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cookie != "" {
		return c.cookie, nil
	}
	ck, err := c.login()
	if err != nil {
		return "", err
	}
	c.cookie = ck
	return ck, nil
}

// do attaches the session cookie and retries once after a fresh login on
// a 401 (the cached cookie expired).
func (c *Client) do(build func(cookie string) (*http.Request, error)) (*http.Response, error) {
	cookie, err := c.sessionCookie()
	if err != nil {
		return nil, err
	}
	req, err := build(cookie)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach Teedy: %w", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		return resp, nil
	}
	resp.Body.Close()
	c.mu.Lock()
	c.cookie = ""
	c.mu.Unlock()
	cookie, err = c.sessionCookie()
	if err != nil {
		return nil, err
	}
	req, err = build(cookie)
	if err != nil {
		return nil, err
	}
	resp, err = c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach Teedy: %w", err)
	}
	return resp, nil
}

// Document is the subset of Teedy's own document fields this package
// actually uses.
type Document struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// ListRecentDocuments returns Teedy's most recently created documents
// (its own default sort with no search term), up to limit. Matching a
// specific upload is the caller's job (see FindByTitleSubstring) -- kept
// separate so a caller that wants the raw list for its own reasons can
// get it too.
func (c *Client) ListRecentDocuments(limit int) ([]Document, error) {
	if c == nil {
		return nil, fmt.Errorf("Teedy is not configured (set %s, %s, %s)", config.TeedyBaseURL, config.TeedyUsername, config.TeedyPassword)
	}
	resp, err := c.do(func(cookie string) (*http.Request, error) {
		u := fmt.Sprintf("%s/api/document/list?limit=%d", c.baseURL, limit)
		req, err := http.NewRequest(http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Cookie", "auth_token="+cookie)
		return req, nil
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Teedy document list failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out struct {
		Documents []Document `json:"documents"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("malformed response from Teedy: %w", err)
	}
	return out.Documents, nil
}

// FindByTitleSubstring reports whether any of Teedy's recent documents
// has titleSubstring anywhere in its title, case-insensitively -- see
// this package's own doc comment for why this doesn't use Teedy's
// `search` parameter.
func (c *Client) FindByTitleSubstring(titleSubstring string) (bool, error) {
	docs, err := c.ListRecentDocuments(200)
	if err != nil {
		return false, err
	}
	needle := strings.ToLower(titleSubstring)
	for _, d := range docs {
		if strings.Contains(strings.ToLower(d.Title), needle) {
			return true, nil
		}
	}
	return false, nil
}

// UploadDocument creates a new Teedy document titled title and attaches
// fileBytes to it as filename -- used by the guest-upload proxy (see
// internal/dashboardapi/guest_upload.go), which never hands out real
// Teedy credentials to an anonymous uploader.
func (c *Client) UploadDocument(title, filename string, fileBytes []byte) error {
	if c == nil {
		return fmt.Errorf("Teedy is not configured (set %s, %s, %s)", config.TeedyBaseURL, config.TeedyUsername, config.TeedyPassword)
	}
	createResp, err := c.do(func(cookie string) (*http.Request, error) {
		form := url.Values{"title": {title}, "language": {"eng"}}
		req, err := http.NewRequest(http.MethodPut, c.baseURL+"/api/document", strings.NewReader(form.Encode()))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Cookie", "auth_token="+cookie)
		return req, nil
	})
	if err != nil {
		return err
	}
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(createResp.Body)
		return fmt.Errorf("Teedy document create failed (%d): %s", createResp.StatusCode, strings.TrimSpace(string(body)))
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		return fmt.Errorf("malformed response from Teedy: %w", err)
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := mw.WriteField("id", created.ID); err != nil {
		return err
	}
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		return err
	}
	if _, err := fw.Write(fileBytes); err != nil {
		return err
	}
	if err := mw.Close(); err != nil {
		return err
	}

	fileResp, err := c.do(func(cookie string) (*http.Request, error) {
		req, err := http.NewRequest(http.MethodPut, c.baseURL+"/api/file", bytes.NewReader(body.Bytes()))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.Header.Set("Cookie", "auth_token="+cookie)
		return req, nil
	})
	if err != nil {
		return err
	}
	defer fileResp.Body.Close()
	if fileResp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(fileResp.Body)
		return fmt.Errorf("Teedy file upload failed (%d): %s", fileResp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return nil
}
