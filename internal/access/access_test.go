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
	if len(p.Groups) != 6 {
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
