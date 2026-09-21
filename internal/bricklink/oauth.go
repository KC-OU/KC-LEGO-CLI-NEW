// Package bricklink is a small client for the BrickLink Store API (v1). It signs
// requests with OAuth 1.0a (HMAC-SHA1) using only the standard library, keeps a
// hard daily budget so the account can never be blocked for over-use, and caches
// answers. BrickLink has no free-text search: everything here looks things up by
// number (parts, sets, minifigs), or asks for prices and where-used lists.
package bricklink

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

// pct is RFC 3986 percent-encoding (only unreserved characters stay), which OAuth
// requires; net/url's query escaping differs (it turns a space into "+").
func pct(s string) string {
	const hexd = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '.' || c == '_' || c == '~' {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			b.WriteByte(hexd[c>>4])
			b.WriteByte(hexd[c&15])
		}
	}
	return b.String()
}

// baseURI is the request URL without its query, scheme and host lower-cased and
// default ports dropped (RFC 5849 section 3.4.1.2).
func baseURI(u *url.URL) string {
	host := strings.ToLower(u.Host)
	switch {
	case u.Scheme == "http" && strings.HasSuffix(host, ":80"):
		host = strings.TrimSuffix(host, ":80")
	case u.Scheme == "https" && strings.HasSuffix(host, ":443"):
		host = strings.TrimSuffix(host, ":443")
	}
	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}
	return strings.ToLower(u.Scheme) + "://" + host + path
}

// signatureBase builds the OAuth signature base string from the method, URL and
// every parameter (query, body and oauth_*), sorted by encoded name then value.
func signatureBase(method string, u *url.URL, params [][2]string) string {
	enc := make([][2]string, len(params))
	for i, p := range params {
		enc[i] = [2]string{pct(p[0]), pct(p[1])}
	}
	sort.Slice(enc, func(i, j int) bool {
		if enc[i][0] != enc[j][0] {
			return enc[i][0] < enc[j][0]
		}
		return enc[i][1] < enc[j][1]
	})
	parts := make([]string, len(enc))
	for i, p := range enc {
		parts[i] = p[0] + "=" + p[1]
	}
	return strings.ToUpper(method) + "&" + pct(baseURI(u)) + "&" + pct(strings.Join(parts, "&"))
}

func sign(base, consumerSecret, tokenSecret string) string {
	mac := hmac.New(sha1.New, []byte(pct(consumerSecret)+"&"+pct(tokenSecret)))
	mac.Write([]byte(base))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func nonce() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// authHeader returns the Authorization header for a signed request. now and nonceVal
// are parameters so tests can pin them.
func authHeader(method string, u *url.URL, creds Credentials, now time.Time, nonceVal string) string {
	oauth := [][2]string{
		{"oauth_consumer_key", creds.ConsumerKey},
		{"oauth_token", creds.Token},
		{"oauth_signature_method", "HMAC-SHA1"},
		{"oauth_timestamp", fmt.Sprint(now.Unix())},
		{"oauth_nonce", nonceVal},
		{"oauth_version", "1.0"},
	}
	params := append([][2]string(nil), oauth...)
	for k, vs := range u.Query() {
		for _, v := range vs {
			params = append(params, [2]string{k, v})
		}
	}
	oauth = append(oauth, [2]string{"oauth_signature", sign(signatureBase(method, u, params), creds.ConsumerSecret, creds.TokenSecret)})
	parts := make([]string, len(oauth))
	for i, p := range oauth {
		parts[i] = pct(p[0]) + `="` + pct(p[1]) + `"`
	}
	return "OAuth " + strings.Join(parts, ", ")
}
