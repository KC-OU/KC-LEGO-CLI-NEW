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
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
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
		scrAccessHub:      accessHubScreen(),
		scrAccessGroups:   &selectList{panelID: "ACCGRP", title: "Groups", rows: groupRows, keys: groupKeys, hint: "↑/↓ choose  Enter permissions  N new  C copy  R restricted  D delete"},
		scrAccessGroupNew: accessGroupNewScreen(),
		scrAccessGrid:     &gridScreen{},
		scrAccessUsers:    &selectList{panelID: "ACCUSR", title: "Users", rows: userRows, keys: userKeys, hint: "↑/↓ choose  Enter edit  G permission overrides  N add user  X remove entry"},
		scrAccessUserNew:  accessUserNewScreen(),
		scrAccessUserEdit: accessUserEditScreen(),
		scrAccessSettings: accessSettingsScreen(),
		scrAccessCheckAsk: accessCheckAskScreen(),
		scrAccessCheck:    accessCheckScreen(),
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
	return ui.RenderColumns(t, append([]string{""}, header...), marked, title) + "\n" + t.Muted.Render(ansi.Truncate(hint, t.W(), "…"))
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
		rows[i] = []string{k, orDash(strings.Join(u.Groups, ",")), twofa, ch, orDash(u.Expires)}
	}
	return []string{"User", "Groups", "2FA", "Channels", "Expires"}, rows, keys
}

func userKeys(app *App, key string, msg tea.KeyMsg) {
	switch {
	case msg.Type == tea.KeyEnter && key != "":
		app.accessEdit = &accessEdit{User: key}
		app.goTo(scrAccessUserEdit)
	case isKey(msg, 'g') && key != "":
		app.accessEdit = &accessEdit{User: key}
		app.goTo(scrAccessGrid)
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
