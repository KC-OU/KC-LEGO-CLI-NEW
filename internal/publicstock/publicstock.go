// Package publicstock is a deliberately tiny, separate, read-only HTTP service
// for the one question a customer may ask: "do you have part X?". It answers
// "in stock", "low stock" or "out of stock" -- never a quantity, a price, a
// location or anything else -- from a read-only handle on lego.db, and rate
// limits per client. It is meant to sit behind Cloudflare; run it with
// `wms public-stock`. It shares no code path with the dashboard API, so
// exposing it exposes nothing else.
package publicstock

import (
	"database/sql"
	"encoding/json"
	"net"
	"net/http"
	"regexp"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

var partRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,39}$`)

// OpenReadOnly opens lego.db so that nothing this service does can write to it.
func OpenReadOnly(path string) (*sql.DB, error) {
	return sql.Open("sqlite", "file:"+path+"?mode=ro&_pragma=query_only(1)&_pragma=busy_timeout(5000)")
}

type limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	hits   map[string][]time.Time
}

func (l *limiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cut := now.Add(-l.window)
	kept := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.max {
		l.hits[key] = kept
		return false
	}
	l.hits[key] = append(kept, now)
	if len(l.hits) > 10000 { // bound memory: drop idle keys
		for k, v := range l.hits {
			if len(v) == 0 || v[len(v)-1].Before(cut) {
				delete(l.hits, k)
			}
		}
	}
	return true
}

// Status maps a held quantity to the only thing customers are told.
func Status(qty int) string {
	switch {
	case qty <= 0:
		return "out of stock"
	case qty <= 10:
		return "low stock"
	}
	return "in stock"
}

// clientKey is the caller's address; Cloudflare's CF-Connecting-IP is trusted
// only when the connection comes from this machine (the tunnel agent).
func clientKey(r *http.Request) string {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		if cf := r.Header.Get("CF-Connecting-IP"); net.ParseIP(cf) != nil {
			return cf
		}
	}
	return host
}

// Handler serves GET /stock?part=3001. perMinute is the per-client limit.
func Handler(db *sql.DB, perMinute int) http.Handler {
	lim := &limiter{max: perMinute, window: time.Minute, hits: map[string][]time.Time{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /stock", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if !lim.allow(clientKey(r), time.Now()) {
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"too many requests"}`))
			return
		}
		part := r.URL.Query().Get("part")
		if !partRE.MatchString(part) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"give a part number, e.g. ?part=3001"}`))
			return
		}
		var qty int
		if err := db.QueryRow(`SELECT COALESCE(SUM(qty), 0) FROM owned_parts WHERE part_num = ?`, part).Scan(&qty); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"temporarily unavailable"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"part": part, "status": Status(qty)})
	})
	return http.MaxBytesHandler(mux, 1<<10)
}
