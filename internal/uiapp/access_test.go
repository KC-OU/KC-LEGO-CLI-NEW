package uiapp

import (
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/auth"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/wmsdb"
)

func setPolicy(t *testing.T, f func(p *access.Policy)) {
	t.Helper()
	if _, err := access.Update(func(p *access.Policy) error { f(p); return nil }); err != nil {
		t.Fatal(err)
	}
}

// signOn runs the post-password sign-on for user as a telnet session from ip.
func signOn(app *App, user, ip string) {
	app.resetSession()
	app.requireTwoFA, app.transport, app.remoteAddr = true, "telnet", ip
	app.session = &auth.Session{Source: "partdb", Username: user, Role: "PartDB User", Permissions: &wmsdb.Permissions{CanWrite: true, Menus: []string{"*"}}}
	_ = markTourSeen(user) // this suite tests access control, not onboarding
	continueSignOn(app, app.session)
}

func auditHas(t *testing.T, s string) bool {
	t.Helper()
	b, _ := os.ReadFile(config.Get(config.AuditLogFile))
	return strings.Contains(string(b), s)
}

func TestExemptAccountSkips2FAOnlyFromItsNetwork(t *testing.T) {
	app := newTestApp(t)
	setPolicy(t, func(p *access.Policy) {
		p.Users["partdb:bot"] = &access.User{Groups: []string{"exporter"}, TwoFA: access.TwoFAExempt, ExemptCIDRs: []string{"192.168.1.0/24"}}
	})
	signOn(app, "bot", "192.168.1.40")
	if !app.authed || app.cur != scrHub {
		t.Fatalf("exempt from the LAN should sign straight in: cur=%q msg=%q", app.cur, app.message)
	}
	if !auditHas(t, "LOGIN_2FA_EXEMPT") {
		t.Error("the exemption should be audited")
	}
	signOn(app, "bot", "203.0.113.9")
	if app.authed {
		t.Fatal("outside the network 2FA is needed (none enrolled, so refused)")
	}
}

func TestExpiredAndChannelLimitedAccounts(t *testing.T) {
	app := newTestApp(t)
	setPolicy(t, func(p *access.Policy) {
		p.Users["partdb:old"] = &access.User{Groups: []string{"builder"}, TwoFA: access.TwoFAExempt, Expires: "2020-01-01"}
		p.Users["partdb:local"] = &access.User{Groups: []string{"builder"}, TwoFA: access.TwoFAExempt, Channels: []string{access.ChannelLocal}}
	})
	signOn(app, "old", "192.168.1.2")
	if app.authed || !strings.Contains(app.message, "ended") {
		t.Errorf("expired: authed=%v msg=%q", app.authed, app.message)
	}
	signOn(app, "local", "192.168.1.2")
	if app.authed || !strings.Contains(app.message, "telnet") {
		t.Errorf("channel: authed=%v msg=%q", app.authed, app.message)
	}
}

func TestBuilderSeesOnlyWhatTheGroupAllows(t *testing.T) {
	app := newTestApp(t)
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 25})
	setPolicy(t, func(p *access.Policy) {
		p.Users["partdb:kid"] = &access.User{Groups: []string{"builder"}, TwoFA: access.TwoFAExempt}
	})
	signOn(app, "kid", "")
	if !app.authed {
		t.Fatalf("not signed in: %q", app.message)
	}
	hub := plain(app.View())
	if strings.Contains(hub, "PartDB Hub") || strings.Contains(hub, "Admin") || !strings.Contains(hub, "LEGO Collection") {
		t.Errorf("builder hub:\n%s", hub)
	}
	app.goTo(scrLegoHub)
	lego := plain(app.View())
	if strings.Contains(lego, "Add / Update") || strings.Contains(lego, "BrickLink") || !strings.Contains(lego, "Search Parts") {
		t.Errorf("builder LEGO menu:\n%s", lego)
	}
	press(app, "4") // hidden: refused and audited
	if app.cur != scrLegoHub || !strings.Contains(app.message, "lego.edit") || !auditHas(t, "DENIED_PERMISSION") {
		t.Errorf("hidden key: cur=%q msg=%q", app.cur, app.message)
	}
	app.goTo(scrLegoPartOwned)
	press(app, "x")
	if app.cur == scrExport || !strings.Contains(app.message, "lego.export") {
		t.Errorf("builder may not export: cur=%q msg=%q", app.cur, app.message)
	}
	app.cur, app.stack = scrHub, nil
	app.goTo(scrSettingsHub)
	if app.cur == scrSettingsHub {
		t.Error("goTo must refuse screens the policy does not allow")
	}
}

func TestPerUserTimingsAndMaxSession(t *testing.T) {
	app := newTestApp(t)
	setPolicy(t, func(p *access.Policy) {
		p.Settings.GraceMin = access.Int(90)
		p.Users["partdb:shift"] = &access.User{Groups: []string{"operator"}, IdleMin: access.Int(5), MaxHours: access.Int(8)}
	})
	app.loadPolicy()
	if app.graceWindow() != 90*time.Minute {
		t.Errorf("global grace = %v", app.graceWindow())
	}
	app.session = &auth.Session{Source: "partdb", Username: "shift"}
	app.loadPolicy()
	if app.idleLimit() != 5*time.Minute || app.maxSession() != 8*time.Hour {
		t.Errorf("user overrides: idle %v max %v", app.idleLimit(), app.maxSession())
	}
	now := time.Now()
	app.now = func() time.Time { return now }
	app.enterHub()
	now = now.Add(8*time.Hour + time.Minute)
	app.lastInput = now // active, so not the idle lock
	app.checkIdle()
	if app.authed || !auditHas(t, "SESSION_MAX_AGE") {
		t.Errorf("the session should end after 8 hours: authed=%v", app.authed)
	}
}
