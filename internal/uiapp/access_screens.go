package uiapp

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/auth"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/twofa"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

// Admin → Access Control: groups and their permission grid, users (groups,
// overrides, 2FA policy, sign-in limits, timeouts), the global timings, and a
// "why can / can't this user…" viewer. Every change goes through access.Update
// (validated, file-locked) and is audited with what changed.

const (
	scrAccessHub      = "access_hub"
	scrAccessGroups   = "access_groups"
	scrAccessGroupNew = "access_group_new"
	scrAccessGrid     = "access_grid"
	scrAccessUsers    = "access_users"
	scrAccessUserNew  = "access_user_new"
	scrAccessUserEdit = "access_user_edit"
	scrAccessSettings = "access_settings"
	scrAccessCheckAsk = "access_check_ask"
	scrAccessCheck    = "access_check"
)

// accessEdit is what the grid / user editor is working on.
type accessEdit struct {
	Group string // a group's grid, or
	User  string // a user's key ("source:name"): their override grid or entry form
}

func accessScreens() map[string]screenModel {
	return map[string]screenModel{
		scrAccessHub:         accessHubScreen(),
		scrAccessGroups:      &selectList{panelID: "ACCGRP", title: "Groups", rows: groupRows, keys: groupKeys, hint: "↑/↓ choose  Enter permissions  N new  C copy  R restricted  D delete"},
		scrAccessGroupNew:    accessGroupNewScreen(),
		scrAccessGrid:        &gridScreen{},
		scrMenuTabsEdit:      &menuTabsScreen{},
		scrAccessUsers:       &selectList{panelID: "ACCUSR", title: "Users", rows: userRows, keys: userKeys, hint: "↑/↓ choose  Enter edit  G permission overrides  B menu tabs  N add user  X remove entry"},
		scrAccessUserNew:     accessUserNewScreen(),
		scrAccessUserEdit:    accessUserEditScreen(),
		scrAccessSettings:    accessSettingsScreen(),
		scrAccessCheckAsk:    accessCheckAskScreen(),
		scrAccessCheck:       accessCheckScreen(),
		scrCustomMenu:        &customMenuScreen{},
		scrAccessBotLink:     accessBotLinkScreen(),
		scrAccessBotLinkEdit: accessBotLinkEditScreen(),
		scrAccessBotReset:    accessBotResetScreen(),
	}
}

func accessHubScreen() screenModel {
	return &menuScreen{
		panelID:      "ACCESS",
		title:        "Access Control",
		adminGated:   true,
		deniedAction: "ACCESS_CONTROL",
		options: func(app *App) []menuOption {
			return []menuOption{
				{Key: "1", Label: "Groups and their permissions", Go: func(app *App) { app.goTo(scrAccessGroups) }},
				{Key: "2", Label: "Users: groups, 2FA exemption, sign-in limits, timeouts", Go: func(app *App) { app.goTo(scrAccessUsers) }},
				{Key: "3", Label: "Security settings: 2FA window, idle, session, exports", Go: func(app *App) { app.goTo(scrAccessSettings) }},
				{Key: "4", Label: "Check a user's effective permissions", Go: func(app *App) { app.goTo(scrAccessCheckAsk) }},
				{Key: "5", Label: "Notifications: channels and where each alert goes", Go: func(app *App) { app.goTo(scrNotify) }},
				{Key: "6", Label: "Customize the main menu (everyone's layout)", Go: func(app *App) { app.goTo(scrCustomMenu) }},
				{Key: "7", Label: "My remote bot link (Discord/Slack, PIN, security Q&A)", Go: func(app *App) { app.goTo(scrAccessBotLink) }},
				{Key: "0", Label: "Return", Go: func(app *App) { app.onBack() }},
			}
		},
		intro: func(app *App) string {
			p, err := access.Load()
			if err != nil {
				return app.theme.Danger.Render(" Policy unreadable: " + err.Error())
			}
			return app.theme.Muted.Render(fmt.Sprintf(" %d groups, %d users governed; others keep their ModernWMS role.", len(p.Groups), len(p.Users)))
		},
	}
}

// saveAccess applies a change, refusing one that would take access.manage away
// from the admin making it, then reloads this session's policy and audits.
func (app *App) saveAccess(what string, change func(p *access.Policy) error) bool {
	me := ""
	if app.session != nil {
		me = access.Key(app.session.Source, app.session.Username)
	}
	_, err := access.Update(func(p *access.Policy) error {
		if err := change(p); err != nil {
			return err
		}
		if _, governed := p.Users[me]; governed {
			src, name, _ := strings.Cut(me, ":")
			if !p.Effective(src, name).Can("access.manage") {
				return fmt.Errorf("that would remove your own access.manage permission and lock you out of this panel")
			}
		}
		return nil
	})
	if err != nil {
		app.setMsg("Not saved: "+err.Error(), true)
		return false
	}
	user, role := "", ""
	if app.session != nil {
		user, role = app.session.Username, app.session.Role
	}
	app.audit.Log(user, role, "ACCESS_CHANGED", "SUCCESS", what)
	app.loadPolicy()
	app.setMsg("Saved: "+what, false)
	return true
}

// ---- a selectable list ----

type selectList struct {
	base
	panelID, title, hint string
	emptyHint            string // shown instead of hint when there are no rows yet; "" keeps hint
	rows                 func(app *App) (header []string, rows [][]string, keys []string)
	keys                 func(app *App, key string, msg tea.KeyMsg)
	sel                  int
}

func (s *selectList) PanelID() string  { return s.panelID }
func (s *selectList) Title() string    { return s.title }
func (s *selectList) OnEnter(app *App) { s.sel = 0; app.loadPolicy() }
func (s *selectList) FKeys() [][2]string {
	return [][2]string{{"F3", "Exit"}, {"F12", "Cancel"}}
}

func (s *selectList) Body(app *App) string {
	t := app.theme
	header, rows, _ := s.rows(app)
	s.sel = min(s.sel, max(0, len(rows)-1))
	marked := make([][]string, len(rows))
	for i, r := range rows {
		m := "  "
		if i == s.sel {
			m = "▶ "
		}
		marked[i] = append([]string{m}, r...)
	}
	title := fmt.Sprintf("%d %s", len(rows), strings.ToLower(s.title))
	hint := s.hint
	if len(rows) == 0 {
		title = "none yet"
		if s.emptyHint != "" {
			hint = s.emptyHint
		}
	}
	cols := append([]string{""}, header...)
	body := ui.RenderColumns(t, cols, marked, title)
	return body + "\n" + t.Muted.Render(ansi.Truncate(hint, t.W(), "…"))
}

func (s *selectList) HandleKey(app *App, msg tea.KeyMsg) {
	_, rows, keys := s.rows(app)
	switch {
	case msg.Type == tea.KeyUp && s.sel > 0:
		s.sel--
		return
	case msg.Type == tea.KeyDown && s.sel < len(rows)-1:
		s.sel++
		return
	}
	key := ""
	if s.sel < len(keys) {
		key = keys[s.sel]
	}
	s.keys(app, key, msg)
}

// ---- groups ----

func groupRows(app *App) ([]string, [][]string, []string) {
	p := app.pol()
	names := sortedMapKeys(p.Groups)
	rows := make([][]string, len(names))
	for i, n := range names {
		g := p.Groups[n]
		allow, deny := 0, 0
		for _, v := range g.Perms {
			if v == access.Allow {
				allow++
			} else {
				deny++
			}
		}
		r := ""
		if g.Restricted {
			r = "no-2FA ok"
		}
		rows[i] = []string{n, fmt.Sprint(allow), fmt.Sprint(deny), fmt.Sprint(len(membersOf(p, n))), r, g.Description}
	}
	return []string{"Group", "Allow", "Deny", "Users", "Restricted", "Description"}, rows, names
}

func groupKeys(app *App, name string, msg tea.KeyMsg) {
	switch {
	case msg.Type == tea.KeyEnter && name != "":
		app.accessEdit = &accessEdit{Group: name}
		app.goTo(scrAccessGrid)
	case isKey(msg, 'n'):
		app.accessEdit = &accessEdit{}
		app.goTo(scrAccessGroupNew)
	case isKey(msg, 'c') && name != "":
		app.accessEdit = &accessEdit{Group: name}
		app.goTo(scrAccessGroupNew)
	case isKey(msg, 'r') && name != "":
		app.saveAccess("group "+name+" restricted toggled", func(p *access.Policy) error {
			p.Groups[name].Restricted = !p.Groups[name].Restricted
			return nil
		})
	case isKey(msg, 'd') && name != "":
		app.saveAccess("group "+name+" deleted", func(p *access.Policy) error {
			if m := membersOf(p, name); len(m) > 0 {
				return fmt.Errorf("group %s still has %d user(s): %s", name, len(m), strings.Join(m, ", "))
			}
			delete(p.Groups, name)
			return nil
		})
	}
}

func accessGroupNewScreen() screenModel {
	return &formScreen{
		panelID: "ACCGNW",
		title:   "New Group",
		build: func(app *App) []ui.Field {
			from := ""
			if app.accessEdit != nil {
				from = app.accessEdit.Group
			}
			return []ui.Field{{Label: "Group name (letters, digits, -)"}, {Label: "Description"}, {Label: "Copy permissions from (blank = none)", Value: from}}
		},
		submit: func(app *App, v []string) {
			name := strings.ToLower(strings.TrimSpace(v[0]))
			if name == "" || strings.ContainsAny(name, " :,") {
				app.setMsg("Give the group a name without spaces, commas or colons.", true)
				return
			}
			from := strings.TrimSpace(v[2])
			if app.saveAccess("group "+name+" created"+map[bool]string{true: " from " + from, false: ""}[from != ""], func(p *access.Policy) error {
				if p.Groups[name] != nil {
					return fmt.Errorf("group %s already exists", name)
				}
				g := &access.Group{Description: strings.TrimSpace(v[1]), Perms: map[string]string{}}
				if from != "" {
					src := p.Groups[from]
					if src == nil {
						return fmt.Errorf("no group %q to copy", from)
					}
					for k, val := range src.Perms {
						g.Perms[k] = val
					}
					g.Restricted = src.Restricted
				}
				p.Groups[name] = g
				return nil
			}) {
				app.accessEdit = &accessEdit{Group: name}
				app.back()
				app.goTo(scrAccessGrid)
			}
		},
	}
}

// ---- the permission grid (a group's perms, or a user's overrides) ----

type gridScreen struct {
	base
	row, col int
	vals     map[string]string // the working copy
	loaded   *accessEdit
}

func (s *gridScreen) PanelID() string { return "ACCGRD" }
func (s *gridScreen) Title() string   { return "Permissions" }
func (s *gridScreen) FKeys() [][2]string {
	return [][2]string{{"F3", "Exit"}, {"F12", "Cancel"}}
}

func (s *gridScreen) OnEnter(app *App) {
	app.loadPolicy()
	s.row, s.col, s.vals, s.loaded = 0, 0, map[string]string{}, app.accessEdit
	p := app.pol()
	var src map[string]string
	switch e := app.accessEdit; {
	case e == nil:
	case e.Group != "" && p.Groups[e.Group] != nil:
		src = p.Groups[e.Group].Perms
	case e.User != "" && p.Users[e.User] != nil:
		src = p.Users[e.User].Perms
	}
	for k, v := range src {
		s.vals[k] = v
	}
	s.snap()
}

// snap keeps the cursor on a column the row actually has.
func (s *gridScreen) snap() {
	acts := access.Areas[s.row].Actions
	s.col = min(s.col, len(acts)-1)
}

func (s *gridScreen) perm() string {
	a := access.Areas[s.row]
	return a.Name + "." + a.Actions[s.col]
}

func (s *gridScreen) Body(app *App) string {
	t := app.theme
	e := app.accessEdit
	if e == nil {
		return t.Muted.Render("Nothing to edit.")
	}
	var b strings.Builder
	what := "Group " + e.Group
	if e.User != "" {
		what = "Overrides for " + e.User + " (· = the groups decide)"
	} else if g := app.pol().Groups[e.Group]; g != nil && g.Restricted {
		what += "  [restricted: usable by 2FA-exempt accounts]"
	}
	b.WriteString(t.Strong.Render(what) + "\n")
	for r, a := range access.Areas {
		b.WriteString(t.Text.Render(fmt.Sprintf("%-16s", a.Label)))
		for c, act := range a.Actions {
			mark, style := "·", t.Muted
			switch s.vals[a.Name+"."+act] {
			case access.Allow:
				mark, style = "✓", t.Success
				if t.Labels {
					mark = "✓Y"
				}
			case access.Deny:
				mark, style = "✗", t.Danger
				if t.Labels {
					mark = "✗N"
				}
			}
			cell := fmt.Sprintf("%s %-8s", mark, act)
			if r == s.row && c == s.col {
				b.WriteString(t.TitleReverse.Render(cell) + " ")
			} else {
				b.WriteString(style.Render(cell) + " ")
			}
		}
		b.WriteString("\n")
	}
	keys := "arrows · Space allow/deny/inherit · S save · Esc"
	if e.Group != "" {
		keys += " · R restricted"
	}
	b.WriteString(t.Muted.Render(ansi.Truncate(s.perm()+": "+cellMeaning(s.vals[s.perm()], e)+"   "+keys, t.W(), "…")))
	return b.String()
}

func cellMeaning(v string, e *accessEdit) string {
	switch v {
	case access.Allow:
		return "allowed"
	case access.Deny:
		return "denied (beats any allow)"
	}
	if e.User != "" {
		return "inherited from the user's groups"
	}
	return "not granted by this group"
}

func (s *gridScreen) HandleKey(app *App, msg tea.KeyMsg) {
	e := app.accessEdit
	if e == nil {
		return
	}
	switch {
	case msg.Type == tea.KeyUp && s.row > 0:
		s.row--
		s.snap()
	case msg.Type == tea.KeyDown && s.row < len(access.Areas)-1:
		s.row++
		s.snap()
	case msg.Type == tea.KeyLeft && s.col > 0:
		s.col--
	case msg.Type == tea.KeyRight && s.col < len(access.Areas[s.row].Actions)-1:
		s.col++
	case msg.Type == tea.KeySpace || isKey(msg, ' '):
		p := s.perm()
		s.vals[p] = map[string]string{"": access.Allow, access.Allow: access.Deny, access.Deny: ""}[s.vals[p]]
		if s.vals[p] == "" {
			delete(s.vals, p)
		}
	case isKey(msg, 'r') && e.Group != "":
		app.saveAccess("group "+e.Group+" restricted toggled", func(p *access.Policy) error {
			p.Groups[e.Group].Restricted = !p.Groups[e.Group].Restricted
			return nil
		})
	case isKey(msg, 's'):
		vals := s.vals
		if e.Group != "" {
			before := app.pol().Groups[e.Group]
			if app.saveAccess("group "+e.Group+": "+permDiff(before.Perms, vals), func(p *access.Policy) error {
				p.Groups[e.Group].Perms = copyMap(vals)
				return nil
			}) {
				app.onBack()
			}
			return
		}
		var old map[string]string
		if u := app.pol().Users[e.User]; u != nil {
			old = u.Perms
		}
		if app.saveAccess("user "+e.User+" overrides: "+permDiff(old, vals), func(p *access.Policy) error {
			u := p.Users[e.User]
			if u == nil {
				u = &access.User{}
				p.Users[e.User] = u
			}
			u.Perms = copyMap(vals)
			return nil
		}) {
			app.onBack()
		}
	}
}

// ---- menu tabs: which top-level hub tabs a user sees ----
//
// Generalizes what used to be a single "LEGO/Part-DB browse" toggle to every
// hub tab that can meaningfully be toggled (see hub.go's hubOptions and
// access.go's modulePerm/screenPerm) — Overview is left out entirely since
// it needs no permission at all (modulePerm["dashboard"] is "", meaning
// "anyone signed in"). Each row grants or denies its Perms together, as an
// explicit override either direction (never delete-to-inherit: a group like
// checker/picker can already grant several of these by default, so clearing
// an override alone would silently fall back to the group's "allow" instead
// of actually turning it off). Operations only covers its four view-level
// items (ops.view/ops.asn/stock.view), deliberately excluding Warehouse
// Ops' write permission (ops.edit) — same "toggle = browse, grid = write"
// split LEGO/Part-DB already had. Admin is the one exception: this screen
// can only ever turn it off, never grant it — real admin capability stays a
// deliberate decision made through the permission grid (G) or Groups, not a
// single checkbox; this is also what the checklist's on/off state reflects
// with OR instead of AND (hub.go's own anyModuleAllowed is an OR across
// these four — any one of them already shows "9=Admin").
type menuTab struct {
	Key, Label string
	Perms      []string
	DenyOnly   bool
}

var menuTabs = []menuTab{
	{Key: "partdb", Label: "Part-DB Hub", Perms: []string{"partdb.view"}},
	{Key: "ops", Label: "Operations (browse)", Perms: []string{"ops.view", "ops.asn", "stock.view"}},
	{Key: "scripts", Label: "Script Hub", Perms: []string{"scripts.run"}},
	{Key: "lego", Label: "LEGO Collection", Perms: []string{"lego.view", "lego.search"}},
	{Key: "admin", Label: "Admin", Perms: []string{"users.view", "settings.view", "containers.view", "access.manage"}, DenyOnly: true},
}

// granted checks the user's EFFECTIVE permission (group membership included
// — "checker"/"picker" already grant several of these by default), not just
// their own override map.
func (t menuTab) granted(p *access.Policy, key string) bool {
	source, username, ok := strings.Cut(key, ":")
	if !ok {
		return false
	}
	eff := p.Effective(source, username)
	if t.DenyOnly {
		for _, perm := range t.Perms {
			if eff.Can(perm) {
				return true
			}
		}
		return false
	}
	for _, perm := range t.Perms {
		if !eff.Can(perm) {
			return false
		}
	}
	return true
}

const scrMenuTabsEdit = "menu_tabs_edit"

type menuTabsScreen struct {
	base
	row  int
	vals map[string]bool // tab key -> desired on/off (working copy)
	user string
}

func (s *menuTabsScreen) PanelID() string { return "MENTAB" }
func (s *menuTabsScreen) Title() string   { return "Menu Tabs" }
func (s *menuTabsScreen) FKeys() [][2]string {
	return [][2]string{{"F3", "Exit"}, {"F12", "Cancel"}}
}

func (s *menuTabsScreen) OnEnter(app *App) {
	app.loadPolicy()
	s.row, s.vals, s.user = 0, map[string]bool{}, ""
	e := app.accessEdit
	if e == nil || e.User == "" {
		return
	}
	s.user = e.User
	p := app.pol()
	for _, t := range menuTabs {
		s.vals[t.Key] = t.granted(p, e.User)
	}
}

func (s *menuTabsScreen) Body(app *App) string {
	t := app.theme
	if s.user == "" {
		return t.Muted.Render("Nothing to edit.")
	}
	var b strings.Builder
	b.WriteString(t.Strong.Render("Menu tabs for "+s.user) + "\n\n")
	for i, tab := range menuTabs {
		mark, style := "off", t.Muted
		if s.vals[tab.Key] {
			mark, style = "on ", t.Success
		}
		cell := fmt.Sprintf("%-20s %s", tab.Label, mark)
		if tab.DenyOnly {
			cell += "  (off only here — grant via G)"
		}
		if i == s.row {
			b.WriteString(t.TitleReverse.Render(cell) + "\n")
		} else {
			b.WriteString(style.Render(cell) + "\n")
		}
	}
	b.WriteString("\n" + t.Muted.Render("↑/↓ choose · Space toggle · S save · Esc"))
	return b.String()
}

func (s *menuTabsScreen) HandleKey(app *App, msg tea.KeyMsg) {
	if s.user == "" {
		return
	}
	switch {
	case msg.Type == tea.KeyUp && s.row > 0:
		s.row--
	case msg.Type == tea.KeyDown && s.row < len(menuTabs)-1:
		s.row++
	case msg.Type == tea.KeySpace || isKey(msg, ' '):
		tab := menuTabs[s.row]
		if tab.DenyOnly && !s.vals[tab.Key] {
			app.setMsg("Admin can only be turned off here — use G to grant specific admin permissions.", true)
			return
		}
		s.vals[tab.Key] = !s.vals[tab.Key]
	case isKey(msg, 's'):
		key, vals := s.user, s.vals
		if app.saveAccess("user "+key+" menu tabs updated", func(p *access.Policy) error {
			u := p.Users[key]
			if u == nil {
				u = &access.User{}
				p.Users[key] = u
			}
			if u.Perms == nil {
				u.Perms = map[string]string{}
			}
			for _, tab := range menuTabs {
				want := access.Deny
				if vals[tab.Key] {
					want = access.Allow
				}
				for _, perm := range tab.Perms {
					u.Perms[perm] = want
				}
			}
			return nil
		}) {
			app.onBack()
		}
	}
}

// permDiff is "lego.edit inherit→allow, ops.* …" for the audit log.
func permDiff(before, after map[string]string) string {
	var out []string
	name := func(v string) string {
		if v == "" {
			return "inherit"
		}
		return v
	}
	for _, p := range access.All() {
		if before[p] != after[p] {
			out = append(out, fmt.Sprintf("%s %s→%s", p, name(before[p]), name(after[p])))
		}
	}
	if len(out) == 0 {
		return "no change"
	}
	return strings.Join(out, ", ")
}

func copyMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// ---- users ----

func userRows(app *App) ([]string, [][]string, []string) {
	p := app.pol()
	keys := sortedMapKeys(p.Users)
	rows := make([][]string, len(keys))
	for i, k := range keys {
		u := p.Users[k]
		twofa := map[string]string{"": "default", "required": "required", "exempt": "EXEMPT"}[u.TwoFA]
		if u.TwoFA == access.TwoFAExempt && len(u.ExemptCIDRs) > 0 {
			twofa += " (" + strings.Join(u.ExemptCIDRs, ",") + ")"
		}
		ch := "all"
		if len(u.Channels) > 0 {
			ch = strings.Join(u.Channels, ",")
		}
		on := 0
		for _, t := range menuTabs {
			if t.granted(p, k) {
				on++
			}
		}
		rows[i] = []string{k, orDash(strings.Join(u.Groups, ",")), twofa, ch, orDash(u.Expires), fmt.Sprintf("%d/%d", on, len(menuTabs))}
	}
	return []string{"User", "Groups", "2FA", "Channels", "Expires", "Menu"}, rows, keys
}

func userKeys(app *App, key string, msg tea.KeyMsg) {
	switch {
	case msg.Type == tea.KeyEnter && key != "":
		app.accessEdit = &accessEdit{User: key}
		app.goTo(scrAccessUserEdit)
	case isKey(msg, 'g') && key != "":
		app.accessEdit = &accessEdit{User: key}
		app.goTo(scrAccessGrid)
	case isKey(msg, 'b') && key != "":
		app.accessEdit = &accessEdit{User: key}
		app.goTo(scrMenuTabsEdit)
	case isKey(msg, 'n'):
		app.goTo(scrAccessUserNew)
	case isKey(msg, 'x') && key != "":
		app.saveAccess("user "+key+" entry removed (back to their ModernWMS role)", func(p *access.Policy) error {
			delete(p.Users, key)
			return nil
		})
	}
}

func accessUserNewScreen() screenModel {
	return &formScreen{
		panelID: "ACCUNW",
		title:   "Add a User to the Policy",
		build: func(app *App) []ui.Field {
			return []ui.Field{{Label: "Username (as they sign in)"}, {Label: "Account source (modernwms or partdb)", Value: "partdb", Fresh: true}, {Label: "Groups (comma-separated)", Value: "viewer", Fresh: true}}
		},
		preamble: func(app *App) string {
			return app.theme.Muted.Render("Groups: " + strings.Join(sortedMapKeys(app.pol().Groups), ", ") + "\nThe user's entry replaces their ModernWMS-role access with the groups' permissions.")
		},
		submit: func(app *App, v []string) {
			name, src := strings.TrimSpace(v[0]), strings.ToLower(strings.TrimSpace(v[1]))
			if name == "" || (src != "modernwms" && src != "partdb") {
				app.setMsg("Enter a username, and modernwms or partdb as the source.", true)
				return
			}
			key := access.Key(src, name)
			groups := splitCSV(v[2])
			if app.saveAccess("user "+key+" added, groups "+strings.Join(groups, ","), func(p *access.Policy) error {
				if p.Users[key] != nil {
					return fmt.Errorf("%s already has an entry", key)
				}
				p.Users[key] = &access.User{Groups: groups}
				return nil
			}) {
				app.accessEdit = &accessEdit{User: key}
				app.back()
				app.goTo(scrAccessUserEdit)
			}
		},
	}
}

const (
	ufGroups = iota
	uf2FA
	ufCIDRs
	ufChannels
	ufExpires
	ufGrace
	ufIdle
	ufMax
	ufNote
)

func accessUserEditScreen() screenModel {
	return &formScreen{
		panelID: "ACCUED",
		title:   "Edit User Access",
		build: func(app *App) []ui.Field {
			u := &access.User{}
			if e := app.accessEdit; e != nil && app.pol().Users[e.User] != nil {
				u = app.pol().Users[e.User]
			}
			twofa := u.TwoFA
			if twofa == "" {
				twofa = "default"
			}
			ch := strings.Join(u.Channels, ",")
			if ch == "" {
				ch = "all"
			}
			return []ui.Field{
				{Label: "Groups (comma-separated)", Value: strings.Join(u.Groups, ",")},
				{Label: "2FA: default, required or exempt", Value: twofa},
				{Label: "Exempt only from networks (e.g. 192.168.1.0/24)", Value: strings.Join(u.ExemptCIDRs, ",")},
				{Label: "Channels: telnet,web,local or all", Value: ch},
				{Label: "Access ends on (YYYY-MM-DD, blank = never)", Value: u.Expires},
				{Label: "2FA remember minutes (blank = global)", Value: ptrStr(u.GraceMin)},
				{Label: "Idle lock minutes (blank = global)", Value: ptrStr(u.IdleMin)},
				{Label: "Max session hours (blank = global)", Value: ptrStr(u.MaxHours)},
				{Label: "Note", Value: u.Note},
			}
		},
		preamble: func(app *App) string {
			if app.accessEdit == nil {
				return ""
			}
			return app.theme.Strong.Render(app.accessEdit.User) + app.theme.Muted.Render("  — Tab moves, Enter on the last field saves. G on the Users list edits permission overrides.")
		},
		submit: func(app *App, v []string) {
			e := app.accessEdit
			if e == nil {
				return
			}
			twofa := strings.ToLower(strings.TrimSpace(v[uf2FA]))
			if twofa == "default" {
				twofa = ""
			}
			ch := splitCSV(v[ufChannels])
			if slices.Contains(ch, "all") {
				ch = nil
			}
			var nums [3]*int
			for i, f := range []int{ufGrace, ufIdle, ufMax} {
				s := strings.TrimSpace(v[f])
				if s == "" {
					continue
				}
				n, err := strconv.Atoi(s)
				if err != nil || n < 0 {
					app.setMsg(fmt.Sprintf("%q is not a number of minutes/hours (leave blank for the global value).", s), true)
					return
				}
				nums[i] = access.Int(n)
			}
			var before []byte
			if u := app.pol().Users[e.User]; u != nil {
				before, _ = json.Marshal(u)
			}
			next := &access.User{Groups: splitCSV(v[ufGroups]), TwoFA: twofa, ExemptCIDRs: splitCSV(v[ufCIDRs]), Channels: ch,
				Expires: strings.TrimSpace(v[ufExpires]), GraceMin: nums[0], IdleMin: nums[1], MaxHours: nums[2], Note: strings.TrimSpace(v[ufNote])}
			if twofa != access.TwoFAExempt {
				next.ExemptCIDRs = nil
			}
			if u := app.pol().Users[e.User]; u != nil {
				next.Perms = u.Perms           // overrides are edited on the grid
				next.BadgeToken = u.BadgeToken // badges are issued from `wms access user badge`, not this form
				// The remote-bot link/PIN/security-Q&A fields are edited on their own
				// screen (accessBotLinkScreen), never this one — carry them forward
				// or this form silently wipes them, same bug class BadgeToken once hit.
				next.DiscordID = u.DiscordID
				next.SlackID = u.SlackID
				next.BotPINHash = u.BotPINHash
				next.SecurityQuestion = u.SecurityQuestion
				next.SecurityAnswerHash = u.SecurityAnswerHash
				next.BotPINFails = u.BotPINFails
				next.BotPINLockUntil = u.BotPINLockUntil
			}
			after, _ := json.Marshal(next)
			if app.saveAccess("user "+e.User+": "+string(before)+" → "+string(after), func(p *access.Policy) error {
				p.Users[e.User] = next
				return nil
			}) {
				app.onBack()
			}
		},
	}
}

// ---- remote-bot link: Discord/Slack ID, bot PIN, security Q&A ----
//
// This is the credential internal/botapi checks on every webhook-driven
// admin action — deliberately separate from the admin's real WMS login, so a
// PIN typed into a chat app and intercepted never doubles as account
// takeover. Always the acting admin's own record; one admin never sets this
// up for another.

const (
	scrAccessBotLink     = "access_bot_link"
	scrAccessBotLinkEdit = "access_bot_link_edit"
	scrAccessBotReset    = "access_bot_reset"
)

func accessBotLinkScreen() screenModel {
	return &menuScreen{
		panelID:      "BOTLNK",
		title:        "My Remote Bot Link",
		adminGated:   true,
		deniedAction: "ACCESS_CONTROL",
		options: func(app *App) []menuOption {
			return []menuOption{
				{Key: "1", Label: "Set up / change my link, PIN, security question", Go: func(app *App) { app.goTo(scrAccessBotLinkEdit) }},
				{Key: "2", Label: "Reset my bot PIN (needs a 2FA code and the security answer)", Go: func(app *App) { app.goTo(scrAccessBotReset) }},
				{Key: "0", Label: "Return", Go: func(app *App) { app.onBack() }},
			}
		},
		intro: func(app *App) string {
			if app.session == nil {
				return ""
			}
			u := app.pol().Users[access.Key(app.session.Source, app.session.Username)]
			if u == nil || u.BotPINHash == "" {
				return app.theme.Muted.Render("No bot PIN set yet — the bot can't act for you until you set one up.")
			}
			linked := u.DiscordID != "" || u.SlackID != ""
			status := "linked"
			if !linked {
				status = "no Discord/Slack ID linked yet"
			}
			return app.theme.Muted.Render("Bot PIN is set; " + status + ".")
		},
	}
}

func accessBotLinkEditScreen() screenModel {
	return &formScreen{
		panelID: "BOTLNKE",
		title:   "My Remote Bot Link",
		preamble: func(app *App) string {
			hint := "Blank PIN/answer = leave it unchanged. Both PIN fields and both answer fields must match to change them."
			if !app.governed() {
				hint = "You need a group of your own in Access Control → Users first (even just \"admin\") " +
					"before linking a bot credential — otherwise saving this would leave you with no policy-level " +
					"permissions at all. " + hint
			}
			return app.theme.Muted.Render(hint)
		},
		build: func(app *App) []ui.Field {
			u := &access.User{}
			if app.session != nil {
				if existing := app.pol().Users[access.Key(app.session.Source, app.session.Username)]; existing != nil {
					u = existing
				}
			}
			return []ui.Field{
				{Label: "Discord user ID", Value: u.DiscordID},
				{Label: "Slack user ID", Value: u.SlackID},
				{Label: "New bot PIN (blank = unchanged)", Password: true},
				{Label: "Confirm new bot PIN", Password: true},
				{Label: "Security question (blank = unchanged)", Value: u.SecurityQuestion},
				{Label: "New security answer (blank = unchanged)", Password: true},
			}
		},
		submit: func(app *App, v []string) {
			if app.session == nil {
				app.onBack()
				return
			}
			// A bare Users[] entry with no Groups would govern this admin with
			// zero policy permissions, instantly losing every legacy-role fallback
			// (see app.can) the moment it's created — refuse rather than silently
			// lock the admin out of their own account to store a bot credential.
			if !app.governed() {
				app.setMsg("Give your account a group in Access Control → Users first, then come back to link your bot credential.", true)
				return
			}
			discordID, slackID := strings.TrimSpace(v[0]), strings.TrimSpace(v[1])
			pin, confirmPIN := v[2], v[3]
			question := strings.TrimSpace(v[4])
			answer := v[5]
			if pin != confirmPIN {
				app.setMsg("The two PIN fields don't match.", true)
				return
			}
			var pinHash, answerHash string
			if pin != "" {
				h, err := auth.HashPartDB(pin)
				if err != nil {
					app.setMsg(err.Error(), true)
					return
				}
				pinHash = h
			}
			if answer != "" {
				h, err := auth.HashPartDB(strings.ToLower(strings.TrimSpace(answer)))
				if err != nil {
					app.setMsg(err.Error(), true)
					return
				}
				answerHash = h
			}
			key := access.Key(app.session.Source, app.session.Username)
			if app.saveAccess("bot link updated for "+key, func(p *access.Policy) error {
				u := p.Users[key]
				if u == nil {
					u = &access.User{}
					p.Users[key] = u
				}
				u.DiscordID, u.SlackID = discordID, slackID
				if pinHash != "" {
					u.BotPINHash, u.BotPINFails, u.BotPINLockUntil = pinHash, 0, ""
				}
				if question != "" {
					u.SecurityQuestion = question
				}
				if answerHash != "" {
					u.SecurityAnswerHash = answerHash
				}
				return nil
			}) {
				app.onBack()
			}
		},
	}
}

func accessBotResetScreen() screenModel {
	return &formScreen{
		panelID: "BOTRST",
		title:   "Reset My Bot PIN",
		preamble: func(app *App) string {
			return app.theme.Muted.Render("Needs a live 2FA code AND the correct security answer — either alone is refused.")
		},
		build: func(app *App) []ui.Field {
			return []ui.Field{
				{Label: "2FA code"},
				{Label: "Security answer", Password: true},
				{Label: "New bot PIN", Password: true},
				{Label: "Confirm new bot PIN", Password: true},
			}
		},
		submit: func(app *App, v []string) {
			if app.session == nil {
				app.onBack()
				return
			}
			code, answer, pin, confirmPIN := strings.TrimSpace(v[0]), v[1], v[2], v[3]
			if pin == "" || pin != confirmPIN {
				app.setMsg("Enter a new PIN, and confirm it — both fields must match.", true)
				return
			}
			key := access.Key(app.session.Source, app.session.Username)
			u := app.pol().Users[key]
			if u == nil || u.SecurityAnswerHash == "" {
				app.setMsg("No security question is set up yet — set one up first.", true)
				return
			}
			if !auth.VerifyPartDB(strings.ToLower(strings.TrimSpace(answer)), u.SecurityAnswerHash) {
				app.setMsg("That doesn't match the security answer on file.", true)
				return
			}
			if _, err := twofa.Verify(app.session.Username, app.session.Source, code); err != nil {
				app.setMsg("2FA check failed: "+err.Error(), true)
				return
			}
			newHash, err := auth.HashPartDB(pin)
			if err != nil {
				app.setMsg(err.Error(), true)
				return
			}
			if app.saveAccess("bot PIN reset for "+key, func(p *access.Policy) error {
				u := p.Users[key]
				if u == nil {
					u = &access.User{}
					p.Users[key] = u
				}
				u.BotPINHash, u.BotPINFails, u.BotPINLockUntil = newHash, 0, ""
				return nil
			}) {
				app.setMsg("Bot PIN reset.", false)
				app.stack = nil
				app.cur = scrAccessHub
			}
		},
	}
}

// ---- security settings ----

func accessSettingsScreen() screenModel {
	return &formScreen{
		panelID: "ACCSET",
		title:   "Security Settings",
		build: func(app *App) []ui.Field {
			p := app.pol()
			return []ui.Field{
				{Label: "2FA remember window, minutes (0 = always ask)", Value: strconv.Itoa(p.GraceMinutes(nil, configMinutes(config.TwoFAGraceMinutes)))},
				{Label: "Idle lock, minutes (0 = never)", Value: strconv.Itoa(p.IdleMinutes(nil, configMinutes(config.IdleLockMinutes)))},
				{Label: "Maximum session, hours (0 = no limit)", Value: strconv.Itoa(p.MaxSessionHours(nil))},
				{Label: "Keep exports for, days (1-365)", Value: strconv.Itoa(p.ExportDays())},
				{Label: "Download links work for, minutes (1-1440)", Value: strconv.Itoa(p.LinkMinutes())},
				{Label: "Show collection stats before sign-in (yes / no)", Value: map[bool]string{true: "yes", false: "no"}[publicStats()]},
				{Label: "Message of the day on the sign-on screen", Value: motd()},
			}
		},
		preamble: func(app *App) string {
			return app.theme.Muted.Render("Everyone's defaults; a user's own values (Users → Enter) win over these.\nExports live in " + config.Get(config.ExportDir) + ".")
		},
		submit: func(app *App, v []string) {
			pub := strings.ToLower(strings.TrimSpace(v[5]))
			if pub != "yes" && pub != "no" {
				app.setMsg("Show collection stats: yes or no.", true)
				return
			}
			var n [5]int
			for i := range n {
				x, err := strconv.Atoi(strings.TrimSpace(v[i]))
				if err != nil {
					app.setMsg(fmt.Sprintf("%q is not a whole number.", v[i]), true)
					return
				}
				n[i] = x
			}
			before := app.pol().Settings
			b, _ := json.Marshal(before)
			if app.saveAccess(fmt.Sprintf("settings %s → grace=%d idle=%d max=%d export_days=%d link=%d", b, n[0], n[1], n[2], n[3], n[4]), func(p *access.Policy) error {
				p.Settings = access.Settings{GraceMin: access.Int(n[0]), IdleMin: access.Int(n[1]), MaxHours: access.Int(n[2]), ExportDays: access.Int(n[3]), LinkMinutes: access.Int(n[4])}
				return nil
			}) {
				_ = config.SetOverride(config.SignOnPublicStats, map[string]string{"yes": "1", "no": "0"}[pub])
				_ = config.SetOverride(config.MOTD, strings.TrimSpace(v[6]))
				app.onBack()
			}
		},
	}
}

// ---- effective permissions ----

func accessCheckAskScreen() screenModel {
	return &formScreen{
		panelID: "ACCCHK",
		title:   "Check Permissions",
		build: func(app *App) []ui.Field {
			return []ui.Field{{Label: "User (name, or source:name)"}}
		},
		submit: func(app *App, v []string) {
			key, ok := app.pol().FindKey(v[0])
			if !ok {
				app.setMsg(strings.TrimSpace(v[0])+" has no policy entry: their ModernWMS role decides what they can do.", false)
				return
			}
			app.accessEdit = &accessEdit{User: key}
			app.goTo(scrAccessCheck)
		},
	}
}

func accessCheckScreen() screenModel {
	return &tableScreen{
		panelID: "ACCEFF",
		title:   "Effective Permissions",
		columns: []string{"Permission", "Result", "Why"},
		fetch: func(app *App) ([][]string, string, error) {
			e := app.accessEdit
			if e == nil {
				return nil, "", nil
			}
			src, name, _ := strings.Cut(e.User, ":")
			eff := app.pol().Effective(src, name)
			var rows [][]string
			for _, p := range access.All() {
				res := "no"
				if eff.Can(p) {
					res = "YES"
				}
				rows = append(rows, []string{p, res, eff.Why(p)})
			}
			return rows, e.User + " — / filter, PgUp/PgDn", nil
		},
	}
}

// ---- helpers ----

func sortedMapKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func membersOf(p *access.Policy, group string) []string {
	var out []string
	for _, k := range sortedMapKeys(p.Users) {
		if slices.Contains(p.Users[k].Groups, group) {
			out = append(out, k)
		}
	}
	return out
}

func splitCSV(s string) []string {
	var out []string
	for _, f := range strings.Split(s, ",") {
		if f = strings.ToLower(strings.TrimSpace(f)); f != "" {
			out = append(out, f)
		}
	}
	return out
}

func ptrStr(p *int) string {
	if p == nil {
		return ""
	}
	return strconv.Itoa(*p)
}
