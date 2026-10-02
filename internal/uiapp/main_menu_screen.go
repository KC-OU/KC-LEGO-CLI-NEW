package uiapp

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
)

// Admin → Access Control → Customize menus: per screen (main hub, picker/
// checker hub, ...), per scope (everyone, a group, or one person) — reorder
// what's on it, drop items nobody there uses, promote an Admin row onto it,
// or group a few items into an admin-named sub-menu. Each viewer still only
// ever sees what their own permissions allow; this only changes where it
// appears, never who can reach it. See menu_catalog.go for the registry and
// resolution, access.Settings.MenuLayouts for storage.
//
// This is a different concern from the older per-user "Menu tabs" checklist
// (Access Control → Users → B): that one grants/denies the underlying
// permission (can this person use Part-DB at all), this one only rearranges
// what's already visible to them. Both stay, on purpose.

const (
	scrCustomMenu    = "custom_menu"    // per-screen hub: edit a layout, or manage sub-menus
	scrMenuEditor    = "menu_editor"    // the toggle/reorder editor itself
	scrSubMenuManage = "submenu_manage" // list/add/delete this screen's sub-menus
)

// menuEditDraft is what's being customized right now: always a screenKey,
// plus either a scope (editing that scope's top-level layout) or a subMenu
// name (editing that sub-menu's own item list) — never both at once.
type menuEditDraft struct {
	screenKey, scope, subMenu string
}

func startMenuCustomize(app *App) {
	var items []pickItem
	for _, def := range menuRegistry {
		items = append(items, pickItem{Key: def.screenKey, Label: def.title})
	}
	startPick(app, &pickState{
		Header: "Customize which menu?",
		Prompt: "Choose",
		Items:  items,
		OnPick: func(app *App, it pickItem) {
			app.menuEdit = &menuEditDraft{screenKey: it.Key}
			app.goTo(scrCustomMenu)
		},
	})
}

func customMenuHubScreen() screenModel {
	return &menuScreen{
		panelID: "MENUHUB",
		title:   "Customize Menu",
		options: func(app *App) []menuOption {
			return []menuOption{
				{Key: "1", Label: "Edit a layout (choose who for)", Go: startMenuScopePick},
				{Key: "2", Label: "Manage sub-menus for this screen", Go: func(app *App) { app.goTo(scrSubMenuManage) }},
				{Key: "0", Label: "Return", Go: func(app *App) { app.onBack() }},
			}
		},
		intro: func(app *App) string {
			if app.menuEdit == nil {
				return ""
			}
			def := menuScreenDefFor(app.menuEdit.screenKey)
			if def == nil {
				return ""
			}
			return app.theme.Muted.Render(def.title)
		},
	}
}

func sourceOf(system string) string {
	if system == "Part-DB" {
		return "partdb"
	}
	return "modernwms"
}

// startMenuScopePick offers who a layout applies to: everyone, one group, or
// one person — any known account, not only ones already governed by the
// access policy, since a menu layout lives on Settings, not on a User record.
func startMenuScopePick(app *App) {
	if app.menuEdit == nil {
		app.onBack()
		return
	}
	items := []pickItem{{Key: "global", Label: "Everyone (global)"}}
	for _, name := range sortedMapKeys(app.pol().Groups) {
		items = append(items, pickItem{Key: "group:" + name, Label: "Group: " + name})
	}
	rows, _ := app.users.ListAll(app.ctx())
	seen := map[string]bool{}
	for _, r := range rows {
		if r.Username == "" {
			continue
		}
		key := access.Key(sourceOf(r.System), r.Username)
		if seen[key] {
			continue
		}
		seen[key] = true
		items = append(items, pickItem{Key: "user:" + key, Label: "User: " + r.Username + " (" + r.System + ")"})
	}
	startPick(app, &pickState{
		Header: "Customize for whom?",
		Prompt: "Choose",
		Items:  items,
		OnPick: func(app *App, it pickItem) {
			app.menuEdit.scope, app.menuEdit.subMenu = it.Key, ""
			app.goTo(scrMenuEditor)
		},
	})
}

// ---- the toggle/reorder editor, shared by "edit a scope's layout" and
// "edit one sub-menu's contents" ----

type menuEditorScreen struct {
	base
	row  int
	vals []string // working copy: the chosen, ordered subset of the screen's catalog (+ sub-menu pseudo-keys, top-level mode only)
}

func (s *menuEditorScreen) PanelID() string { return "MENUED" }
func (s *menuEditorScreen) Title() string   { return "Customize Menu" }
func (s *menuEditorScreen) FKeys() [][2]string {
	return [][2]string{{"F3", "Exit"}, {"F12", "Cancel"}}
}

func (s *menuEditorScreen) def(app *App) *menuScreenDef {
	if app.menuEdit == nil {
		return nil
	}
	return menuScreenDefFor(app.menuEdit.screenKey)
}

// editingSubMenu is whether this instance is editing one named sub-menu's
// own contents rather than a scope's top-level layout.
func (s *menuEditorScreen) editingSubMenu(app *App) bool {
	return app.menuEdit != nil && app.menuEdit.subMenu != ""
}

func (s *menuEditorScreen) stored(app *App) []string {
	if app.menuEdit == nil {
		return nil
	}
	m := app.pol().Settings.MenuLayouts[app.menuEdit.screenKey]
	if s.editingSubMenu(app) {
		return m.SubMenus[app.menuEdit.subMenu]
	}
	return m.Scopes[app.menuEdit.scope]
}

func (s *menuEditorScreen) OnEnter(app *App) {
	app.loadPolicy()
	s.row = 0
	s.vals = append([]string(nil), s.stored(app)...)
	if len(s.vals) == 0 {
		if def := s.def(app); def != nil && !s.editingSubMenu(app) {
			s.vals = append([]string(nil), def.defaultKeys...)
		}
	}
}

// assignable is every key this editor can toggle in: the screen's real
// catalog items, plus — only when editing a scope's top-level layout, never
// inside a sub-menu itself — every existing sub-menu as a "submenu:<name>"
// pseudo-key, so one can be included/positioned exactly like a real item.
func (s *menuEditorScreen) assignable(app *App) []string {
	def := s.def(app)
	if def == nil {
		return nil
	}
	var out []string
	for _, it := range def.catalog {
		out = append(out, it.key)
	}
	if !s.editingSubMenu(app) {
		m := app.pol().Settings.MenuLayouts[app.menuEdit.screenKey]
		for _, name := range sortedMapKeys(m.SubMenus) {
			out = append(out, "submenu:"+name)
		}
	}
	return out
}

func (s *menuEditorScreen) itemLabel(app *App, key string) string {
	if name, ok := strings.CutPrefix(key, "submenu:"); ok {
		return "▸ " + name
	}
	if def := s.def(app); def != nil {
		if it := catalogItemByKey(def.catalog, key); it != nil {
			return it.labelFor(app)
		}
	}
	return key
}

// display is the on-screen order: chosen items first in their chosen
// sequence, then every other assignable key after, so nothing is ever
// missing from the list, just ranked.
func (s *menuEditorScreen) display(app *App) []string {
	out := append([]string(nil), s.vals...)
	chosen := map[string]bool{}
	for _, k := range s.vals {
		chosen[k] = true
	}
	for _, k := range s.assignable(app) {
		if !chosen[k] {
			out = append(out, k)
		}
	}
	return out
}

func (s *menuEditorScreen) valIndex(key string) int {
	for i, k := range s.vals {
		if k == key {
			return i
		}
	}
	return -1
}

func (s *menuEditorScreen) capacity(app *App, total int) int {
	if app.height <= 0 {
		return total
	}
	chrome := 8 + 2 + 2
	if app.message != "" {
		chrome++
	}
	return max(3, app.height-chrome)
}

func (s *menuEditorScreen) Body(app *App) string {
	t := app.theme
	if app.menuEdit == nil {
		return t.Muted.Render("Nothing to edit.")
	}
	disp := s.display(app)
	s.row = min(s.row, max(0, len(disp)-1))
	cap := s.capacity(app, len(disp))
	top, title := 0, "for "+scopeLabel(app.menuEdit)
	if s.editingSubMenu(app) {
		title = "sub-menu " + app.menuEdit.subMenu
	}
	if len(disp) > cap {
		top = max(0, min(s.row-cap/2, len(disp)-cap))
		title = fmt.Sprintf("%s  (%d-%d of %d)", title, top+1, top+cap, len(disp))
	}
	var b strings.Builder
	b.WriteString(t.Strong.Render(title) + "\n\n")
	for i := top; i < min(top+cap, len(disp)); i++ {
		key := disp[i]
		pos, mark, style := "  —", "off", t.Muted
		if vi := s.valIndex(key); vi >= 0 {
			pos, mark, style = fmt.Sprintf("%3d", vi+1), "on ", t.Success
		}
		cell := fmt.Sprintf("%s  %-28s %s", pos, s.itemLabel(app, key), mark)
		if i == s.row {
			b.WriteString(t.TitleReverse.Render(cell) + "\n")
		} else {
			b.WriteString(style.Render(cell) + "\n")
		}
	}
	b.WriteString("\n" + t.Muted.Render("↑/↓ choose · Space on/off · , move up · . move down · S save · Esc"))
	return b.String()
}

// scopeLabel is a friendlier form of a scope key for the editor's title —
// "group:checker" -> "group checker", "user:partdb:dave" -> "user dave".
func scopeLabel(d *menuEditDraft) string {
	if d.scope == "global" {
		return "everyone"
	}
	kind, rest, _ := strings.Cut(d.scope, ":")
	if kind == "user" {
		_, name, ok := strings.Cut(rest, ":")
		if ok {
			rest = name
		}
	}
	return kind + " " + rest
}

func (s *menuEditorScreen) HandleKey(app *App, msg tea.KeyMsg) {
	if app.menuEdit == nil {
		return
	}
	disp := s.display(app)
	if len(disp) == 0 {
		return
	}
	switch {
	case msg.Type == tea.KeyUp && s.row > 0:
		s.row--
		return
	case msg.Type == tea.KeyDown && s.row < len(disp)-1:
		s.row++
		return
	case msg.Type == tea.KeySpace || isKey(msg, ' '):
		key := disp[s.row]
		if vi := s.valIndex(key); vi >= 0 {
			s.vals = append(s.vals[:vi], s.vals[vi+1:]...)
		} else {
			s.vals = append(s.vals, key)
		}
		s.followRow(app, key)
	case isKey(msg, ','):
		key := disp[s.row]
		if vi := s.valIndex(key); vi > 0 {
			s.vals[vi-1], s.vals[vi] = s.vals[vi], s.vals[vi-1]
		}
		s.followRow(app, key)
	case isKey(msg, '.'):
		key := disp[s.row]
		if vi := s.valIndex(key); vi >= 0 && vi < len(s.vals)-1 {
			s.vals[vi], s.vals[vi+1] = s.vals[vi+1], s.vals[vi]
		}
		s.followRow(app, key)
	case isKey(msg, 'o'):
		s.openOrganise(app)
	case isKey(msg, 's'):
		d, vals := *app.menuEdit, append([]string(nil), s.vals...)
		what := "menu " + d.screenKey + " scope " + d.scope + " customized"
		if d.subMenu != "" {
			what = "menu " + d.screenKey + " sub-menu " + d.subMenu + " customized"
		}
		app.saveAccess(what, func(p *access.Policy) error {
			if p.Settings.MenuLayouts == nil {
				p.Settings.MenuLayouts = map[string]access.ScreenMenu{}
			}
			m := p.Settings.MenuLayouts[d.screenKey]
			if d.subMenu != "" {
				if m.SubMenus == nil {
					m.SubMenus = map[string][]string{}
				}
				m.SubMenus[d.subMenu] = vals
			} else {
				if m.Scopes == nil {
					m.Scopes = map[string][]string{}
				}
				m.Scopes[d.scope] = vals
			}
			// An item lives in exactly one place: once it's in a sub-menu,
			// it comes off every scope's top-level list — otherwise "move
			// this into a sub-menu" would leave a duplicate behind instead
			// of actually moving it.
			inAnySubMenu := map[string]bool{}
			for _, items := range m.SubMenus {
				for _, k := range items {
					inAnySubMenu[k] = true
				}
			}
			for scope, items := range m.Scopes {
				cleaned := make([]string, 0, len(items))
				for _, k := range items {
					if !inAnySubMenu[k] {
						cleaned = append(cleaned, k)
					}
				}
				m.Scopes[scope] = cleaned
			}
			p.Settings.MenuLayouts[d.screenKey] = m
			return nil
		})
	}
}

// followRow keeps the cursor on the item it was on before a toggle/move
// changed display()'s order out from under it.
func (s *menuEditorScreen) followRow(app *App, key string) {
	for i, k := range s.display(app) {
		if k == key {
			s.row = i
			return
		}
	}
}

// openOrganise is "O": reset the working copy to the screen's built-in
// default order, apply a previously-saved template, or save the current
// working copy as a new named template — none of these save on their own,
// same as every other edit here; S still commits it.
func (s *menuEditorScreen) openOrganise(app *App) {
	if app.menuEdit == nil {
		return
	}
	d := *app.menuEdit
	templates := app.pol().Settings.MenuLayouts[d.screenKey].Templates

	var items []pickItem
	if !s.editingSubMenu(app) {
		items = append(items, pickItem{Key: "default", Label: "Reset to the default order"})
	}
	for _, name := range sortedMapKeys(templates) {
		items = append(items, pickItem{Key: "apply:" + name, Label: "Apply template: " + name})
	}
	items = append(items, pickItem{Key: "save", Label: "Save the current order as a new template"})

	startPick(app, &pickState{
		Header: "Organise",
		Prompt: "Choose",
		Items:  items,
		OnPick: func(app *App, it pickItem) {
			switch {
			case it.Key == "default":
				if def := s.def(app); def != nil {
					s.vals = append([]string(nil), def.defaultKeys...)
				}
				app.popTo(scrMenuEditor)
			case it.Key == "save":
				startPick(app, &pickState{
					Header:    "Save as template",
					Prompt:    "Name it",
					AllowFree: true,
					FreeHint:  "a short name",
					OnFree: func(app *App, text string) {
						text = strings.TrimSpace(text)
						if text == "" {
							app.setMsg("Enter a name.", true)
							return
						}
						vals := append([]string(nil), s.vals...)
						app.saveAccess("menu "+d.screenKey+" template "+text+" saved", func(p *access.Policy) error {
							if p.Settings.MenuLayouts == nil {
								p.Settings.MenuLayouts = map[string]access.ScreenMenu{}
							}
							mm := p.Settings.MenuLayouts[d.screenKey]
							if mm.Templates == nil {
								mm.Templates = map[string][]string{}
							}
							mm.Templates[text] = vals
							p.Settings.MenuLayouts[d.screenKey] = mm
							return nil
						})
						app.popTo(scrMenuEditor)
					},
				})
			default:
				name := strings.TrimPrefix(it.Key, "apply:")
				s.vals = append([]string(nil), templates[name]...)
				app.popTo(scrMenuEditor)
			}
		},
	})
}

// ---- managing a screen's sub-menus ----

func subMenuManageRows(app *App) ([]string, [][]string, []string) {
	if app.menuEdit == nil {
		return []string{"Name", "Items"}, nil, nil
	}
	m := app.pol().Settings.MenuLayouts[app.menuEdit.screenKey]
	names := sortedMapKeys(m.SubMenus)
	rows := make([][]string, len(names))
	for i, name := range names {
		rows[i] = []string{name, fmt.Sprint(len(m.SubMenus[name]))}
	}
	return []string{"Name", "Items"}, rows, names
}

func subMenuManageKeys(app *App, name string, msg tea.KeyMsg) {
	if app.menuEdit == nil {
		return
	}
	switch {
	case msg.Type == tea.KeyEnter && name != "":
		app.menuEdit.subMenu, app.menuEdit.scope = name, ""
		app.goTo(scrMenuEditor)
	case isKey(msg, 'n'):
		startPick(app, &pickState{
			Header:    "New sub-menu",
			Prompt:    "Name it",
			AllowFree: true,
			FreeHint:  "a short name, e.g. \"More\"",
			OnFree: func(app *App, text string) {
				text = strings.TrimSpace(text)
				if text == "" {
					app.setMsg("Enter a name.", true)
					return
				}
				d := *app.menuEdit
				app.saveAccess("menu "+d.screenKey+" sub-menu "+text+" created", func(p *access.Policy) error {
					if p.Settings.MenuLayouts == nil {
						p.Settings.MenuLayouts = map[string]access.ScreenMenu{}
					}
					m := p.Settings.MenuLayouts[d.screenKey]
					if m.SubMenus == nil {
						m.SubMenus = map[string][]string{}
					}
					if _, exists := m.SubMenus[text]; exists {
						return fmt.Errorf("a sub-menu named %q already exists here", text)
					}
					m.SubMenus[text] = nil
					p.Settings.MenuLayouts[d.screenKey] = m
					return nil
				})
				app.menuEdit.subMenu, app.menuEdit.scope = text, ""
				app.goTo(scrMenuEditor)
			},
		})
	case isKey(msg, 'd') && name != "":
		d := *app.menuEdit
		app.saveAccess("menu "+d.screenKey+" sub-menu "+name+" deleted", func(p *access.Policy) error {
			m := p.Settings.MenuLayouts[d.screenKey]
			for scope, items := range m.Scopes {
				for _, k := range items {
					if k == "submenu:"+name {
						return fmt.Errorf("scope %s still includes this sub-menu — remove it there first", scope)
					}
				}
			}
			delete(m.SubMenus, name)
			p.Settings.MenuLayouts[d.screenKey] = m
			return nil
		})
	case isKey(msg, 'r') && name != "":
		oldName := name
		startPick(app, &pickState{
			Header:    "Rename sub-menu " + oldName,
			Prompt:    "New name",
			AllowFree: true,
			FreeHint:  "a short name",
			OnFree: func(app *App, text string) {
				text = strings.TrimSpace(text)
				if text == "" {
					app.setMsg("Enter a name.", true)
					return
				}
				if text == oldName {
					app.onBack()
					return
				}
				d := *app.menuEdit
				app.saveAccess("menu "+d.screenKey+" sub-menu "+oldName+" renamed to "+text, func(p *access.Policy) error {
					m := p.Settings.MenuLayouts[d.screenKey]
					if _, exists := m.SubMenus[text]; exists {
						return fmt.Errorf("a sub-menu named %q already exists here", text)
					}
					items, ok := m.SubMenus[oldName]
					if !ok {
						return fmt.Errorf("sub-menu %q no longer exists", oldName)
					}
					m.SubMenus[text] = items
					delete(m.SubMenus, oldName)
					// Every scope that had this sub-menu included keeps pointing at it.
					for scope, scopeItems := range m.Scopes {
						for i, k := range scopeItems {
							if k == "submenu:"+oldName {
								scopeItems[i] = "submenu:" + text
							}
						}
						m.Scopes[scope] = scopeItems
					}
					p.Settings.MenuLayouts[d.screenKey] = m
					return nil
				})
				app.onBack()
			},
		})
	}
}

func subMenuManageScreen() screenModel {
	return &selectList{
		panelID:   "SUBMAN",
		title:     "Sub-menus",
		hint:      "Enter edit its items · N new · R rename · D delete",
		emptyHint: "No sub-menus here yet — N creates one.",
		rows:      subMenuManageRows,
		keys:      subMenuManageKeys,
	}
}
