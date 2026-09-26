package uiapp

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/auth"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

// Enforcement of the access policy (internal/access). A user with a policy entry
// is "governed": every check asks the policy. A user without one keeps exactly
// the role-based checks the suite always had (ModernWMS menus, CanWrite, IsAdmin),
// so nothing changes for anyone until an admin gives them a group.

// actionPerm maps the audit action names the existing gates already pass to the
// permission they need under the policy.
var actionPerm = map[string]string{
	"RECEIVE_STOCK_ASN":      "ops.asn",
	"ADD_LEGO_SET":           "lego.edit",
	"ADD_PART":               "partdb.edit",
	"PICK_ITEM":              "", // the picker serves whichever flow opened it; that flow's gate already ran
	"ADJUST_PARTDB_STOCK":    "stock.adjust",
	"SETTINGS_ACCESS":        "settings.view",
	"SETTINGS_THEME":         "settings.edit",
	"USER_MANAGEMENT_ACCESS": "users.view",
	"ACCESS_CONTROL":         "access.manage",
}

// modulePerm maps the hub's module keys to the permission that shows them.
var modulePerm = map[string]string{
	"dashboard": "", "partdb": "partdb.view", "scripts": "scripts.run", "lego": "lego.view",
	"asn": "ops.asn", "warehouse_ops": "ops.edit", "stock_lookup": "stock.view", "master_data": "ops.view",
	"delivery": "ops.view", "user_mgmt": "users.view", "docker": "containers.view", "settings": "settings.view",
	"access": "access.manage",
}

// loadPolicy reads the policy for the signed-in user (at sign-on, and after an
// admin changes it). An unreadable policy leaves the user on the legacy rules and
// is reported, rather than locking everyone out.
func (a *App) loadPolicy() {
	a.policy, a.pUser = &access.Policy{}, nil
	p, err := access.Load()
	if err != nil {
		if a.audit != nil {
			a.audit.Log("", "", "ACCESS_POLICY_UNREADABLE", "FAILED", err.Error())
		}
		return
	}
	a.policy = p
	if a.session != nil {
		a.pUser = p.Users[access.Key(a.session.Source, a.session.Username)]
		a.perms = p.Effective(a.session.Source, a.session.Username)
	}
}

func (a *App) governed() bool { return a.pUser != nil }

// can reports whether the signed-in user holds permission p ("" = anyone signed in).
func (a *App) can(p string) bool {
	if p == "" {
		return a.session != nil
	}
	if a.governed() {
		return a.perms.Can(p)
	}
	return legacyCan(a.session, p)
}

// require is can with the denial: a message naming the permission, and an audit line.
func (a *App) require(p, action string) bool {
	if a.can(p) {
		return true
	}
	user, role := "", ""
	if a.session != nil {
		user, role = a.session.Username, a.session.Role
	}
	why := "not granted"
	if a.governed() {
		why = a.perms.Why(p)
	}
	a.audit.Log(user, role, "DENIED_PERMISSION", "DENIED", fmt.Sprintf("%s needs %s (%s)", action, p, why))
	a.setMsg(fmt.Sprintf("ACCESS DENIED — needs the %s permission (%s).", p, why), true)
	return false
}

// legacyCan is what the role-based rules allowed before the policy existed, spelt
// as permissions, so hidden menu options match what those users could always do.
func legacyCan(s *auth.Session, p string) bool {
	if s == nil || s.Permissions == nil {
		return false
	}
	if s.Permissions.IsAdmin {
		return true
	}
	write := s.Permissions.CanWrite
	area, action, _ := strings.Cut(p, ".")
	mod := func(m string) bool { return auth.IsModuleAllowed(s, m) }
	switch area {
	case "lego":
		return mod("lego") && (write || action == "view" || action == "search" || action == "export")
	case "partdb":
		return mod("partdb") && (write || action == "view")
	case "stock":
		return write || action != "adjust"
	case "ops":
		switch action {
		case "asn":
			return mod("asn") && write
		case "edit":
			return mod("warehouse_ops") && write
		}
		return mod("master_data") || mod("warehouse_ops") || mod("delivery") || mod("stock_lookup")
	case "exports", "bricklink", "audit":
		return true
	case "sets", "orders", "labels":
		return mod("lego") && (write || action == "view" || action == "print")
	case "scripts":
		return mod("scripts")
	case "containers":
		return action == "view" && mod("docker")
	case "users":
		return mod("user_mgmt") && write
	}
	return false // access and settings: admins only
}

// isAdmin is true only once login (and any 2FA) is done and the session carries the
// admin flag — used where a check needs "an admin, specifically" rather than a named
// permission (e.g. browsing everyone's archived reports, not just your own).
func (a *App) isAdmin() bool {
	return a.session != nil && a.session.Permissions != nil && a.session.Permissions.IsAdmin
}

// moduleAllowed replaces auth.IsModuleAllowed for the hub and its submenus.
func (a *App) moduleAllowed(m string) bool {
	if a.governed() {
		return a.can(modulePerm[m])
	}
	if m == "access" {
		return a.isAdmin()
	}
	return auth.IsModuleAllowed(a.session, m)
}

// ---- sign-on and session timings ----

// channel is how this session came in: a shell on this box, telnet, or the web terminal.
func (a *App) channel() string {
	switch {
	case !a.requireTwoFA:
		return access.ChannelLocal
	case a.touchMode:
		return access.ChannelWeb
	}
	return access.ChannelTelnet
}

func configMinutes(key string) int {
	n, err := strconv.Atoi(strings.TrimSpace(config.Get(key)))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// graceWindow is how long after a verified code the same origin may sign in again
// without one: the user's override, the policy's value, else TWOFA_GRACE_MINUTES.
func (a *App) graceWindow() time.Duration {
	return time.Duration(a.pol().GraceMinutes(a.pUser, configMinutes(config.TwoFAGraceMinutes))) * time.Minute
}

// idleLimit is when an idle session locks (0 = never).
func (a *App) idleLimit() time.Duration {
	return time.Duration(a.pol().IdleMinutes(a.pUser, configMinutes(config.IdleLockMinutes))) * time.Minute
}

// maxSession is the longest a session may last however active (0 = no limit).
func (a *App) maxSession() time.Duration {
	return time.Duration(a.pol().MaxSessionHours(a.pUser)) * time.Hour
}

func (a *App) pol() *access.Policy {
	if a.policy == nil {
		return &access.Policy{}
	}
	return a.policy
}

// denySignOn ends a sign-on that the policy refuses.
func (a *App) denySignOn(action, msg string) {
	a.audit.Log(a.session.Username, a.session.Role, action, "DENIED", msg)
	a.session, a.pUser = nil, nil
	a.setMsg(msg, true)
	a.screens[scrLogin].OnEnter(a)
}

// screenPerm is the permission each screen needs. Navigation checks it for governed
// users (goTo), so a screen can't be reached by any path — menu, key, palette —
// without it; the palette also hides what the user can't open.
var screenPerm = map[string]string{
	scrOverview: "", scrTour: "", scrPartDBHub: "partdb.view", scrPartDBBrowse: "partdb.view", scrPartDBResults: "partdb.view",
	scrPartDBLookup: "partdb.view", scrPartDBDetail: "partdb.view", scrPartDBCreate: "partdb.edit", scrPartDBAdjust: "stock.adjust",
	scrScripts: "scripts.run", scrAuditLog: "audit.view", scrASN: "ops.asn", scrOpsHub: "ops.view", scrAdminHub: "",
	scrInventory: "stock.view", scrInventoryResults: "stock.view",
	scrLegoHub: "lego.view", scrLegoSetSearch: "lego.search", scrLegoSetFind: "lego.search", scrLegoPartSearch: "lego.search",
	scrLegoSetAdd: "lego.edit", scrLegoPartAdd: "lego.edit", scrLegoPartOwned: "lego.view", scrLegoStats: "lego.view",
	scrLegoMissingAsk: "lego.view", scrLegoMissing: "lego.view", scrLegoBuild: "lego.view", scrLegoHistory: "lego.view",
	scrLegoDetailAsk: "lego.view", scrLegoDetail: "lego.view", scrLegoBLAsk: "bricklink.view", scrLegoAchievements: "lego.view",
	scrExport: "exports.create", scrUsers: "users.view", scrSettingsHub: "settings.view", scrSettingsTheme: "settings.edit",
	scrSettingsBrickLink: "settings.edit", scrMyTheme: "", scrMyLoading: "",
	scrAccessHub: "access.manage", scrAccessGroups: "access.manage", scrAccessGroupNew: "access.manage", scrAccessGrid: "access.manage",
	scrAccessUsers: "access.manage", scrAccessUserNew: "access.manage", scrAccessUserEdit: "access.manage",
	scrAccessSettings: "access.manage", scrAccessCheckAsk: "access.manage", scrAccessCheck: "access.manage",
	scrWorkshop: "lego.view", scrCompletion: "lego.view", scrSetMissing: "lego.view", scrShopLinks: "lego.view",
	scrOrders: "orders.view", scrOrderLines: "orders.view", scrOrderEdit: "orders.manage", scrLinePrice: "orders.manage",
	scrLineReceive: "orders.manage", scrSpend: "orders.view", scrLabels: "labels.print", scrLabelsAsk: "labels.print",
	scrSetInfo: "lego.edit", scrCheckAsk: "sets.check", scrNotify: "access.manage", scrNotifyRoute: "access.manage",
	scrReports: "lego.view", scrReportsSet: "lego.view", scrRetiring: "lego.view", scrReportsStocktake: "lego.view", scrReportsArchive: "lego.view",
}

// mayOpen reports whether the user may open screen id (unlisted screens are
// reached only from a listed one, whose check already ran).
func (a *App) mayOpen(id string) bool { return a.can(screenPerm[id]) }
