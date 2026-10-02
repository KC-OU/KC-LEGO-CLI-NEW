package uiapp

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
)

func TestHubOptionsUsesDefaultWhenMenuLayoutsUnset(t *testing.T) {
	app := newTestApp(t)
	opts := hubOptions(app)
	if len(opts) != len(defaultMainMenuKeys) {
		t.Fatalf("hubOptions = %d entries, want %d (the default set)", len(opts), len(defaultMainMenuKeys))
	}
	for i, key := range defaultMainMenuKeys {
		it := catalogItemByKey(mainMenuCatalog, key)
		if opts[i].Key != it.hotkey || opts[i].Label != it.label {
			t.Errorf("opts[%d] = %q/%q, want %q/%q", i, opts[i].Key, opts[i].Label, it.hotkey, it.label)
		}
	}
}

func setGlobalLayout(t *testing.T, screenKey string, keys []string) {
	t.Helper()
	setPolicy(t, func(p *access.Policy) {
		if p.Settings.MenuLayouts == nil {
			p.Settings.MenuLayouts = map[string]access.ScreenMenu{}
		}
		m := p.Settings.MenuLayouts[screenKey]
		if m.Scopes == nil {
			m.Scopes = map[string][]string{}
		}
		m.Scopes["global"] = keys
		p.Settings.MenuLayouts[screenKey] = m
	})
}

func TestHubOptionsHonorsCustomOrderAndPromotedItems(t *testing.T) {
	app := newTestApp(t)
	setGlobalLayout(t, scrHub, []string{"admin_assign", "overview"})
	app.loadPolicy()

	opts := hubOptions(app)
	if len(opts) != 2 {
		t.Fatalf("hubOptions = %+v, want exactly 2 entries", opts)
	}
	if opts[0].Label != "Admin: Assign Work" || opts[1].Label != "Overview" {
		t.Errorf("opts = %q, %q, want Assign Work first, Overview second", opts[0].Label, opts[1].Label)
	}
}

func TestHubOptionsStillHidesWhatThisViewerCannotSee(t *testing.T) {
	app := newTestApp(t)
	setGlobalLayout(t, scrHub, []string{"admin_assign", "overview"})
	setPolicy(t, func(p *access.Policy) {
		p.Users["partdb:checker3"] = &access.User{Groups: []string{"checker"}, TwoFA: access.TwoFAExempt}
	})
	signOn(app, "checker3", "")

	opts := hubOptions(app)
	for _, o := range opts {
		if o.Label == "Admin: Assign Work" {
			t.Errorf("checker3 has no user_mgmt permission — Assign Work must not appear, got %+v", opts)
		}
	}
}

// TestResolveMenuKeysPrecedence covers the 3-tier fallback: user beats
// group, group beats global, global beats the hard-coded default, and a
// user in two groups resolves via the alphabetically-first one that
// actually has an override (sorted-groups semantics, not insertion order).
func TestResolveMenuKeysPrecedence(t *testing.T) {
	def := []string{"fallback"}
	p := &access.Policy{Settings: access.Settings{MenuLayouts: map[string]access.ScreenMenu{
		"scr": {Scopes: map[string][]string{
			"global":              {"g"},
			"group:alpha":         {"ga"},
			"group:zulu":          {"gz"},
			"user:modernwms:dave": {"u"},
		}},
	}}}

	if got := resolveMenuKeys(p, "scr", "", nil, def); len(got) != 1 || got[0] != "g" {
		t.Errorf("no user/groups still gets the global override = %v, want [g]", got)
	}
	if got := resolveMenuKeys(&access.Policy{}, "scr", "", nil, def); len(got) != 1 || got[0] != "fallback" {
		t.Errorf("no MenuLayouts at all = %v, want default %v", got, def)
	}
	if got := resolveMenuKeys(p, "scr", "modernwms:someone", []string{"zulu"}, def); len(got) != 1 || got[0] != "gz" {
		t.Errorf("group only = %v, want [gz]", got)
	}
	if got := resolveMenuKeys(p, "scr", "modernwms:someone", []string{"zulu", "alpha"}, def); len(got) != 1 || got[0] != "ga" {
		t.Errorf("two groups = %v, want [ga] (alpha sorts first)", got)
	}
	if got := resolveMenuKeys(p, "scr", "modernwms:dave", []string{"zulu"}, def); len(got) != 1 || got[0] != "u" {
		t.Errorf("user beats group = %v, want [u]", got)
	}
	if got := resolveMenuKeys(p, "scr", "modernwms:someone", nil, def); len(got) != 1 || got[0] != "g" {
		t.Errorf("no user/group override = %v, want global [g]", got)
	}
	if got := resolveMenuKeys(p, "scr", "modernwms:someone", []string{"nope"}, def); len(got) != 1 || got[0] != "g" {
		t.Errorf("group with no override falls through to global = %v, want [g]", got)
	}
}

func menuEditorFor(app *App, screenKey, scope string) *menuEditorScreen {
	app.menuEdit = &menuEditDraft{screenKey: screenKey, scope: scope}
	app.goTo(scrMenuEditor)
	return app.screens[scrMenuEditor].(*menuEditorScreen)
}

func TestMenuEditorToggleMoveAndSave(t *testing.T) {
	app := newTestApp(t)
	scr := menuEditorFor(app, scrHub, "global")
	if len(scr.vals) != len(defaultMainMenuKeys) || scr.vals[0] != defaultMainMenuKeys[0] {
		t.Fatalf("initial working copy = %v, want it seeded from the default set %v", scr.vals, defaultMainMenuKeys)
	}

	// Turn "Script Hub" (default position 4, cursor row 3) off.
	scr.row = 3
	scr.HandleKey(app, tea.KeyMsg{Type: tea.KeySpace})
	if idx := scr.valIndex("scripts"); idx != -1 {
		t.Fatalf("scripts should be off after toggling, still at index %d", idx)
	}

	// Promote "Admin: Assign Work" on, then move it to the very front.
	for i, k := range scr.display(app) {
		if k == "admin_assign" {
			scr.row = i
			break
		}
	}
	scr.HandleKey(app, tea.KeyMsg{Type: tea.KeySpace}) // turn it on (appends to the end)
	for scr.valIndex("admin_assign") > 0 {
		scr.HandleKey(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{','}}) // move it up
	}
	if scr.valIndex("admin_assign") != 0 {
		t.Fatalf("admin_assign should now be first, vals = %v", scr.vals)
	}

	scr.HandleKey(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})

	saved := app.pol().Settings.MenuLayouts[scrHub].Scopes["global"]
	if len(saved) == 0 || saved[0] != "admin_assign" {
		t.Fatalf("saved layout = %v, want admin_assign first", saved)
	}
	for _, k := range saved {
		if k == "scripts" {
			t.Fatalf("saved layout = %v, scripts should have been dropped", saved)
		}
	}
	if !auditHas(t, "menu custom_menu scope") && !auditHas(t, "menu hub scope global customized") {
		t.Error("expected an audit log entry")
	}
}

// TestSubMenuCreateIncludeAndExpand drives the whole "move items into a
// sub-menu" flow end to end: create one, put two items in it, include it in
// the global layout, then confirm it actually renders as a navigable entry
// showing exactly those two items — the user's literal ask this session.
func TestSubMenuCreateIncludeAndExpand(t *testing.T) {
	app := newTestApp(t)
	app.menuEdit = &menuEditDraft{screenKey: scrPickerHub}
	app.goTo(scrSubMenuManage)
	subMenuManageKeys(app, "", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	app.pick.OnFree(app, "More")

	if app.cur != scrMenuEditor || app.menuEdit.subMenu != "More" {
		t.Fatalf("creating a sub-menu should land in the editor for it, cur=%q menuEdit=%+v", app.cur, app.menuEdit)
	}
	sub := app.screens[scrMenuEditor].(*menuEditorScreen)
	for _, want := range []string{"my_accuracy", "my_exports"} {
		for i, k := range sub.display(app) {
			if k == want {
				sub.row = i
				break
			}
		}
		sub.HandleKey(app, tea.KeyMsg{Type: tea.KeySpace})
	}
	sub.HandleKey(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})

	stored := app.pol().Settings.MenuLayouts[scrPickerHub].SubMenus["More"]
	if len(stored) != 2 || stored[0] != "my_accuracy" || stored[1] != "my_exports" {
		t.Fatalf("stored sub-menu = %v, want [my_accuracy my_exports]", stored)
	}

	// Include it in the picker hub's global layout, dropping the two items
	// it now contains from the top level.
	top := menuEditorFor(app, scrPickerHub, "global")
	for _, drop := range []string{"my_accuracy", "my_exports"} {
		if vi := top.valIndex(drop); vi >= 0 {
			top.row = vi
			top.HandleKey(app, tea.KeyMsg{Type: tea.KeySpace})
		}
	}
	for i, k := range top.display(app) {
		if k == "submenu:More" {
			top.row = i
			break
		}
	}
	top.HandleKey(app, tea.KeyMsg{Type: tea.KeySpace})
	top.HandleKey(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})

	// Now render the real picker hub and follow the synthetic entry.
	app.cur, app.stack, app.menuEdit = scrHub, nil, nil
	opts := resolveAndRenderMenu(app, scrPickerHub, pickerHubCatalog, defaultPickerHubKeys)
	var found *menuOption
	for i := range opts {
		if opts[i].Label == "More" {
			found = &opts[i]
		}
		if opts[i].Label == "My accuracy" || opts[i].Label == "My Exports" {
			t.Errorf("my_accuracy/my_exports should no longer be at the top level, got %q", opts[i].Label)
		}
	}
	if found == nil {
		t.Fatalf("no synthetic \"More\" entry in %+v", opts)
	}
	found.Go(app)
	if app.cur != scrSubMenuView || app.viewingSubmenu == nil || app.viewingSubmenu.name != "More" {
		t.Fatalf("following the sub-menu entry should open scrSubMenuView for it, cur=%q view=%+v", app.cur, app.viewingSubmenu)
	}
	body := app.screens[scrSubMenuView].Body(app)
	for _, want := range []string{"My accuracy", "My Exports"} {
		if !contains(body, want) {
			t.Errorf("sub-menu body missing %q:\n%s", want, body)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

// TestMenuLayoutsCrossScopeIsolation: saving one scope/screen must never
// leak into another.
func TestMenuLayoutsCrossScopeIsolation(t *testing.T) {
	app := newTestApp(t)
	setPolicy(t, func(p *access.Policy) { p.Groups["pickers"] = &access.Group{Perms: map[string]string{}} })

	grp := menuEditorFor(app, scrHub, "group:pickers")
	grp.row = 0
	grp.HandleKey(app, tea.KeyMsg{Type: tea.KeySpace})
	grp.HandleKey(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})

	if g := app.pol().Settings.MenuLayouts[scrHub].Scopes["global"]; len(g) != 0 {
		t.Errorf("global scope should be untouched, got %v", g)
	}
	if pk := app.pol().Settings.MenuLayouts[scrPickerHub]; len(pk.Scopes) != 0 {
		t.Errorf("scrPickerHub's own layout should be untouched, got %+v", pk)
	}
}

// TestEditingAUserPreservesMenuLayouts is the same bug class as BadgeToken/
// the bot-link fields earlier this session, checked directly rather than
// assumed: accessUserEditScreen's submit rebuilds a *access.User record, and
// Settings.MenuLayouts lives on *access.Policy.Settings, a different struct
// entirely — that rebuild has no field for it and cannot touch it, which
// this proves by actually editing an unrelated user and checking nothing moved.
func TestEditingAUserPreservesMenuLayouts(t *testing.T) {
	app := adminApp(t)
	setGlobalLayout(t, scrHub, []string{"admin_assign", "overview"})
	setPolicy(t, func(p *access.Policy) {
		p.Users["partdb:exportbot"] = &access.User{Groups: []string{"exporter"}}
	})
	app.loadPolicy()
	app.accessEdit = &accessEdit{User: "partdb:exportbot"}
	app.goTo(scrAccessUserEdit)
	app.screens[scrAccessUserEdit].(*formScreen).submit(app, []string{"exporter", "default", "", "all", "", "", "", "", "note"})

	got := app.pol().Settings.MenuLayouts[scrHub].Scopes["global"]
	if len(got) != 2 || got[0] != "admin_assign" {
		t.Fatalf("editing an unrelated user must not touch MenuLayouts, got %v", got)
	}
}
