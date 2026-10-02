package uiapp

import (
	"sort"
	"strings"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
)

// menuCatalogItem is one entry an admin can choose to show on a customizable
// menu. hotkey is only a best-effort identity tag for toggleLego/
// toggleAdminView's hardcoded tab-bar jumps to match against — the key a
// viewer actually presses is assigned by position (see resolveAndRenderMenu),
// since items renumber as a layout is reordered, same as any other numbered
// list in this app. See menuRegistry for which screens are customizable.
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

// keyPool is every single key a rendered menu item can be numbered with, in
// assignment order: 1-9 then safe letters (q/u/l/g/v skipped — those are
// intercepted globally before any menu ever sees them, see handleGlobalKey),
// then 0 last, since 0 is "Return"/"Exit" everywhere by convention.
const keyPool = "123456789abcdefhijkmnoprstwxyz0"

// keyAssigner hands out keys from keyPool in order, skipping the global
// single-letter mnemonics and anything the caller already reserved — shared
// by resolveAndRenderMenu and the sub-menu viewer so both number their items
// the same way.
func keyAssigner(reserved ...string) func() string {
	used := map[string]bool{"q": true, "u": true, "l": true, "g": true, "v": true}
	for _, r := range reserved {
		used[r] = true
	}
	pos := 0
	return func() string {
		for pos < len(keyPool) {
			c := string(keyPool[pos])
			pos++
			if !used[c] {
				return c
			}
		}
		return "?"
	}
}

// resolveAndRenderMenu is every customizable screen's options func: resolve
// this viewer's layout, then render it — each item numbered by its position
// in that layout (1, 2, 3, ... from keyPool), not by a fixed per-item key,
// so reordering a layout renumbers it the way reordering any other numbered
// list would. reserved excludes keys a caller has already spoken for (the
// picker hub pins "9"/"0" to Log out/Exit — see pickerHubScreen). A
// "submenu:<name>" entry renders as a synthetic item opening the shared
// sub-menu viewer (submenu_screen.go) instead of a catalog target.
func resolveAndRenderMenu(app *App, screenKey string, catalog []menuCatalogItem, defaultKeys []string, reserved ...string) []menuOption {
	userKey, groups := "", []string(nil)
	if app.session != nil {
		userKey = access.Key(app.session.Source, app.session.Username)
		if app.pUser != nil {
			groups = app.pUser.Groups
		}
	}
	keys := resolveMenuKeys(app.pol(), screenKey, userKey, groups, defaultKeys)
	nextKey := keyAssigner(reserved...)

	var opts []menuOption
	for _, key := range keys {
		if name, ok := strings.CutPrefix(key, "submenu:"); ok {
			opts = append(opts, menuOption{Key: nextKey(), Label: name, Go: func(app *App) {
				app.viewingSubmenu = &submenuView{screenKey: screenKey, name: name}
				app.goTo(scrSubMenuView)
			}})
			continue
		}
		it := catalogItemByKey(catalog, key)
		if it == nil || !it.allowed(app) {
			continue
		}
		opts = append(opts, menuOption{Key: nextKey(), Label: it.labelFor(app), Go: it.open})
	}
	return opts
}
