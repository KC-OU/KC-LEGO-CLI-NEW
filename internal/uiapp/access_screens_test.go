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

// TestBrowseToggleGrantsAndRevokesLegoAndPartDBViewOnly covers the "b" hotkey
// on the Users list: it should grant exactly the three read permissions that
// already gate LEGO Collection/Part-DB browsing (see hub.go's hubOptions and
// access.go's screenPerm), together, and nothing else; pressing it again
// takes it away again. An explicit allow/deny each time, not "set vs.
// delete-to-inherit" — a group (checker, picker) can already grant this by
// default (access.go's seed()), so merely deleting an override would fall
// back to the group's own "allow" and fail to turn it off (see
// TestBrowseToggleWithdrawsItForOnePerson for that exact case). This user
// has no group at all, so it genuinely starts off.
func TestBrowseToggleGrantsAndRevokesLegoAndPartDBViewOnly(t *testing.T) {
	app := adminApp(t)
	setPolicy(t, func(p *access.Policy) {
		p.Users["partdb:restricted1"] = &access.User{}
	})
	app.loadPolicy()

	if browseGranted(app.pol(), "partdb:restricted1") {
		t.Fatal("browse should start off for a user in no group")
	}

	userKeys(app, "partdb:restricted1", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})

	p, _ := access.Load()
	u := p.Users["partdb:restricted1"]
	if u == nil {
		t.Fatal("expected a policy entry for the user")
	}
	for _, perm := range []string{"lego.view", "lego.search", "partdb.view"} {
		if u.Perms[perm] != access.Allow {
			t.Errorf("Perms[%q] = %q, want allow", perm, u.Perms[perm])
		}
	}
	for _, perm := range []string{"lego.edit", "stock.adjust", "orders.manage"} {
		if u.Perms[perm] != "" {
			t.Errorf("the browse toggle must never set %q, got %q", perm, u.Perms[perm])
		}
	}
	app.loadPolicy()
	if !browseGranted(app.pol(), "partdb:restricted1") {
		t.Error("browseGranted should now report on")
	}

	userKeys(app, "partdb:restricted1", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	p, _ = access.Load()
	u = p.Users["partdb:restricted1"]
	for _, perm := range []string{"lego.view", "lego.search", "partdb.view"} {
		if u.Perms[perm] != access.Deny {
			t.Errorf("pressing b again should explicitly deny %q, got %q", perm, u.Perms[perm])
		}
	}
	app.loadPolicy()
	if browseGranted(app.pol(), "partdb:restricted1") {
		t.Error("browseGranted should now report off")
	}
}

// TestBrowseToggleCreatesAnEntryForAnUngovernedUser mirrors the existing "g"
// permission-grid save path (access_screens.go's gridScreen HandleKey, the
// 's' case): toggling browse for a user with no Users[] entry yet creates a
// bare one, same precedent, not something new this hotkey invents.
func TestBrowseToggleCreatesAnEntryForAnUngovernedUser(t *testing.T) {
	app := adminApp(t)
	app.loadPolicy()
	if p := app.pol(); p.Users["partdb:freshuser"] != nil {
		t.Fatal("test setup: expected no existing entry")
	}

	userKeys(app, "partdb:freshuser", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})

	p, _ := access.Load()
	u := p.Users["partdb:freshuser"]
	if u == nil || u.Perms["lego.view"] != access.Allow {
		t.Fatalf("expected a new entry with browse granted, got %+v", u)
	}
}

// TestBrowseToggleActuallyUnlocksLegoAndPartDBReadOnly is the end-to-end
// proof behind the design decision not to add any new access-control
// plumbing for the browse toggle. The "checker" group already carries
// lego.view/lego.search/partdb.view by default (see access.go's seed()) —
// what was actually missing was a menu path to them (picker_hub.go now has
// one, Perm-gated like every other entry there), and an admin's way to
// override the default per person, which is what the "b" toggle is for.
func TestPickerHubShowsLegoAndPartDBForAPlainCheckerByDefault(t *testing.T) {
	app := adminApp(t)
	setPolicy(t, func(p *access.Policy) {
		p.Users["partdb:checker3"] = &access.User{Groups: []string{"checker"}, TwoFA: access.TwoFAExempt}
	})
	signOn(app, "checker3", "")

	if !app.moduleAllowed("lego") || !app.moduleAllowed("partdb") {
		t.Fatal("the checker group already grants lego.view/partdb.view by default")
	}
	app.cur, app.stack = scrPickerHub, nil
	menu := app.screens[scrPickerHub].Body(app)
	if !strings.Contains(menu, "LEGO Collection") || !strings.Contains(menu, "Part-DB Hub") {
		t.Errorf("the picker/checker hub should list both by default, got:\n%s", menu)
	}

	app.goTo(scrLegoHub)
	if app.cur != scrLegoHub {
		t.Errorf("a plain checker should be able to open LEGO Collection, cur = %q (%s)", app.cur, app.message)
	}
	app.cur, app.stack = scrPickerHub, nil
	app.goTo(scrLegoSetAdd)
	if app.cur == scrLegoSetAdd {
		t.Error("browsing must never reach a write screen like Add a Set")
	}
}

// TestBrowseToggleWithdrawsItForOnePerson is the toggle's actual real-world
// use: an admin turning LEGO/Part-DB browsing back OFF for one specific
// checker, overriding the group default.
func TestBrowseToggleWithdrawsItForOnePerson(t *testing.T) {
	app := adminApp(t)
	setPolicy(t, func(p *access.Policy) {
		p.Users["partdb:checker3"] = &access.User{Groups: []string{"checker"}, TwoFA: access.TwoFAExempt}
	})
	app.loadPolicy()
	if !browseGranted(app.pol(), "partdb:checker3") {
		t.Fatal("expected browse to start on, inherited from the checker group")
	}

	userKeys(app, "partdb:checker3", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}}) // on -> off
	signOn(app, "checker3", "")

	if app.moduleAllowed("lego") || app.moduleAllowed("partdb") {
		t.Fatal("after the toggle, this one checker should no longer see LEGO/Part-DB")
	}
	app.cur, app.stack = scrPickerHub, nil
	menu := app.screens[scrPickerHub].Body(app)
	if strings.Contains(menu, "LEGO Collection") || strings.Contains(menu, "Part-DB Hub") {
		t.Errorf("the menu entries must disappear once denied, got:\n%s", menu)
	}
	app.goTo(scrLegoHub)
	if app.cur == scrLegoHub {
		t.Error("goTo must also deny it directly, not just hide the menu entry")
	}
}
