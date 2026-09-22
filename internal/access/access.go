// Package access is the TUI's permission system: groups with an allow/deny grid
// of area.action permissions (the model Part-DB uses), per-user overrides, a
// per-user 2FA policy (required, exempt within limits, or the default), and the
// session and export timings admins set. It sits on top of ModernWMS and Part-DB,
// which still check passwords.
//
// A user with no entry here is untouched: the caller keeps today's role-based
// checks for them (see uiapp's legacy path), so adopting the policy is gradual.
// Deny always beats allow; "inherit" (no value) falls through user → groups → not granted.
package access

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

// Areas and their actions, in display order. A permission is "area.action".
var Areas = []struct {
	Name, Label string
	Actions     []string
}{
	{"lego", "LEGO collection", []string{"view", "search", "edit", "delete", "export"}},
	{"partdb", "Part-DB parts", []string{"view", "edit", "delete"}},
	{"stock", "Stock", []string{"view", "check", "adjust"}},
	{"ops", "Warehouse ops", []string{"view", "asn", "edit"}},
	{"sets", "Set checks", []string{"check", "stocktake"}},
	{"orders", "Parts orders", []string{"view", "manage"}},
	{"labels", "Labels", []string{"print"}},
	{"exports", "Exports", []string{"create", "download"}},
	{"bricklink", "BrickLink", []string{"view", "price"}},
	{"scripts", "Script Hub", []string{"run"}},
	{"containers", "Containers", []string{"view", "manage"}},
	{"users", "Users", []string{"view", "manage"}},
	{"access", "Access control", []string{"manage"}},
	{"settings", "Settings", []string{"view", "edit"}},
	{"audit", "Audit log", []string{"view"}},
}

// Actions is every action name used by any area, in grid-column order.
var Actions = []string{"view", "search", "check", "stocktake", "create", "edit", "asn", "adjust", "price", "export", "download", "print", "delete", "run", "manage"}

// All lists every permission.
func All() []string {
	var out []string
	for _, a := range Areas {
		for _, act := range a.Actions {
			out = append(out, a.Name+"."+act)
		}
	}
	return out
}

// Valid reports whether p is a known permission.
func Valid(p string) bool { return slices.Contains(All(), p) }

// Privileged permissions may not be held by an account that signs in without 2FA.
var Privileged = []string{"users.manage", "access.manage", "settings.edit", "scripts.run", "containers.manage"}

const (
	Allow = "allow"
	Deny  = "deny"

	TwoFADefault  = ""
	TwoFARequired = "required"
	TwoFAExempt   = "exempt"

	ChannelLocal  = "local"
	ChannelTelnet = "telnet"
	ChannelWeb    = "web"
)

type Group struct {
	Description string            `json:"description,omitempty"`
	Perms       map[string]string `json:"perms"`
	// Restricted groups may be given to 2FA-exempt accounts; the policy refuses a
	// restricted group any privileged permission.
	Restricted bool `json:"restricted,omitempty"`
}

type User struct {
	Groups      []string          `json:"groups,omitempty"`
	Perms       map[string]string `json:"perms,omitempty"`
	TwoFA       string            `json:"twofa,omitempty"`        // "", required, exempt
	ExemptCIDRs []string          `json:"exempt_cidrs,omitempty"` // exempt only from these networks (none = anywhere)
	Channels    []string          `json:"channels,omitempty"`     // allowed ways in; none = all
	Expires     string            `json:"expires,omitempty"`      // YYYY-MM-DD: access ends at the start of this day
	GraceMin    *int              `json:"grace_minutes,omitempty"`
	IdleMin     *int              `json:"idle_minutes,omitempty"`
	MaxHours    *int              `json:"max_session_hours,omitempty"`
	Note        string            `json:"note,omitempty"`
}

// Settings are the global timings; nil means "use the config key" (the env/settings value).
type Settings struct {
	GraceMin    *int `json:"grace_minutes,omitempty"`
	IdleMin     *int `json:"idle_minutes,omitempty"`
	MaxHours    *int `json:"max_session_hours,omitempty"`
	ExportDays  *int `json:"export_days,omitempty"`
	LinkMinutes *int `json:"link_minutes,omitempty"`
	// NotifyRoutes sends each alert event to notify channel ids (none = every channel).
	NotifyRoutes map[string][]string `json:"notify_routes,omitempty"`
}

type Policy struct {
	Groups   map[string]*Group `json:"groups"`
	Users    map[string]*User  `json:"users"`
	Settings Settings          `json:"settings"`
	Seeded   bool              `json:"seeded"` // the starter groups were written once; deleting one keeps it deleted
}

// Key is how a user is stored: "source:username" (lower case), like the 2FA store.
func Key(source, username string) string { return source + ":" + strings.ToLower(username) }

// FindKey resolves "alex" or "partdb:alex" to a stored key, preferring an exact match.
func (p *Policy) FindKey(name string) (string, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if _, ok := p.Users[name]; ok {
		return name, true
	}
	var hits []string
	for k := range p.Users {
		if _, u, _ := strings.Cut(k, ":"); u == name {
			hits = append(hits, k)
		}
	}
	if len(hits) == 1 {
		return hits[0], true
	}
	return "", false
}

// ---- storage ----

var mu sync.Mutex

func path() string { return config.Get(config.AccessFile) }

// Load reads the policy, seeding the starter groups the first time. A missing file
// is an empty policy (everyone on the legacy rules), never an error.
func Load() (*Policy, error) {
	p := &Policy{}
	b, err := os.ReadFile(path())
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		if err := json.Unmarshal(b, p); err != nil {
			return nil, fmt.Errorf("reading %s: %w", path(), err)
		}
	}
	if p.Groups == nil {
		p.Groups = map[string]*Group{}
	}
	if p.Users == nil {
		p.Users = map[string]*User{}
	}
	seed(p)
	return p, nil
}

// Update loads, changes and saves the policy under a file lock (every telnet session
// is its own process), refusing a change that fails Validate.
func Update(change func(p *Policy) error) (*Policy, error) {
	unlock, err := lock()
	if err != nil {
		return nil, err
	}
	defer unlock()
	p, err := Load()
	if err != nil {
		return nil, err
	}
	if err := change(p); err != nil {
		return nil, err
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return p, save(p)
}

func lock() (func(), error) {
	mu.Lock()
	if err := os.MkdirAll(filepath.Dir(path()), 0o700); err != nil {
		mu.Unlock()
		return nil, err
	}
	f, err := os.OpenFile(path()+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		mu.Unlock()
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		mu.Unlock()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
		mu.Unlock()
	}, nil
}

func save(p *Policy) error {
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path()), ".access-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path())
}

// ---- starter groups ----

func grant(perms ...string) map[string]string {
	m := map[string]string{}
	for _, p := range perms {
		m[p] = Allow
	}
	return m
}

// seed adds the starter groups the first time; after that, groups you edit or
// delete stay as you left them.
func seed(p *Policy) {
	if p.Seeded {
		return
	}
	p.Seeded = true
	starters := map[string]*Group{
		"admin": {Description: "everything", Perms: grant(All()...)},
		"operator": {Description: "day-to-day stock, parts and LEGO work", Perms: grant(
			"lego.view", "lego.search", "lego.edit", "lego.export", "sets.check", "sets.stocktake", "orders.view", "orders.manage", "labels.print", "partdb.view", "partdb.edit", "stock.view", "stock.check", "stock.adjust",
			"ops.view", "ops.asn", "ops.edit", "exports.create", "exports.download", "bricklink.view", "bricklink.price", "containers.view", "audit.view")},
		"viewer": {Description: "read-only everywhere", Restricted: true, Perms: grant(
			"lego.view", "lego.search", "partdb.view", "stock.view", "ops.view", "bricklink.view", "containers.view")},
		"builder": {Description: "LEGO only: browse the collection, missing parts, what to build", Restricted: true, Perms: grant(
			"lego.view", "lego.search")},
		"exporter": {Description: "LEGO view plus exports and downloads", Restricted: true, Perms: grant(
			"lego.view", "lego.search", "lego.export", "exports.create", "exports.download")},
		"stock-clerk": {Description: "stock checks, Part-DB view, LEGO part/set search and export", Restricted: true, Perms: grant(
			"lego.view", "lego.search", "lego.export", "exports.create", "exports.download", "partdb.view", "stock.view", "stock.check",
			"sets.stocktake", "orders.view", "labels.print")},
	}
	for name, g := range starters {
		if _, ok := p.Groups[name]; !ok {
			p.Groups[name] = g
		}
	}
}

// ---- evaluation ----

// Perms is a user's effective permissions and why.
type Perms struct {
	values map[string]string // allow or deny
	why    map[string]string
}

// Can reports whether p is allowed.
func (e Perms) Can(p string) bool { return e.values[p] == Allow }

// Why explains the value of p: "allowed by group exporter", "denied by user override", "not granted".
func (e Perms) Why(p string) string {
	if w, ok := e.why[p]; ok {
		return w
	}
	return "not granted"
}

// MayDownload reports whether the user stored as key ("source:username") may use an
// export download link: users without a policy entry keep today's rule (yes).
func (p *Policy) MayDownload(key string) bool {
	u, ok := p.Users[strings.ToLower(key)]
	return !ok || p.effectiveFor(u).Can("exports.download")
}

// Has reports whether the user has an entry (and so is governed by the policy).
func (p *Policy) Has(source, username string) bool {
	_, ok := p.Users[Key(source, username)]
	return ok
}

// Effective works out a user's permissions: a user override wins; otherwise any
// group's deny beats any group's allow.
func (p *Policy) Effective(source, username string) Perms {
	return p.effectiveFor(p.Users[Key(source, username)])
}

func (p *Policy) effectiveFor(u *User) Perms {
	e := Perms{values: map[string]string{}, why: map[string]string{}}
	if u == nil {
		return e
	}
	groups := slices.Clone(u.Groups)
	sort.Strings(groups)
	for _, perm := range All() {
		if v := u.Perms[perm]; v == Allow || v == Deny {
			e.values[perm], e.why[perm] = v, map[string]string{Allow: "allowed", Deny: "denied"}[v]+" by user override"
			continue
		}
		for _, gn := range groups {
			g := p.Groups[gn]
			if g == nil {
				continue
			}
			switch g.Perms[perm] {
			case Deny:
				e.values[perm], e.why[perm] = Deny, "denied by group "+gn
			case Allow:
				if e.values[perm] != Deny {
					e.values[perm], e.why[perm] = Allow, "allowed by group "+gn
				}
			}
		}
	}
	return e
}

// Validate refuses a policy that would let an account without 2FA hold privileged
// permissions, or that names unknown groups or permissions.
func (p *Policy) Validate() error {
	for name, g := range p.Groups {
		for perm, v := range g.Perms {
			if !Valid(perm) || (v != Allow && v != Deny) {
				return fmt.Errorf("group %s: %q=%q is not a permission and allow/deny", name, perm, v)
			}
			if g.Restricted && v == Allow && slices.Contains(Privileged, perm) {
				return fmt.Errorf("group %s is restricted (usable without 2FA), so it cannot allow %s", name, perm)
			}
		}
	}
	for key, u := range p.Users {
		for _, gn := range u.Groups {
			if p.Groups[gn] == nil {
				return fmt.Errorf("user %s: no group named %q", key, gn)
			}
		}
		for perm, v := range u.Perms {
			if !Valid(perm) || (v != Allow && v != Deny) {
				return fmt.Errorf("user %s: %q=%q is not a permission and allow/deny", key, perm, v)
			}
		}
		for _, c := range u.ExemptCIDRs {
			if _, _, err := net.ParseCIDR(c); err != nil {
				return fmt.Errorf("user %s: %q is not a network like 192.168.1.0/24", key, c)
			}
		}
		for _, c := range u.Channels {
			if c != ChannelLocal && c != ChannelTelnet && c != ChannelWeb {
				return fmt.Errorf("user %s: channel %q must be local, telnet or web", key, c)
			}
		}
		if u.Expires != "" {
			if _, err := time.Parse("2006-01-02", u.Expires); err != nil {
				return fmt.Errorf("user %s: expiry %q must be YYYY-MM-DD", key, u.Expires)
			}
		}
		if u.TwoFA != TwoFADefault && u.TwoFA != TwoFARequired && u.TwoFA != TwoFAExempt {
			return fmt.Errorf("user %s: 2FA must be required, exempt or default", key)
		}
		if u.TwoFA == TwoFAExempt {
			for _, gn := range u.Groups {
				if !p.Groups[gn].Restricted {
					return fmt.Errorf("user %s is 2FA-exempt, so every group must be restricted; %s is not", key, gn)
				}
			}
			eff := p.effectiveFor(u)
			for _, perm := range Privileged {
				if eff.Can(perm) {
					return fmt.Errorf("user %s is 2FA-exempt and cannot hold %s", key, perm)
				}
			}
		}
	}
	return p.Settings.validate()
}

func (s Settings) validate() error {
	check := func(name string, v *int, lo, hi int) error {
		if v != nil && (*v < lo || *v > hi) {
			return fmt.Errorf("%s must be between %d and %d", name, lo, hi)
		}
		return nil
	}
	return errors.Join(
		check("2FA remember minutes", s.GraceMin, 0, 24*60),
		check("idle minutes", s.IdleMin, 0, 24*60),
		check("max session hours", s.MaxHours, 0, 24*7),
		check("export days", s.ExportDays, 1, 365),
		check("link minutes", s.LinkMinutes, 1, 24*60),
	)
}

// ---- sign-on ----

// Expired reports whether the user's access has ended.
func (u *User) Expired(now time.Time) bool {
	if u == nil || u.Expires == "" {
		return false
	}
	end, err := time.ParseInLocation("2006-01-02", u.Expires, now.Location())
	return err == nil && !now.Before(end)
}

// ChannelAllowed reports whether the user may come in this way.
func (u *User) ChannelAllowed(channel string) bool {
	return u == nil || len(u.Channels) == 0 || slices.Contains(u.Channels, channel)
}

// ExemptFrom reports whether the user may skip 2FA from ip ("" = unknown, which
// never matches a network limit), and why.
func (u *User) ExemptFrom(ip string) (bool, string) {
	if u == nil || u.TwoFA != TwoFAExempt {
		return false, ""
	}
	if len(u.ExemptCIDRs) == 0 {
		return true, "2FA-exempt account"
	}
	addr := net.ParseIP(ip)
	if addr == nil {
		return false, "2FA-exempt only from " + strings.Join(u.ExemptCIDRs, ", ") + "; this session's address is unknown"
	}
	for _, c := range u.ExemptCIDRs {
		if _, n, err := net.ParseCIDR(c); err == nil && n.Contains(addr) {
			return true, "2FA-exempt from " + c
		}
	}
	return false, "2FA-exempt only from " + strings.Join(u.ExemptCIDRs, ", ")
}

// ---- timings ----

// Minutes is a timing in effect for a user: their override, the policy's global
// value, or fallback (the config key's value).
func pick(user, global *int, fallback int) int {
	switch {
	case user != nil:
		return *user
	case global != nil:
		return *global
	}
	return fallback
}

func (p *Policy) GraceMinutes(u *User, fallback int) int {
	return pick(fieldOf(u, func(u *User) *int { return u.GraceMin }), p.Settings.GraceMin, fallback)
}
func (p *Policy) IdleMinutes(u *User, fallback int) int {
	return pick(fieldOf(u, func(u *User) *int { return u.IdleMin }), p.Settings.IdleMin, fallback)
}
func (p *Policy) MaxSessionHours(u *User) int {
	return pick(fieldOf(u, func(u *User) *int { return u.MaxHours }), p.Settings.MaxHours, 0)
}

func fieldOf(u *User, f func(*User) *int) *int {
	if u == nil {
		return nil
	}
	return f(u)
}

// ExportDays and LinkMinutes are the export folder's retention and a download
// link's lifetime.
func (p *Policy) ExportDays() int  { return pick(nil, p.Settings.ExportDays, 7) }
func (p *Policy) LinkMinutes() int { return pick(nil, p.Settings.LinkMinutes, 15) }

// Int is a helper for building the *int fields.
func Int(n int) *int { return &n }
