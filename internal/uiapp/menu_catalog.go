package uiapp

import (
	"sort"
	"strings"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
)

// menuCatalogItem is one entry an admin can choose to show on a customizable
// menu — a real target (open) with a fixed hotkey of its own regardless of
// where it lands in the chosen order, so hardcoded tab-bar jumps like
// toggleLego/toggleAdminView keep lighting up the right tab whenever that
// tab is actually shown. See menuRegistry for which screens are customizable
// and resolveAndRenderMenu for how a catalog turns into what's on screen.
type menuCatalogItem struct {
	key, hotkey, label string
	allowed            func(app *App) bool
	open               func(app *App)
}

// catalogDynamicLabels overrides label by key for the rare item whose text
// depends on who's looking — kept out of menuCatalogItem itself so every
// catalog entry can stay a plain positional literal. The one case so far is
// the picker hub's "Request a set to check" vs. "Request an order to pick,"
// which depends on which role the viewer actually holds (see picker_hub.go).
var catalogDynamicLabels = map[string]func(app *App) string{}

func (it *menuCatalogItem) labelFor(app *App) string {
	if f := catalogDynamicLabels[it.key]; f != nil {
		return f(app)
	}
	return it.label
}

func catalogItemByKey(catalog []menuCatalogItem, key string) *menuCatalogItem {
	for i := range catalog {
		if catalog[i].key == key {
			return &catalog[i]
		}
	}
	return nil
}

// menuScreenDef registers one screen as customizable: its catalog of
// reassignable items and the order shown before any admin ever customizes
// it. Bringing another menuScreen under this system later is exactly this —
// a catalog slice, a defaultKeys list, one more entry in menuRegistry — not
// a redesign.
type menuScreenDef struct {
	screenKey, title string
	catalog          []menuCatalogItem
	defaultKeys      []string
}

var menuRegistry = []menuScreenDef{
	{scrHub, "Main hub", mainMenuCatalog, defaultMainMenuKeys},
	{scrPickerHub, "Picker/Checker hub", pickerHubCatalog, defaultPickerHubKeys},
}

func menuScreenDefFor(screenKey string) *menuScreenDef {
	for i := range menuRegistry {
		if menuRegistry[i].screenKey == screenKey {
			return &menuRegistry[i]
		}
	}
	return nil
}

// resolveMenuKeys is the 3-tier lookup every customizable screen resolves
// through: this viewer's own override, else the first of their (sorted)
// groups that has one, else the global override, else defaultKeys. Mirrors
// effectiveFor's sorted-groups precedent (access.go), not the 2-tier,
// group-blind pick() used for GraceMinutes/IdleMinutes/MaxSessionHours — a
// menu layout can meaningfully differ by group (pickers vs. checkers), those
// per-user timings never do. An empty or missing list at any tier falls
// through to the next one, so a blank save can never leave a viewer with no
// menu at all.
func resolveMenuKeys(p *access.Policy, screenKey, userKey string, groups []string, defaultKeys []string) []string {
	m, ok := p.Settings.MenuLayouts[screenKey]
	if !ok {
		return defaultKeys
	}
	scope := func(key string) []string {
		if items := m.Scopes[key]; len(items) > 0 {
			return items
		}
		return nil
	}
	if userKey != "" {
		if items := scope("user:" + userKey); items != nil {
			return items
		}
		sorted := append([]string(nil), groups...)
		sort.Strings(sorted)
		for _, g := range sorted {
			if items := scope("group:" + g); items != nil {
				return items
			}
		}
	}
	if items := scope("global"); items != nil {
		return items
	}
	return defaultKeys
}

// resolveAndRenderMenu is every customizable screen's options func: resolve
// this viewer's layout, then turn keys into menuOptions, expanding
// "submenu:<name>" into a synthetic entry that opens the shared sub-menu
// viewer (submenu_screen.go) instead of a catalog target directly.
func resolveAndRenderMenu(app *App, screenKey string, catalog []menuCatalogItem, defaultKeys []string) []menuOption {
	userKey, groups := "", []string(nil)
	if app.session != nil {
		userKey = access.Key(app.session.Source, app.session.Username)
		if app.pUser != nil {
			groups = app.pUser.Groups
		}
	}
	keys := resolveMenuKeys(app.pol(), screenKey, userKey, groups, defaultKeys)

	// Catalog items keep their own fixed hotkey; a sub-menu entry gets one
	// assigned from whatever's left over, so it can never collide with a
	// real item or with the global single-letter mnemonics (q/u/l/g/v are
	// intercepted before any menu ever sees them — see handleGlobalKey).
	used := map[string]bool{"q": true, "u": true, "l": true, "g": true, "v": true}
	type pending struct {
		idx  int
		name string
	}
	var opts []menuOption
	var subs []pending
	for _, key := range keys {
		if name, ok := strings.CutPrefix(key, "submenu:"); ok {
			subs = append(subs, pending{idx: len(opts), name: name})
			opts = append(opts, menuOption{})
			continue
		}
		it := catalogItemByKey(catalog, key)
		if it == nil || !it.allowed(app) {
			continue
		}
		used[it.hotkey] = true
		opts = append(opts, menuOption{Key: it.hotkey, Label: it.labelFor(app), Go: it.open})
	}
	pool := "abcdefhijkmnoprstwxyz0123456789"
	next := 0
	for _, s := range subs {
		hotkey := ""
		for next < len(pool) {
			c := string(pool[next])
			next++
			if !used[c] {
				hotkey = c
				break
			}
		}
		name := s.name
		opts[s.idx] = menuOption{Key: hotkey, Label: name, Go: func(app *App) {
			app.viewingSubmenu = &submenuView{screenKey: screenKey, name: name}
			app.goTo(scrSubMenuView)
		}}
	}
	return opts
}
