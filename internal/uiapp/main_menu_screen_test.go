package uiapp

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
)

// TestHubOptionsUsesDefaultWhenMenuLayoutsUnset also covers the
// auto-numbered-by-position scheme: items are keyed "1","2","3"... in
// display order, not by each catalog item's own fixed (now cosmetic-only)
// hotkey — see keyAssigner.
func TestHubOptionsUsesDefaultWhenMenuLayoutsUnset(t *testing.T) {
	app := newTestApp(t)
	opts := hubOptions(app)
	if len(opts) != len(defaultMainMenuKeys) {
		t.Fatalf("hubOptions = %d entries, want %d (the default set)", len(opts), len(defaultMainMenuKeys))
	}
	for i, key := range defaultMainMenuKeys {
		it := catalogItemByKey(mainMenuCatalog, key)
		wantKey := string(keyPool[i])
		if opts[i].Key != wantKey || opts[i].Label != it.label {
			t.Errorf("opts[%d] = %q/%q, want %q/%q", i, opts[i].Key, opts[i].Label, wantKey, it.label)
		}
	}
}

// TestHubAlwaysPinsLogOutAndExit mirrors TestPickerHubAlwaysPinsLogOutAndExit
// — the fix for a reported gap where a customized main hub could lose its
// own way out (no Log out/Exit, no button, only Escape/Q from a keyboard).
// Goes through the screen's own options func, not hubOptions itself: that
// bare list is also the classic tab bar's source (app.go's viewClassic),
// which must never grow these two one-shot actions as tabs.
func TestHubAlwaysPinsLogOutAndExit(t *testing.T) {
	app := newTestApp(t)
	setGlobalLayout(t, scrHub, []string{"overview"})
	app.loadPolicy()

	scr := hubScreen().(*menuScreen)
	opts := scr.options(app)
	last := opts[len(opts)-1]
	secondLast := opts[len(opts)-2]
	if secondLast.Label != "Log out" || last.Label != "Exit" {
		t.Fatalf("Log out/Exit should always be pinned last, got %+v", opts)
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

// pickItemByKey finds a pick item by its Key, so a test can drive a specific
// choice out of a startPick list (via app.pick.OnPick) without depending on
// its on-screen order.
func pickItemByKey(t *testing.T, st *pickState, key string) pickItem {
	t.Helper()
	for _, it := range st.Items {
		if it.Key == key {
			return it
		}
	}
	t.Fatalf("no pick item with key %q in %+v", key, st.Items)
	return pickItem{}
}

// TestOrganiseSaveApplyAndResetTemplate drives the whole "O" flow: save the
// current order as a named template, reset to the built-in default, then
// apply the saved template back — covering both halves of "a most
// appropriate order, or an order I set."
func TestOrganiseSaveApplyAndResetTemplate(t *testing.T) {
	app := newTestApp(t)
	scr := menuEditorFor(app, scrHub, "global")

	// Turn "Script Hub" off, then save this as a template.
	scr.row = 3
	scr.HandleKey(app, tea.KeyMsg{Type: tea.KeySpace})
	customized := append([]string(nil), scr.vals...)

	scr.HandleKey(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	app.pick.OnPick(app, pickItemByKey(t, app.pick, "save"))
	app.pick.OnFree(app, "My order")

	if app.cur != scrMenuEditor {
		t.Fatalf("saving a template should return to the editor, cur = %q", app.cur)
	}
	saved := app.pol().Settings.MenuLayouts[scrHub].Templates["My order"]
	if len(saved) != len(customized) || saved[0] != customized[0] {
		t.Fatalf("saved template = %v, want %v", saved, customized)
	}

	// Reset to the built-in default — "Script Hub" comes back.
	scr.HandleKey(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	app.pick.OnPick(app, pickItemByKey(t, app.pick, "default"))
	if len(scr.vals) != len(defaultMainMenuKeys) || scr.valIndex("scripts") == -1 {
		t.Fatalf("after reset, vals = %v, want the default set (scripts included)", scr.vals)
	}

	// Apply the saved template back — "Script Hub" is gone again.
	scr.HandleKey(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	app.pick.OnPick(app, pickItemByKey(t, app.pick, "apply:My order"))
	if scr.valIndex("scripts") != -1 {
		t.Fatalf("after applying the template, vals = %v, scripts should be gone again", scr.vals)
	}
	if !auditHas(t, "template My order saved") {
		t.Error("expected an audit log entry for saving the template")
	}
}

// TestHubOptionsNumbersByPositionNotByFixedIdentity confirms the key
// assigned to each item follows where it sits in the chosen order, not a
// per-catalog-item fixed key: promoting "Admin: Assign Work" to the front
// gives it "1", not its old cosmetic-only hotkey "h".
func TestHubOptionsNumbersByPositionNotByFixedIdentity(t *testing.T) {
	app := newTestApp(t)
	setGlobalLayout(t, scrHub, []string{"admin_assign", "overview", "partdb"})
	app.loadPolicy()

	opts := hubOptions(app)
	want := []struct{ key, label string }{
		{"1", "Admin: Assign Work"}, {"2", "Overview"}, {"3", "PartDB Hub"},
	}
	for i, w := range want {
		if opts[i].Key != w.key || opts[i].Label != w.label {
			t.Errorf("opts[%d] = %q/%q, want %q/%q", i, opts[i].Key, opts[i].Label, w.key, w.label)
		}
	}
}

// TestPickerHubNumberingNeverCollidesWithPinnedLogOutExit confirms a full
// layout (enough items to reach "9" under plain sequential numbering) still
// skips the two keys Log out/Exit already pin.
func TestPickerHubNumberingNeverCollidesWithPinnedLogOutExit(t *testing.T) {
	app := newTestApp(t)
	setGlobalLayout(t, scrPickerHub, defaultPickerHubKeys) // 9 items: would reach key "9" unreserved
	app.loadPolicy()

	scr := pickerHubScreen().(*menuScreen)
	opts := scr.options(app)
	seen := map[string]int{}
	for _, o := range opts {
		seen[o.Key]++
	}
	for key, n := range seen {
		if n > 1 {
			t.Fatalf("key %q used %d times: %+v", key, n, opts)
		}
	}
	logOut, exit := opts[len(opts)-2], opts[len(opts)-1]
	if logOut.Key != "9" || exit.Key != "0" {
		t.Fatalf("Log out/Exit should keep their pinned keys, got %q=%q, %q=%q", logOut.Label, logOut.Key, exit.Label, exit.Key)
	}
	for _, o := range opts[:len(opts)-2] {
		if o.Key == "9" || o.Key == "0" {
			t.Errorf("a regular item took a pinned key: %+v", o)
		}
	}
}

// TestSubMenuRenameUpdatesStorageAndScopeReferences covers both halves of a
// rename: the SubMenus key itself, and every scope that had "submenu:<old>"
// included — those must keep pointing at the sub-menu under its new name,
// not silently stop resolving.
func TestSubMenuRenameUpdatesStorageAndScopeReferences(t *testing.T) {
	app := newTestApp(t)
	app.menuEdit = &menuEditDraft{screenKey: scrPickerHub}
	app.goTo(scrSubMenuManage)
	subMenuManageKeys(app, "", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	app.pick.OnFree(app, "More")
	sub := app.screens[scrMenuEditor].(*menuEditorScreen)
	for i, k := range sub.display(app) {
		if k == "my_accuracy" {
			sub.row = i
			break
		}
	}
	sub.HandleKey(app, tea.KeyMsg{Type: tea.KeySpace})
	sub.HandleKey(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})

	top := menuEditorFor(app, scrPickerHub, "global")
	for i, k := range top.display(app) {
		if k == "submenu:More" {
			top.row = i
			break
		}
	}
	top.HandleKey(app, tea.KeyMsg{Type: tea.KeySpace})
	top.HandleKey(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})

	app.menuEdit = &menuEditDraft{screenKey: scrPickerHub}
	app.goTo(scrSubMenuManage)
	subMenuManageKeys(app, "More", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	app.pick.OnFree(app, "Extras")

	m := app.pol().Settings.MenuLayouts[scrPickerHub]
	if _, stillThere := m.SubMenus["More"]; stillThere {
		t.Error("the old sub-menu name should be gone")
	}
	if items := m.SubMenus["Extras"]; len(items) != 1 || items[0] != "my_accuracy" {
		t.Fatalf("renamed sub-menu's items = %v, want [my_accuracy]", items)
	}
	global := m.Scopes["global"]
	found := false
	for _, k := range global {
		if k == "submenu:More" {
			t.Error("global scope still references the old sub-menu name")
		}
		if k == "submenu:Extras" {
			found = true
		}
	}
	if !found {
		t.Errorf("global scope should now reference submenu:Extras, got %v", global)
	}
}

// TestSubMenuCreateIncludeAndExpand drives the whole "move items into a
// sub-menu" flow end to end: create one, put two items in it, include it in
// the global layout, then confirm it actually renders as a navigable entry
// showing exactly those two items — the user's literal ask this session.
// TestSavingASubMenuRemovesItsItemsFromEveryTopLevelScope is the fix for a
// real reported gap: putting an item in a sub-menu used to leave it also
// still "on" at the top level (a copy, not a move) — now saving a sub-menu's
// membership immediately strips those same keys out of every scope for that
// screen, not just the one being edited at the time.
func TestSavingASubMenuRemovesItsItemsFromEveryTopLevelScope(t *testing.T) {
	app := newTestApp(t)
	setPolicy(t, func(p *access.Policy) {
		p.Settings.MenuLayouts = map[string]access.ScreenMenu{
			scrPickerHub: {Scopes: map[string][]string{
				"group:checker":  {"overview", "my_accuracy", "my_exports"},
				"group:pickers2": {"my_accuracy", "overview"},
			}},
		}
	})
	app.loadPolicy()

	app.menuEdit = &menuEditDraft{screenKey: scrPickerHub}
	app.goTo(scrSubMenuManage)
	subMenuManageKeys(app, "", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	app.pick.OnFree(app, "More")
	sub := app.screens[scrMenuEditor].(*menuEditorScreen)
	for i, k := range sub.display(app) {
		if k == "my_accuracy" {
			sub.row = i
			break
		}
	}
	sub.HandleKey(app, tea.KeyMsg{Type: tea.KeySpace})
	sub.HandleKey(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})

	m := app.pol().Settings.MenuLayouts[scrPickerHub]
	for scope, items := range m.Scopes {
		for _, k := range items {
			if k == "my_accuracy" {
				t.Errorf("scope %s still lists my_accuracy directly after it moved into a sub-menu: %v", scope, items)
			}
		}
	}
	if items := m.Scopes["group:checker"]; len(items) != 2 || items[0] != "overview" || items[1] != "my_exports" {
		t.Errorf("group:checker = %v, want [overview my_exports] (order preserved, duplicate removed)", items)
	}
}

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
