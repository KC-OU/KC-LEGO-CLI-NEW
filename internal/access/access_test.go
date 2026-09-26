package access

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

func tmpPolicy(t *testing.T) {
	t.Helper()
	t.Setenv(config.AccessFile, filepath.Join(t.TempDir(), "access.json"))
}

func TestSeedAndEffective(t *testing.T) {
	tmpPolicy(t)
	p, err := Update(func(p *Policy) error {
		p.Users[Key("partdb", "Clerk")] = &User{Groups: []string{"stock-clerk"}}
		p.Users[Key("partdb", "mixed")] = &User{Groups: []string{"operator", "viewer"}, Perms: map[string]string{"stock.adjust": Deny, "audit.view": Allow}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Groups) != 7 {
		t.Fatalf("groups = %d", len(p.Groups))
	}
	c := p.Effective("partdb", "clerk")
	for _, want := range []string{"lego.search", "lego.export", "exports.download", "stock.check", "partdb.view"} {
		if !c.Can(want) {
			t.Errorf("stock-clerk should have %s", want)
		}
	}
	if c.Can("lego.edit") || c.Can("stock.adjust") || c.Why("lego.edit") != "not granted" {
		t.Errorf("stock-clerk has too much: %s", c.Why("lego.edit"))
	}
	m := p.Effective("partdb", "mixed")
	if m.Can("stock.adjust") || m.Why("stock.adjust") != "denied by user override" {
		t.Errorf("user deny must win: %s", m.Why("stock.adjust"))
	}
	if !m.Can("lego.edit") || !strings.Contains(m.Why("lego.edit"), "operator") {
		t.Errorf("operator allow: %s", m.Why("lego.edit"))
	}
	// Seeding never overwrites an edited group.
	if _, err := Update(func(p *Policy) error { p.Groups["builder"].Perms = grant("lego.view"); return nil }); err != nil {
		t.Fatal(err)
	}
	p, _ = Load()
	if len(p.Groups["builder"].Perms) != 1 {
		t.Error("an edited starter group was reseeded")
	}
	if _, err := Update(func(p *Policy) error { delete(p.Groups, "exporter"); return nil }); err != nil {
		t.Fatal(err)
	}
	if p, _ = Load(); p.Groups["exporter"] != nil {
		t.Error("a deleted starter group came back")
	}
}

func TestGroupDenyBeatsGroupAllow(t *testing.T) {
	tmpPolicy(t)
	p, err := Update(func(p *Policy) error {
		p.Groups["no-export"] = &Group{Perms: map[string]string{"lego.export": Deny}}
		p.Users["partdb:x"] = &User{Groups: []string{"exporter", "no-export"}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if e := p.Effective("partdb", "x"); e.Can("lego.export") || e.Why("lego.export") != "denied by group no-export" {
		t.Errorf("deny should win: %s", e.Why("lego.export"))
	}
}

func TestValidateRefusesPrivilegedWithout2FA(t *testing.T) {
	tmpPolicy(t)
	cases := map[string]func(p *Policy){
		"exempt admin": func(p *Policy) { p.Users["partdb:a"] = &User{Groups: []string{"admin"}, TwoFA: TwoFAExempt} },
		"exempt override": func(p *Policy) {
			p.Users["partdb:a"] = &User{Groups: []string{"builder"}, TwoFA: TwoFAExempt, Perms: map[string]string{"users.manage": Allow}}
		},
		"restricted privileged": func(p *Policy) { p.Groups["builder"].Perms["scripts.run"] = Allow },
		"bad cidr":              func(p *Policy) { p.Users["partdb:a"] = &User{ExemptCIDRs: []string{"lan"}} },
		"bad perm":              func(p *Policy) { p.Users["partdb:a"] = &User{Perms: map[string]string{"lego.fly": Allow}} },
		"unknown group":         func(p *Policy) { p.Users["partdb:a"] = &User{Groups: []string{"nope"}} },
		"bad link minutes":      func(p *Policy) { p.Settings.LinkMinutes = Int(0) },
	}
	for name, f := range cases {
		if _, err := Update(func(p *Policy) error { f(p); return nil }); err == nil {
			t.Errorf("%s: should be refused", name)
		}
	}
	if _, err := Update(func(p *Policy) error {
		p.Users["partdb:bot"] = &User{Groups: []string{"exporter"}, TwoFA: TwoFAExempt, ExemptCIDRs: []string{"192.168.1.0/24"}}
		return nil
	}); err != nil {
		t.Errorf("an exempt exporter is fine: %v", err)
	}
}

func TestCheckerGroupIsSafeFor2FAExempt(t *testing.T) {
	tmpPolicy(t)
	if _, err := Update(func(p *Policy) error {
		p.Users["partdb:checker"] = &User{Groups: []string{"checker"}, TwoFA: TwoFAExempt}
		return nil
	}); err != nil {
		t.Fatalf("an exempt checker should be accepted: %v", err)
	}
	p, _ := Load()
	c := p.Effective("partdb", "checker")
	for _, want := range []string{"sets.check", "orders.view", "orders.manage", "exports.create", "exports.download", "stock.check", "stock.adjust", "partdb.view", "lego.view", "lego.export"} {
		if !c.Can(want) {
			t.Errorf("checker should have %s", want)
		}
	}
	for _, priv := range Privileged {
		if c.Can(priv) {
			t.Errorf("checker must not hold the privileged permission %s", priv)
		}
	}
}

// TestANewStarterGroupReachesAnAlreadySeededFile reproduces exactly what an
// install seeded before "checker" existed in the code was stuck with: seed()
// used to return immediately once Seeded was true, so a group added to the
// starters map later never showed up there at all, however many times the
// policy was reloaded. It must now arrive once, without resurrecting a starter
// that install had deliberately deleted.
func TestANewStarterGroupReachesAnAlreadySeededFile(t *testing.T) {
	tmpPolicy(t)
	// A file exactly like one seeded by the old code, before SeededGroups
	// existed and before "checker" was a starter: bare Seeded=true, the
	// original six groups, "exporter" deliberately deleted.
	legacy := &Policy{Seeded: true, Groups: map[string]*Group{}, Users: map[string]*User{}}
	for _, n := range originalStarterGroups {
		if n != "exporter" {
			legacy.Groups[n] = &Group{Description: "old"}
		}
	}
	if err := save(legacy); err != nil {
		t.Fatal(err)
	}
	p, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.Groups["checker"]; !ok {
		t.Error("a starter group added to the code since must still reach an already-seeded file")
	}
	if _, ok := p.Groups["exporter"]; ok {
		t.Error("a starter you deliberately deleted must not come back just because a new one was added")
	}
	if g := p.Groups["admin"]; g == nil || g.Description != "old" {
		t.Errorf("an existing starter must be left exactly as it was, not reset: %+v", g)
	}
	// Reloading again must not add "exporter" back either, or duplicate anything.
	p2, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p2.Groups["exporter"]; ok {
		t.Error("exporter must still be gone after a second load")
	}
}

func TestSignOnRules(t *testing.T) {
	u := &User{TwoFA: TwoFAExempt, ExemptCIDRs: []string{"192.168.1.0/24"}, Channels: []string{ChannelTelnet}, Expires: "2026-10-01"}
	if ok, _ := u.ExemptFrom("192.168.1.20"); !ok {
		t.Error("inside the network")
	}
	for _, ip := range []string{"10.0.0.1", ""} {
		if ok, why := u.ExemptFrom(ip); ok || why == "" {
			t.Errorf("%q should need 2FA (%s)", ip, why)
		}
	}
	if !u.ChannelAllowed(ChannelTelnet) || u.ChannelAllowed(ChannelWeb) {
		t.Error("channels")
	}
	day := func(s string) time.Time { d, _ := time.ParseInLocation("2006-01-02 15:04", s, time.Local); return d }
	if u.Expired(day("2026-09-30 23:59")) || !u.Expired(day("2026-10-01 00:00")) {
		t.Error("expiry")
	}
	var none *User
	if none.Expired(time.Now()) || !none.ChannelAllowed(ChannelWeb) {
		t.Error("no entry = no limits")
	}
}

func TestTimings(t *testing.T) {
	p := &Policy{Settings: Settings{GraceMin: Int(60)}}
	if p.GraceMinutes(nil, 30) != 60 || p.GraceMinutes(&User{GraceMin: Int(5)}, 30) != 5 || p.IdleMinutes(nil, 15) != 15 {
		t.Error("user > global > config")
	}
	if p.ExportDays() != 7 || p.LinkMinutes() != 15 {
		t.Error("export defaults")
	}
}

func TestConcurrentUpdates(t *testing.T) {
	tmpPolicy(t)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _ = Update(func(p *Policy) error { p.Users[Key("partdb", string(rune('a'+i)))] = &User{}; return nil })
		}(i)
	}
	wg.Wait()
	p, _ := Load()
	if len(p.Users) != 20 {
		t.Errorf("lost updates: %d users", len(p.Users))
	}
}

func TestBadgeTokenResolvesToTheRealUsernameNotTheKey(t *testing.T) {
	tok1, err := NewBadgeToken()
	if err != nil {
		t.Fatal(err)
	}
	tok2, _ := NewBadgeToken()
	if tok1 == "" || tok1 == tok2 {
		t.Fatalf("tokens must be non-empty and unique: %q %q", tok1, tok2)
	}
	if strings.Contains(strings.ToLower(tok1), "alex") {
		t.Error("a badge token must not embed the username")
	}
	p := &Policy{Users: map[string]*User{
		Key("partdb", "alex"):   {BadgeToken: tok1},
		Key("modernwms", "sam"): {BadgeToken: tok2},
	}}
	if name, ok := p.FindByBadge(tok1); !ok || name != "alex" {
		t.Errorf("FindByBadge(tok1) = %q, %v", name, ok)
	}
	if name, ok := p.FindByBadge(tok2); !ok || name != "sam" {
		t.Errorf("FindByBadge(tok2) = %q, %v", name, ok)
	}
	if _, ok := p.FindByBadge("not-a-real-token"); ok {
		t.Error("an unknown token must not resolve to anyone")
	}
	if _, ok := p.FindByBadge(""); ok {
		t.Error("an empty token must not resolve to anyone")
	}
	if _, ok := p.FindByBadge("alex"); ok {
		t.Error("the plain username itself must not work as a badge")
	}
}
