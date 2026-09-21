// Package update checks GitHub releases for a newer wms and, when told to, installs it.
// It only ever downloads from GitHub's release hosts over https, verifies the archive against
// the release's checksums.txt, keeps the old binary as <name>.old, and never restarts anything.
package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultAPI   = "https://api.github.com"
	DefaultRepo  = "KC-OU/KC-LEGO-CLI-NEW"
	AssetPrefix  = "wms-go"
	BinaryName   = "wms"
	maxArchive   = 100 << 20
	maxBinary    = 200 << 20
	maxChecksums = 1 << 20
)

// trustedHosts are the only hosts a download may come from (GitHub's release hosts).
var trustedHosts = map[string]bool{
	"api.github.com": true, "github.com": true,
	"objects.githubusercontent.com": true, "release-assets.githubusercontent.com": true,
}

// Client talks to the release API.
type Client struct {
	API  string // base URL, default DefaultAPI
	Repo string // owner/name, default DefaultRepo
	HTTP *http.Client
}

// NewClient returns a Client for the default repository.
func NewClient() *Client {
	return &Client{API: DefaultAPI, Repo: DefaultRepo, HTTP: &http.Client{Timeout: 60 * time.Second}}
}

// hostOK allows https from a trusted host, and (for tests and mirrors set with WMS_UPDATE_URL)
// the API's own host.
func (c *Client) hostOK(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil {
		return false
	}
	api, _ := url.Parse(c.API)
	if api != nil && u.Host == api.Host && (u.Scheme == "https" || isLoopback(u.Hostname())) {
		return true
	}
	return u.Scheme == "https" && trustedHosts[strings.ToLower(u.Hostname())] && u.Port() == ""
}

func isLoopback(h string) bool { return h == "127.0.0.1" || h == "localhost" || h == "::1" }

func (c *Client) client() *http.Client {
	base := c.HTTP
	if base == nil {
		base = &http.Client{Timeout: 60 * time.Second}
	}
	cp := *base
	cp.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || !c.hostOK(req.URL.String()) {
			return errors.New("redirect to a host that is not allowed")
		}
		return nil
	}
	return &cp
}

// Asset is one downloadable file of a release.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

// Release is a GitHub release.
type Release struct {
	Tag        string  `json:"tag_name"`
	Name       string  `json:"name"`
	Body       string  `json:"body"`
	Prerelease bool    `json:"prerelease"`
	Draft      bool    `json:"draft"`
	Assets     []Asset `json:"assets"`
}

// ErrNoReleases means the repository has no published release yet.
var ErrNoReleases = errors.New("no release has been published yet")

// Latest fetches the newest published release.
func (c *Client) Latest(ctx context.Context) (*Release, error) {
	api := strings.TrimRight(c.API, "/")
	if !strings.HasPrefix(api, "https://") && !isLoopback(hostOf(api)) {
		return nil, errors.New("the update address must be https")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, api+"/repos/"+c.Repo+"/releases/latest", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "wms-go-update")
	resp, err := c.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach GitHub: %v", stripURL(err))
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, ErrNoReleases
	case http.StatusForbidden, http.StatusTooManyRequests:
		return nil, errors.New("GitHub is rate-limiting this address; try again later")
	default:
		return nil, fmt.Errorf("GitHub answered HTTP %d", resp.StatusCode)
	}
	var r Release
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&r); err != nil {
		return nil, fmt.Errorf("GitHub sent something unreadable: %w", err)
	}
	if r.Tag == "" || r.Draft {
		return nil, ErrNoReleases
	}
	return &r, nil
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func stripURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

// ---- versions ----

type semver struct {
	major, minor, patch int
	pre                 string
}

func parseVersion(v string) (semver, bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if v == "" {
		return semver{}, false
	}
	core, pre, _ := strings.Cut(v, "-")
	core, _, _ = strings.Cut(core, "+")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return semver{}, false
	}
	var n [3]int
	for i, p := range parts {
		x, err := strconv.Atoi(p)
		if err != nil || x < 0 {
			return semver{}, false
		}
		n[i] = x
	}
	return semver{n[0], n[1], n[2], pre}, true
}

// Compare returns -1, 0 or 1 comparing versions a and b ("v1.2.3", "1.2.3-rc1"); a release
// outranks its own pre-releases. ok is false when either is not a version (such as "dev").
func Compare(a, b string) (cmp int, ok bool) {
	x, okA := parseVersion(a)
	y, okB := parseVersion(b)
	if !okA || !okB {
		return 0, false
	}
	for _, d := range []int{x.major - y.major, x.minor - y.minor, x.patch - y.patch} {
		if d != 0 {
			if d < 0 {
				return -1, true
			}
			return 1, true
		}
	}
	switch {
	case x.pre == y.pre:
		return 0, true
	case x.pre == "":
		return 1, true
	case y.pre == "":
		return -1, true
	case x.pre < y.pre:
		return -1, true
	}
	return 1, true
}

// ArchiveName is the release archive for this platform ("wms-go_1.2.3_linux_amd64.tar.gz").
func ArchiveName(version, goos, goarch string) string {
	return fmt.Sprintf("%s_%s_%s_%s.tar.gz", AssetPrefix, strings.TrimPrefix(version, "v"), goos, goarch)
}

// ---- applying ----

// Options controls Apply.
type Options struct {
	Exe    string // the binary to replace; default: the running one
	GOOS   string
	GOARCH string
	Force  bool // allow a package-managed path and an equal or older version
}

// Result says what Apply did.
type Result struct {
	Version  string
	Path     string
	OldPath  string
	Archive  string
	SHA256   string
	Verified bool
}

func packageManaged(path string) bool {
	for _, p := range []string{"/usr/bin/", "/usr/sbin/", "/bin/", "/sbin/"} {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

func (c *Client) fetch(ctx context.Context, raw string, limit int64) ([]byte, error) {
	if !c.hostOK(raw) {
		return nil, fmt.Errorf("refusing to download from %s: not a GitHub release host", hostOf(raw))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "wms-go-update")
	resp, err := c.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("download failed: %v", stripURL(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download answered HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("the download is larger than %d MB: refusing it", limit>>20)
	}
	return b, nil
}

func checksumFor(sums []byte, name string) (string, bool) {
	for _, line := range strings.Split(string(sums), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name && len(f[0]) == 64 {
			return strings.ToLower(f[0]), true
		}
	}
	return "", false
}

// extractBinary pulls exactly the file named BinaryName out of a .tar.gz, refusing
// anything else in the archive (no paths, no links).
func extractBinary(archive []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("the archive is not gzip: %w", err)
	}
	tr := tar.NewReader(zr)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("the archive is damaged: %w", err)
		}
		if h.Typeflag != tar.TypeReg || filepath.Base(h.Name) != BinaryName || strings.Contains(h.Name, "..") {
			continue
		}
		if h.Size <= 0 || h.Size > maxBinary {
			return nil, fmt.Errorf("the binary in the archive has an implausible size (%d bytes)", h.Size)
		}
		b, err := io.ReadAll(io.LimitReader(tr, maxBinary+1))
		if err != nil {
			return nil, err
		}
		return b, nil
	}
	return nil, fmt.Errorf("the archive holds no %q file", BinaryName)
}

// Apply downloads the release's archive for this platform, verifies it against
// checksums.txt, and swaps it in for the running binary (the old one is kept as
// <name>.old). It changes nothing unless every check passes.
func (c *Client) Apply(ctx context.Context, rel *Release, current string, o Options) (*Result, error) {
	if o.GOOS == "" {
		o.GOOS = runtime.GOOS
	}
	if o.GOARCH == "" {
		o.GOARCH = runtime.GOARCH
	}
	if cmp, ok := Compare(rel.Tag, current); ok && cmp <= 0 && !o.Force {
		return nil, fmt.Errorf("%s is not newer than the installed %s (use --force to reinstall or downgrade)", rel.Tag, current)
	}
	exe := o.Exe
	if exe == "" {
		p, err := os.Executable()
		if err != nil {
			return nil, err
		}
		if exe, err = filepath.EvalSymlinks(p); err != nil {
			return nil, err
		}
	}
	if packageManaged(exe) && !o.Force {
		return nil, fmt.Errorf("%s looks package-managed: update it with your package manager (or pass --force)", exe)
	}
	if fi, err := os.Stat(filepath.Dir(exe)); err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("cannot write next to %s", exe)
	}

	name := ArchiveName(rel.Tag, o.GOOS, o.GOARCH)
	var archiveURL, sumsURL string
	for _, a := range rel.Assets {
		switch a.Name {
		case name:
			archiveURL = a.URL
		case "checksums.txt":
			sumsURL = a.URL
		}
	}
	if archiveURL == "" {
		return nil, fmt.Errorf("release %s has no build for %s/%s (%s)", rel.Tag, o.GOOS, o.GOARCH, name)
	}
	if sumsURL == "" {
		return nil, fmt.Errorf("release %s has no checksums.txt: refusing to install an unverifiable binary", rel.Tag)
	}
	sums, err := c.fetch(ctx, sumsURL, maxChecksums)
	if err != nil {
		return nil, err
	}
	want, ok := checksumFor(sums, name)
	if !ok {
		return nil, fmt.Errorf("checksums.txt does not list %s: refusing to install", name)
	}
	archive, err := c.fetch(ctx, archiveURL, maxArchive)
	if err != nil {
		return nil, err
	}
	got := sha256.Sum256(archive)
	if hex.EncodeToString(got[:]) != want {
		return nil, fmt.Errorf("the download's SHA-256 (%s) does not match checksums.txt (%s): NOT installed", hex.EncodeToString(got[:])[:16], want[:16])
	}
	bin, err := extractBinary(archive)
	if err != nil {
		return nil, err
	}

	tmp, err := os.CreateTemp(filepath.Dir(exe), "."+BinaryName+"-new-*")
	if err != nil {
		return nil, fmt.Errorf("cannot write next to %s: %w", exe, err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(bin); err != nil {
		tmp.Close()
		return nil, err
	}
	if err := tmp.Chmod(0o755); err != nil {
		tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	if o.GOOS == runtime.GOOS && o.GOARCH == runtime.GOARCH { // a wrong-platform or corrupt binary is caught before it replaces a good one
		vctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if out, err := exec.CommandContext(vctx, tmp.Name(), "version").CombinedOutput(); err != nil {
			return nil, fmt.Errorf("the new binary does not run (%v): NOT installed: %s", err, strings.TrimSpace(string(out)))
		}
	}

	old := exe + ".old"
	_ = os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		return nil, fmt.Errorf("could not keep the old binary: %w", err)
	}
	if err := os.Rename(tmp.Name(), exe); err != nil {
		_ = os.Rename(old, exe) // put the old one back
		return nil, fmt.Errorf("could not install the new binary (the old one was restored): %w", err)
	}
	return &Result{Version: rel.Tag, Path: exe, OldPath: old, Archive: name, SHA256: want, Verified: true}, nil
}
