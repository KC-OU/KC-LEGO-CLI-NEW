package uiapp

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
)

func adminApp(t *testing.T) *App {
	app := newTestApp(t)
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 25})
	app.cur, app.stack = scrHub, nil
	return app
}

func TestAdminEditsAGroupGrid(t *testing.T) {
	app := adminApp(t)
	press(app, "9")
	press(app, "4")
	if app.cur != scrAccessHub {
		t.Fatalf("Admin → 4 should open Access Control, on %q", app.cur)
	}
	press(app, "1")
	v := plain(app.View())
	if !strings.Contains(v, "stock-clerk") || !strings.Contains(v, "builder") {
		t.Fatalf("groups list:\n%s", v)
	}
	// builder is second alphabetically (admin, builder, …)
	app.Update(tea.KeyMsg{Type: tea.KeyDown})
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if app.cur != scrAccessGrid || app.accessEdit.Group != "builder" {
		t.Fatalf("grid for builder, on %q %+v", app.cur, app.accessEdit)
	}
	// lego row, move to "export" (5th action) and allow it
	for i := 0; i < 4; i++ {
		app.Update(tea.KeyMsg{Type: tea.KeyRight})
	}
	app.Update(tea.KeyMsg{Type: tea.KeySpace})
	press(app, "s")
	p, _ := access.Load()
	if p.Groups["builder"].Perms["lego.export"] != access.Allow {
		t.Fatalf("not saved: %v (%s)", p.Groups["builder"].Perms, app.message)
	}
	if !auditHas(t, "ACCESS_CHANGED") || !auditHas(t, "lego.export inherit→allow") {
		t.Error("the change and its diff should be audited")
	}
	// A restricted group cannot be given a privileged permission.
	app.accessEdit = &accessEdit{Group: "builder"}
	app.goTo(scrAccessGrid)
	g := app.screens[scrAccessGrid].(*gridScreen)
	g.row = indexOfArea("scripts")
	app.Update(tea.KeyMsg{Type: tea.KeySpace})
	press(app, "s")
	if !strings.Contains(app.message, "restricted") {
		t.Errorf("expected a refusal, got %q", app.message)
	}
}

func TestAdminSetsUserPolicyAndSettings(t *testing.T) {
	app := adminApp(t)
	app.goTo(scrAccessUserNew)
	app.screens[scrAccessUserNew].(*formScreen).submit(app, []string{"ExportBot", "partdb", "exporter"})
	if app.cur != scrAccessUserEdit {
		t.Fatalf("after adding, the editor opens; on %q (%s)", app.cur, app.message)
	}
	app.screens[scrAccessUserEdit].(*formScreen).submit(app, []string{"exporter", "exempt", "192.168.1.0/24", "telnet,web", "2027-01-01", "", "20", "", "home export account"})
	p, _ := access.Load()
	u := p.Users["partdb:exportbot"]
	if u == nil || u.TwoFA != access.TwoFAExempt || len(u.ExemptCIDRs) != 1 || u.IdleMin == nil || *u.IdleMin != 20 || u.Expires != "2027-01-01" {
		t.Fatalf("user = %+v (%s)", u, app.message)
	}
	app.goTo(scrAccessSettings)
	app.screens[scrAccessSettings].(*formScreen).submit(app, []string{"60", "10", "8", "14", "30", "yes", "Stock check Saturday"})
	p, _ = access.Load()
	if p.ExportDays() != 14 || p.LinkMinutes() != 30 || p.MaxSessionHours(nil) != 8 {
		t.Fatalf("settings = %+v (%s)", p.Settings, app.message)
	}
	app.goTo(scrAccessSettings)
	app.screens[scrAccessSettings].(*formScreen).submit(app, []string{"60", "10", "8", "0", "30", "yes", ""})
	if !strings.Contains(app.message, "export days") {
		t.Errorf("0 export days must be refused: %q", app.message)
	}
}

func TestAdminCannotLockThemselvesOut(t *testing.T) {
	app := adminApp(t)
	setPolicy(t, func(p *access.Policy) { p.Users["modernwms:admin"] = &access.User{Groups: []string{"admin"}} })
	app.loadPolicy()
	app.accessEdit = &accessEdit{User: "modernwms:admin"}
	app.goTo(scrAccessUserEdit)
	app.screens[scrAccessUserEdit].(*formScreen).submit(app, []string{"viewer", "default", "", "all", "", "", "", "", ""})
	if !strings.Contains(app.message, "lock you out") {
		t.Errorf("expected the self-lockout refusal, got %q", app.message)
	}
}

// Editing a user through the form rebuilds the User record from the fields it
// shows — which does not include the badge token (that's issued from `wms access
// user badge`, not typed into a form) — so a save must not silently wipe it.
func TestEditingAUserPreservesTheirBadgeToken(t *testing.T) {
	app := adminApp(t)
	setPolicy(t, func(p *access.Policy) {
		p.Users["partdb:exportbot"] = &access.User{Groups: []string{"exporter"}, BadgeToken: "BADGE123XYZ"}
	})
	app.loadPolicy()
	app.accessEdit = &accessEdit{User: "partdb:exportbot"}
	app.goTo(scrAccessUserEdit)
	app.screens[scrAccessUserEdit].(*formScreen).submit(app, []string{"exporter", "default", "", "all", "", "", "", "", "renamed note"})
	p, _ := access.Load()
	u := p.Users["partdb:exportbot"]
	if u == nil || u.BadgeToken != "BADGE123XYZ" {
		t.Fatalf("saving the edit form must keep the badge token: %+v (%s)", u, app.message)
	}
}

func TestAccessScreensFit(t *testing.T) {
	app := adminApp(t)
	for _, id := range []string{scrAccessHub, scrAccessGroups, scrAccessUsers, scrAccessSettings, scrAccessGrid} {
		app.accessEdit = &accessEdit{Group: "operator"}
		app.cur, app.stack = scrHub, nil
		app.goTo(id)
		v := plain(app.View())
		if rows := strings.Count(v, "\n") + 1; rows > 25 {
			t.Errorf("%s: %d rows:\n%s", id, rows, v)
		}
		for i, l := range strings.Split(v, "\n") {
			if w := len([]rune(l)); w > 80 {
				t.Errorf("%s: row %d is %d wide: %q", id, i, w, l)
			}
		}
	}
}

func indexOfArea(name string) int {
	for i, a := range access.Areas {
		if a.Name == name {
			return i
		}
	}
	return -1
}
