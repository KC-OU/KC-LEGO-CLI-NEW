// Package exports saves export files where the TUI, the CLI and the gateway all find
// them (WMS_EXPORT_DIR), and hands out single-use download links for them, so a file
// made in a telnet or web-terminal session can reach your phone or PC: the TUI shows
// the link as a QR code, and the web gateway serves it once at /dl/<token>.
//
// Security model: a token is 20 random bytes; only its SHA-256 is written to disk, so
// reading the link folder does not reveal a usable link. A link works once (it is
// claimed by an atomic rename), expires after LinkTTL, serves only a file inside the
// export folder, and every download is audited with the user who made it.
package exports

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

const linkDir = ".links"

// LinkTTL is how long a download link works and MaxAge how long an export is kept;
// admins set both (Admin → Access Control → Security settings, or `wms access settings`).
func LinkTTL() time.Duration { return time.Duration(policy().LinkMinutes()) * time.Minute }
func MaxAge() time.Duration  { return time.Duration(policy().ExportDays()) * 24 * time.Hour }

func policy() *access.Policy {
	p, err := access.Load()
	if err != nil {
		return &access.Policy{} // an unreadable policy means the defaults, not no cleanup
	}
	return p
}

// ErrNoLink is any reason a download link can't be used (unknown, used, expired,
// or pointing outside the export folder); callers answer 404 without saying which.
var ErrNoLink = errors.New("no such download")

// Dir is the export folder.
func Dir() string { return config.Get(config.ExportDir) }

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// Save writes data as <dir>/<user>-<kind>-<num>-<time>.<ext> (readable by the owner
// only) and returns its path.
func Save(dir, user, kind, num, ext string, data []byte) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	parts := []string{user, kind}
	if num != "" {
		parts = append(parts, num)
	}
	name := unsafeName.ReplaceAllString(strings.Join(parts, "-"), "_") + "-" + time.Now().Format("20060102-150405") + "." + ext
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

type link struct {
	File    string    `json:"file"`
	User    string    `json:"user"`
	Expires time.Time `json:"expires"`
}

func linkPath(dir, token string) string {
	sum := sha256.Sum256([]byte(token))
	return filepath.Join(dir, linkDir, hex.EncodeToString(sum[:])+".json")
}

// NewLink makes a single-use token for file (which must be in dir).
func NewLink(dir, file, user string) (string, error) {
	var raw [20]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	// Upper-case base32 (160 bits): a QR code holds it in its compact alphanumeric
	// mode, which keeps the code small enough for an 80x25 telnet window.
	token := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw[:])
	b, err := json.Marshal(link{File: filepath.Base(file), User: user, Expires: time.Now().Add(LinkTTL())})
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Join(dir, linkDir), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(linkPath(dir, token), b, 0o600); err != nil {
		return "", err
	}
	return token, nil
}

// URL is the download address for token, "" when WMS_PUBLIC_URL is not set. It is
// all upper case (scheme, host and path are case-insensitive here, and the token is
// upper case already), which is what lets a QR code use alphanumeric mode.
func URL(token string) string {
	base := strings.TrimRight(strings.TrimSpace(config.Get(config.PublicURL)), "/")
	if base == "" {
		return ""
	}
	return strings.ToUpper(base) + "/DL/" + token
}

var tokenRE = regexp.MustCompile(`^[A-Z2-7]{32}$`)

// Claim uses up token and returns the file it names and who made it. It can succeed
// only once per token: the record is renamed away before it is read, so two
// requests racing for the same link cannot both win.
func Claim(dir, token string) (file, user string, err error) {
	token = strings.ToUpper(token)
	if !tokenRE.MatchString(token) {
		return "", "", ErrNoLink
	}
	p := linkPath(dir, token)
	used := p + ".used"
	if os.Rename(p, used) != nil {
		return "", "", ErrNoLink
	}
	defer os.Remove(used)
	b, err := os.ReadFile(used)
	if err != nil {
		return "", "", ErrNoLink
	}
	var l link
	if json.Unmarshal(b, &l) != nil || time.Now().After(l.Expires) {
		return "", "", ErrNoLink
	}
	// Only a plain file name directly in the export folder, so a tampered record can't
	// point anywhere else.
	if l.File == "" || l.File != filepath.Base(l.File) || strings.HasPrefix(l.File, ".") {
		return "", "", ErrNoLink
	}
	full := filepath.Join(dir, l.File)
	if fi, err := os.Lstat(full); err != nil || !fi.Mode().IsRegular() {
		return "", "", ErrNoLink
	}
	return full, l.User, nil
}

// Cleanup deletes exports older than MaxAge and expired links; it returns how many
// files went.
func Cleanup(dir string) int {
	n := 0
	now := time.Now()
	maxAge, linkTTL := MaxAge(), LinkTTL()
	for _, sub := range []string{dir, filepath.Join(dir, linkDir)} {
		entries, err := os.ReadDir(sub)
		if err != nil {
			continue
		}
		for _, e := range entries {
			fi, err := e.Info()
			if err != nil || !fi.Mode().IsRegular() {
				continue
			}
			age := maxAge
			if sub != dir {
				age = linkTTL
			}
			if now.Sub(fi.ModTime()) > age && os.Remove(filepath.Join(sub, e.Name())) == nil {
				n++
			}
		}
	}
	return n
}

// Describe is a one-line summary for messages: the file name and its size.
func Describe(path string) string {
	fi, err := os.Stat(path)
	if err != nil {
		return filepath.Base(path)
	}
	return fmt.Sprintf("%s (%s)", filepath.Base(path), humanSize(fi.Size()))
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d bytes", n)
}
