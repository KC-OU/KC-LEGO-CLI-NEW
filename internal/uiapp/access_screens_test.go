package uiapp

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pquerna/otp/totp"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/auth"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/twofa"
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

// TestEditingAUserPreservesTheirBotLink is the same regression as the badge
// token above, for the remote-bot fields (internal/botapi): they're edited on
// accessBotLinkEditScreen, never this form, so a save here must not wipe them.
func TestEditingAUserPreservesTheirBotLink(t *testing.T) {
	app := adminApp(t)
	setPolicy(t, func(p *access.Policy) {
		p.Users["partdb:exportbot"] = &access.User{
			Groups: []string{"exporter"}, DiscordID: "111222333", BotPINHash: "hash",
			SecurityQuestion: "First pet?", SecurityAnswerHash: "hash2", BotPINFails: 2, BotPINLockUntil: "2030-01-01T00:00:00Z",
		}
	})
	app.loadPolicy()
	app.accessEdit = &accessEdit{User: "partdb:exportbot"}
	app.goTo(scrAccessUserEdit)
	app.screens[scrAccessUserEdit].(*formScreen).submit(app, []string{"exporter", "default", "", "all", "", "", "", "", "renamed note"})
	p, _ := access.Load()
	u := p.Users["partdb:exportbot"]
	if u == nil || u.DiscordID != "111222333" || u.BotPINHash != "hash" || u.SecurityQuestion != "First pet?" ||
		u.SecurityAnswerHash != "hash2" || u.BotPINFails != 2 || u.BotPINLockUntil != "2030-01-01T00:00:00Z" {
		t.Fatalf("saving the edit form must keep every bot-link field: %+v (%s)", u, app.message)
	}
}

// governAdmin gives adminApp's own signed-in account a real (fully-permissioned)
// group of its own, so it's "governed" before touching a bot-link screen —
// otherwise a bare Users[] entry would govern it with zero policy permissions,
// which those screens now deliberately refuse (see accessBotLinkEditScreen).
func governAdmin(t *testing.T, app *App) {
	t.Helper()
	key := access.Key(app.session.Source, app.session.Username)
	setPolicy(t, func(p *access.Policy) { p.Users[key] = &access.User{Groups: []string{"admin"}} })
	app.loadPolicy()
}

// TestAccessBotLinkEditSetsAndKeepsFieldsBlankMeansUnchanged covers the
// actual setup screen: a first save sets everything, a second save with
// blank PIN/answer fields must leave those hashes exactly as they were.
func TestAccessBotLinkEditSetsAndKeepsFieldsBlankMeansUnchanged(t *testing.T) {
	app := adminApp(t)
	governAdmin(t, app)
	app.goTo(scrAccessBotLinkEdit)
	app.screens[scrAccessBotLinkEdit].(*formScreen).submit(app, []string{"111222333", "", "1234", "1234", "First pet?", "Rex"})

	key := access.Key(app.session.Source, app.session.Username)
	u := app.pol().Users[key]
	if u == nil || u.DiscordID != "111222333" || u.BotPINHash == "" || u.SecurityQuestion != "First pet?" || u.SecurityAnswerHash == "" {
		t.Fatalf("first save should set everything: %+v", u)
	}
	firstPINHash, firstAnswerHash := u.BotPINHash, u.SecurityAnswerHash

	app.goTo(scrAccessBotLinkEdit)
	app.screens[scrAccessBotLinkEdit].(*formScreen).submit(app, []string{"999888777", "", "", "", "", ""})
	u = app.pol().Users[key]
	if u.DiscordID != "999888777" {
		t.Errorf("the Discord ID itself should still be editable, got %q", u.DiscordID)
	}
	if u.BotPINHash != firstPINHash || u.SecurityAnswerHash != firstAnswerHash {
		t.Error("blank PIN/answer fields on a later save must leave the existing hashes untouched")
	}
}

// TestAccessBotLinkEditRefusesForAnUngovernedAdmin locks in the guard added
// to prevent a real footgun: a bare Users[] entry with no Groups would govern
// this admin with zero policy permissions, instantly dropping every
// legacy-role fallback (app.can) for the rest of the session.
func TestAccessBotLinkEditRefusesForAnUngovernedAdmin(t *testing.T) {
	app := adminApp(t) // the default fixture admin is deliberately ungoverned (legacy IsAdmin)
	app.goTo(scrAccessBotLinkEdit)
	app.screens[scrAccessBotLinkEdit].(*formScreen).submit(app, []string{"111222333", "", "1234", "1234", "First pet?", "Rex"})
	if !app.messageErr {
		t.Fatal("an ungoverned admin's save must be refused, not silently govern them with zero permissions")
	}
	key := access.Key(app.session.Source, app.session.Username)
	if _, governed := app.pol().Users[key]; governed {
		t.Error("the refused save must not have created a Users[] entry at all")
	}
}

func TestAccessBotLinkEditRefusesMismatchedPINConfirmation(t *testing.T) {
	app := adminApp(t)
	governAdmin(t, app)
	app.goTo(scrAccessBotLinkEdit)
	app.screens[scrAccessBotLinkEdit].(*formScreen).submit(app, []string{"111222333", "", "1234", "9999", "", ""})
	if !app.messageErr {
		t.Error("mismatched PIN/confirm should be refused")
	}
	key := access.Key(app.session.Source, app.session.Username)
	if u := app.pol().Users[key]; u != nil && u.BotPINHash != "" {
		t.Error("a refused save must not set a PIN")
	}
}

// TestAccessBotResetNeedsBothFactors drives the reset screen directly and
// checks the "both required together" rule: a real 2FA code with the wrong
// answer fails, the right answer with a bogus code fails, and only both
// correct together actually changes the PIN.
func TestAccessBotResetNeedsBothFactors(t *testing.T) {
	app := adminApp(t)
	governAdmin(t, app)
	app.goTo(scrAccessBotLinkEdit)
	app.screens[scrAccessBotLinkEdit].(*formScreen).submit(app, []string{"111222333", "", "1234", "1234", "First pet?", "Rex"})
	key := access.Key(app.session.Source, app.session.Username)
	oldHash := app.pol().Users[key].BotPINHash

	secret, _, err := twofa.Enroll(app.session.Username, app.session.Source)
	if err != nil {
		t.Fatal(err)
	}
	firstCode, _ := totp.GenerateCode(secret, time.Now())
	if _, err := twofa.Confirm(app.session.Username, app.session.Source, firstCode); err != nil {
		t.Fatal(err)
	}

	// Each code must land in a later 30s step than the last one actually
	// consumed (twofa.Verify refuses a repeated/earlier step as a replay).
	validCode, _ := totp.GenerateCode(secret, time.Now().Add(30*time.Second))
	app.goTo(scrAccessBotReset)
	app.screens[scrAccessBotReset].(*formScreen).submit(app, []string{validCode, "wrong-answer", "5678", "5678"})
	if !app.messageErr {
		t.Error("a valid 2FA code with the wrong security answer must still be refused")
	}
	if app.pol().Users[key].BotPINHash != oldHash {
		t.Error("a refused reset must not change the PIN hash")
	}

	app.goTo(scrAccessBotReset)
	app.screens[scrAccessBotReset].(*formScreen).submit(app, []string{"000000", "Rex", "5678", "5678"})
	if !app.messageErr {
		t.Error("a bogus 2FA code must refuse the reset even with the right answer")
	}
	if app.pol().Users[key].BotPINHash != oldHash {
		t.Error("a refused reset must not change the PIN hash")
	}

	validCode2, _ := totp.GenerateCode(secret, time.Now().Add(30*time.Second))
	app.goTo(scrAccessBotReset)
	app.screens[scrAccessBotReset].(*formScreen).submit(app, []string{validCode2, "Rex", "5678", "5678"})
	if app.messageErr {
		t.Fatalf("both factors correct should succeed, got error: %s", app.message)
	}
	if newHash := app.pol().Users[key].BotPINHash; newHash == oldHash || !auth.VerifyPartDB("5678", newHash) {
		t.Error("the PIN should now be 5678")
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

// menuTabRow finds a tab's index in menuTabs by key, for tests that need to
// drive the cursor to a specific row.
func menuTabRow(t *testing.T, key string) int {
	t.Helper()
	for i, tab := range menuTabs {
		if tab.Key == key {
			return i
		}
	}
	t.Fatalf("no menu tab %q", key)
	return -1
}

func toggleMenuTab(t *testing.T, app *App, scr *menuTabsScreen, key string) {
	t.Helper()
	row := menuTabRow(t, key)
	for scr.row < row {
		scr.HandleKey(app, tea.KeyMsg{Type: tea.KeyDown})
	}
	for scr.row > row {
		scr.HandleKey(app, tea.KeyMsg{Type: tea.KeyUp})
	}
	scr.HandleKey(app, tea.KeyMsg{Type: tea.KeySpace})
}

// TestMenuTabsScreenTogglesOneTabAtATime covers the "b" hotkey on the Users
// list: each row grants/denies exactly its own permissions (see hub.go's
// hubOptions and access.go's screenPerm), independently of every other row —
// toggling LEGO Collection must never also touch Part-DB Hub. An explicit
// allow/deny each time, not "set vs. delete-to-inherit" — a group (checker,
// picker) can already grant several of these by default, so merely deleting
// an override would fall back to the group's own "allow" and fail to turn it
// off (see TestMenuTabsScreenWithdrawsOneTabForOnePerson for that exact
// case). This user has no group at all, so everything genuinely starts off.
func TestMenuTabsScreenTogglesOneTabAtATime(t *testing.T) {
	app := adminApp(t)
	setPolicy(t, func(p *access.Policy) {
		p.Users["partdb:restricted1"] = &access.User{}
	})

	userKeys(app, "partdb:restricted1", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	if app.cur != scrMenuTabsEdit {
		t.Fatalf("cur = %q, want the menu tabs screen", app.cur)
	}
	scr := app.screens[scrMenuTabsEdit].(*menuTabsScreen)
	for _, tab := range menuTabs {
		if scr.vals[tab.Key] {
			t.Fatalf("tab %q should start off for a user in no group", tab.Key)
		}
	}

	toggleMenuTab(t, app, scr, "lego")
	scr.HandleKey(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})

	p, _ := access.Load()
	u := p.Users["partdb:restricted1"]
	if u == nil {
		t.Fatal("expected a policy entry for the user")
	}
	for _, perm := range []string{"lego.view", "lego.search"} {
		if u.Perms[perm] != access.Allow {
			t.Errorf("Perms[%q] = %q, want allow", perm, u.Perms[perm])
		}
	}
	if u.Perms["partdb.view"] == access.Allow {
		t.Error("toggling LEGO Collection must not also grant Part-DB Hub")
	}
	for _, perm := range []string{"lego.edit", "stock.adjust", "orders.manage"} {
		if u.Perms[perm] != "" {
			t.Errorf("the menu tabs screen must never set %q, got %q", perm, u.Perms[perm])
		}
	}
}

// TestMenuTabsScreenCreatesAnEntryForAnUngovernedUser mirrors the existing
// "g" permission-grid save path (access_screens.go's gridScreen HandleKey,
// the 's' case): saving for a user with no Users[] entry yet creates a bare
// one, same precedent, not something new this screen invents.
func TestMenuTabsScreenCreatesAnEntryForAnUngovernedUser(t *testing.T) {
	app := adminApp(t)
	app.loadPolicy()
	if p := app.pol(); p.Users["partdb:freshuser"] != nil {
		t.Fatal("test setup: expected no existing entry")
	}

	userKeys(app, "partdb:freshuser", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	scr := app.screens[scrMenuTabsEdit].(*menuTabsScreen)
	toggleMenuTab(t, app, scr, "partdb")
	scr.HandleKey(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})

	p, _ := access.Load()
	u := p.Users["partdb:freshuser"]
	if u == nil || u.Perms["partdb.view"] != access.Allow {
		t.Fatalf("expected a new entry with Part-DB Hub granted, got %+v", u)
	}
}

// TestMenuTabsScreenRefusesToGrantAdmin: unlike every other row, Admin can
// only be turned off from this screen — real admin capability stays a
// deliberate decision via the permission grid (G) or Groups, never a single
// checkbox here.
func TestMenuTabsScreenRefusesToGrantAdmin(t *testing.T) {
	app := adminApp(t)
	setPolicy(t, func(p *access.Policy) {
		p.Users["partdb:restricted1"] = &access.User{}
	})
	userKeys(app, "partdb:restricted1", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	scr := app.screens[scrMenuTabsEdit].(*menuTabsScreen)
	toggleMenuTab(t, app, scr, "admin")
	if scr.vals["admin"] {
		t.Error("Admin must never be toggled on from this screen")
	}
	if !app.messageErr {
		t.Error("expected a refusal message")
	}
}

// TestMenuTabsScreenAdminReflectsAnyAdminPermAndCanBeTurnedOff is the direct
// fix for the reported bug: an "operator" group member carries
// containers.view (one of the four permissions that shows "9=Admin" —
// hub.go's anyModuleAllowed is an OR across them), even though nothing named
// them an admin. The Admin row must show as on (OR, not AND, across its
// perms) and the admin can turn it off, denying all four together.
func TestMenuTabsScreenAdminReflectsAnyAdminPermAndCanBeTurnedOff(t *testing.T) {
	app := adminApp(t)
	setPolicy(t, func(p *access.Policy) {
		p.Users["partdb:operator1"] = &access.User{Groups: []string{"operator"}}
	})
	app.loadPolicy()
	if !app.pol().Effective("partdb", "operator1").Can("containers.view") {
		t.Fatal("test setup: expected the operator group to carry containers.view")
	}

	userKeys(app, "partdb:operator1", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	scr := app.screens[scrMenuTabsEdit].(*menuTabsScreen)
	if !scr.vals["admin"] {
		t.Fatal("Admin should show as on: containers.view is one of its OR'd permissions")
	}

	toggleMenuTab(t, app, scr, "admin") // on -> off
	if scr.vals["admin"] {
		t.Fatal("Space should have turned Admin off")
	}
	scr.HandleKey(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})

	p, _ := access.Load()
	u := p.Users["partdb:operator1"]
	for _, perm := range []string{"users.view", "settings.view", "containers.view", "access.manage"} {
		if u.Perms[perm] != access.Deny {
			t.Errorf("Perms[%q] = %q, want deny", perm, u.Perms[perm])
		}
	}
	app.loadPolicy()
	if app.pol().Effective("partdb", "operator1").Can("containers.view") {
		t.Error("containers.view should now be denied")
	}
}

// TestMenuTabsScreenWithdrawsOneTabForOnePerson is the toggle's real-world
// use: an admin turning one specific tab back off for one specific checker,
// overriding the group default, without touching any other tab.
func TestMenuTabsScreenWithdrawsOneTabForOnePerson(t *testing.T) {
	app := adminApp(t)
	setPolicy(t, func(p *access.Policy) {
		p.Users["partdb:checker3"] = &access.User{Groups: []string{"checker"}, TwoFA: access.TwoFAExempt}
	})
	app.loadPolicy()
	if !menuTabs[menuTabRow(t, "lego")].granted(app.pol(), "partdb:checker3") {
		t.Fatal("expected LEGO Collection to start on, inherited from the checker group")
	}

	userKeys(app, "partdb:checker3", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	scr := app.screens[scrMenuTabsEdit].(*menuTabsScreen)
	toggleMenuTab(t, app, scr, "lego") // on -> off
	scr.HandleKey(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})

	signOn(app, "checker3", "")
	if app.moduleAllowed("lego") {
		t.Fatal("after the toggle, this one checker should no longer see LEGO Collection")
	}
	if !app.moduleAllowed("partdb") {
		t.Error("Part-DB Hub should be untouched — only the LEGO Collection row was toggled")
	}
	app.cur, app.stack = scrPickerHub, nil
	menu := app.screens[scrPickerHub].Body(app)
	if strings.Contains(menu, "LEGO Collection") {
		t.Errorf("the LEGO Collection entry must disappear once denied, got:\n%s", menu)
	}
	if !strings.Contains(menu, "Part-DB Hub") {
		t.Error("Part-DB Hub should still be listed")
	}
	app.goTo(scrLegoHub)
	if app.cur == scrLegoHub {
		t.Error("goTo must also deny it directly, not just hide the menu entry")
	}
}

// TestPickerHubShowsLegoAndPartDBForAPlainCheckerByDefault confirms the
// "checker" group already carries lego.view/lego.search/partdb.view by
// default (see access.go's seed()) — what was actually missing was a menu
// path to them (picker_hub.go now has one, Perm-gated like every other entry
// there), and an admin's way to override the default per person, which is
// what the menu tabs screen above is for.
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
