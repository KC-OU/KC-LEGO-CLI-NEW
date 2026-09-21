package bricklink

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

func FuzzDecodeBody(f *testing.F) {
	for _, s := range []string{
		`{"meta":{"code":200,"message":"OK"},"data":{"no":"3001","weight":"2.3"}}`,
		`{"meta":{"code":401,"message":"TOKEN_IP_MISMATCHED"}}`,
		`{"data":[1,2,3]}`, `[]`, `null`, ``, `{"meta":`, `{"meta":{"code":"x"}}`,
		`{"meta":{"code":200},"data":{"min_price":"abc"}}`,
	} {
		f.Add(200, []byte(s))
		f.Add(500, []byte(s))
	}
	f.Fuzz(func(t *testing.T, status int, body []byte) {
		var it Item
		var pr Price
		var sup []Superset
		_ = decodeBody(status, body, &it) // errors are fine; a panic is not
		_ = decodeBody(status, body, &pr)
		_ = decodeBody(status, body, &sup)
	})
}

func FuzzAuthHeader(f *testing.F) {
	f.Add("https://api.bricklink.com/api/store/v1/items/PART/3001?a=b", "ck", "cs", "tk", "ts")
	f.Add("http://x/%zz?", "", "", "", "")
	f.Fuzz(func(t *testing.T, raw, ck, cs, tk, ts string) {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			return
		}
		h := authHeader("GET", u, Credentials{ck, cs, tk, ts}, time.Unix(1700000000, 0), "n")
		if !strings.HasPrefix(h, "OAuth ") {
			t.Fatalf("header = %q", h)
		}
		for _, secret := range []string{cs, ts} {
			if len(secret) >= 4 && strings.Contains(h, secret) && !strings.Contains(ck+tk+"n1700000000HMAC-SHA1oauth", secret) {
				t.Fatalf("a secret appears in the header: %q in %q", secret, h)
			}
		}
	})
}

func FuzzItemNumbers(f *testing.F) {
	for _, s := range []string{"3001", "75192", "a/b", "", "../x", "%2e%2e", "a b", "é"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, no string) {
		p, err := itemPath(Part, no)
		if err == nil && (strings.Count(p, "/") != 3 || strings.ContainsAny(p, "?#\\")) {
			t.Fatalf("itemPath(%q) = %q escapes its slot", no, p)
		}
	})
}
