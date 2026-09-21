// Package img fetches part and set pictures from a small allow-list of hosts,
// caches them on disk, and draws them in a terminal as Unicode half-blocks (or
// plain ASCII where colour or UTF-8 can't be relied on). It works over telnet: it
// emits only ordinary text and colour codes, never a terminal graphics protocol.
package img

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // registers the decoders image.Decode uses
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	maxDownload   = 2 << 20  // one picture
	maxPixels     = 16 << 20 // decoded size, so a tiny file can't expand into gigabytes
	missTTL       = 7 * 24 * time.Hour
	DefaultMaxDir = 200 << 20 // cache size cap
)

// allowedHosts are the only servers pictures are fetched from.
var allowedHosts = map[string]bool{
	"cdn.rebrickable.com": true,
	"img.bricklink.com":   true,
}

// ErrNoPicture means there is no picture to show (unknown, blocked or missing upstream).
var ErrNoPicture = errors.New("no picture available")

// Fetcher downloads and caches pictures.
type Fetcher struct {
	Dir     string // cache directory; "" disables the disk cache
	MaxDir  int64  // cache size cap in bytes (0 = DefaultMaxDir)
	HTTP    *http.Client
	Offline bool // never download; serve only what is cached
}

// NewFetcher returns a Fetcher caching in dir.
func NewFetcher(dir string) *Fetcher {
	return &Fetcher{Dir: dir, HTTP: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 || !allowed(req.URL) {
			return errors.New("redirect to a host that is not allowed")
		}
		return nil
	}}}
}

// Allowed reports whether a picture may be fetched from raw.
func Allowed(raw string) bool {
	u, err := url.Parse(normalise(raw))
	return err == nil && allowed(u)
}

func allowed(u *url.URL) bool {
	return u.Scheme == "https" && allowedHosts[strings.ToLower(u.Hostname())] && u.User == nil && u.Port() == ""
}

// normalise turns BrickLink's protocol-relative URLs ("//img.bricklink.com/...") into https ones.
func normalise(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	return raw
}

func (f *Fetcher) path(raw, ext string) string {
	sum := sha256.Sum256([]byte(raw))
	return filepath.Join(f.Dir, hex.EncodeToString(sum[:16])+ext)
}

// Get returns the picture's bytes, from the cache when it has them. A picture
// that is missing upstream is remembered for a week, so a page with no picture
// does not ask again on every visit.
func (f *Fetcher) Get(ctx context.Context, raw string) ([]byte, error) {
	raw = normalise(raw)
	u, err := url.Parse(raw)
	if err != nil || !allowed(u) {
		return nil, fmt.Errorf("%w: pictures come only from %s", ErrNoPicture, strings.Join(sortedHosts(), ", "))
	}
	return f.getURL(ctx, raw)
}

// getURL is Get after the host check (tests call it directly with a local server).
func (f *Fetcher) getURL(ctx context.Context, raw string) ([]byte, error) {
	if f.Dir != "" {
		if b, err := os.ReadFile(f.path(raw, ".img")); err == nil && len(b) > 0 {
			now := time.Now()
			_ = os.Chtimes(f.path(raw, ".img"), now, now) // most recently used stays longest
			return b, nil
		}
		if fi, err := os.Stat(f.path(raw, ".miss")); err == nil && time.Since(fi.ModTime()) < missTTL {
			return nil, ErrNoPicture
		}
	}
	if f.Offline {
		return nil, fmt.Errorf("%w (offline, and it is not cached)", ErrNoPicture)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "wms-go/1.0 (+personal LEGO inventory)")
	resp, err := f.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNoPicture, stripURL(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusForbidden {
		f.remember(raw, ".miss", []byte{'x'})
		return nil, ErrNoPicture
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: HTTP %d", ErrNoPicture, resp.StatusCode)
	}
	if ct := strings.ToLower(resp.Header.Get("Content-Type")); !strings.HasPrefix(ct, "image/png") && !strings.HasPrefix(ct, "image/jpeg") {
		return nil, fmt.Errorf("%w: not a PNG or JPEG (%q)", ErrNoPicture, ct)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxDownload+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNoPicture, err)
	}
	if len(b) > maxDownload {
		return nil, fmt.Errorf("%w: larger than %d MB", ErrNoPicture, maxDownload>>20)
	}
	if _, err := Decode(b); err != nil { // never cache something that will not draw
		return nil, err
	}
	f.remember(raw, ".img", b)
	return b, nil
}

func stripURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

func sortedHosts() []string {
	var h []string
	for k := range allowedHosts {
		h = append(h, k)
	}
	sort.Strings(h)
	return h
}

// remember stores b and keeps the cache under its size cap by deleting the least
// recently used files first.
func (f *Fetcher) remember(raw, ext string, b []byte) {
	if f.Dir == "" {
		return
	}
	if err := os.MkdirAll(f.Dir, 0o755); err != nil {
		return
	}
	tmp, err := os.CreateTemp(f.Dir, ".tmp-*")
	if err != nil {
		return
	}
	_, werr := tmp.Write(b)
	cerr := tmp.Close()
	if werr != nil || cerr != nil || os.Rename(tmp.Name(), f.path(raw, ext)) != nil {
		os.Remove(tmp.Name())
		return
	}
	f.prune()
}

func (f *Fetcher) prune() {
	limit := f.MaxDir
	if limit <= 0 {
		limit = DefaultMaxDir
	}
	entries, err := os.ReadDir(f.Dir)
	if err != nil {
		return
	}
	type file struct {
		path string
		size int64
		mod  time.Time
	}
	var files []file
	var total int64
	for _, e := range entries {
		if e.IsDir() || !(strings.HasSuffix(e.Name(), ".img") || strings.HasSuffix(e.Name(), ".miss")) {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, file{filepath.Join(f.Dir, e.Name()), fi.Size(), fi.ModTime()})
		total += fi.Size()
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.Before(files[j].mod) })
	for _, fl := range files {
		if total <= limit {
			break
		}
		if os.Remove(fl.path) == nil {
			total -= fl.size
		}
	}
}

// Decode decodes a PNG or JPEG, refusing pictures whose decoded size would be huge.
func Decode(b []byte) (image.Image, error) {
	cfg, _, err := image.DecodeConfig(bytesReader(b))
	if err != nil {
		return nil, fmt.Errorf("%w: not a picture (%v)", ErrNoPicture, err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxPixels {
		return nil, fmt.Errorf("%w: %dx%d is too large", ErrNoPicture, cfg.Width, cfg.Height)
	}
	m, _, err := image.Decode(bytesReader(b))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNoPicture, err)
	}
	return m, nil
}

// PartURLs lists picture URLs to try for a part, best first: the requested colour,
// then other colours the part is known in (a part with no picture in one colour often
// has one in another). colors are Rebrickable colour ids.
func PartURLs(part string, colorID int, colors []int) []string {
	part = strings.TrimSpace(part)
	if part == "" || strings.ContainsAny(part, "/?#\\ ") {
		return nil
	}
	var urls []string
	seen := map[int]bool{}
	add := func(c int) {
		if c < 0 || seen[c] {
			return
		}
		seen[c] = true
		urls = append(urls, fmt.Sprintf("https://cdn.rebrickable.com/media/parts/ldraw/%d/%s.png", c, url.PathEscape(part)))
	}
	add(colorID)
	// Try the commonest colours first among the known ones: they are the likeliest to have a rendering.
	for _, pref := range []int{15, 0, 4, 1, 14, 2, 72, 71} {
		for _, c := range colors {
			if c == pref {
				add(c)
			}
		}
	}
	for _, c := range colors {
		if len(urls) >= 5 {
			break
		}
		add(c)
	}
	return urls
}

// Seed stores a picture in the cache under raw's URL without downloading it (used by
// tests and by the documentation screenshot generator, which must not touch the network).
func (f *Fetcher) Seed(raw string, b []byte) error {
	if f.Dir == "" {
		return errors.New("no cache directory")
	}
	if _, err := Decode(b); err != nil {
		return err
	}
	f.remember(normalise(raw), ".img", b)
	return nil
}
