package auth

import (
	"context"
	"errors"
	"strings"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/partdb"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/wmsdb"
)

var ErrInvalidCredentials = errors.New("invalid username or password")
var ErrAccountDisabled = errors.New("account is disabled")

// WMSAuthenticator and PartDBAuthenticator narrow wmsdb.Client/partdb.DB to
// what AuthenticateUser needs, so tests can substitute fakes instead of
// requiring a live docker container or sqlite file.
type WMSAuthenticator interface {
	AuthenticateUser(ctx context.Context, usernameOrID, password, passwordMD5 string) (*wmsdb.UserAuthResult, error)
	FetchUserPermissions(ctx context.Context, role string) (*wmsdb.Permissions, error)
}

type PartDBAuthenticator interface {
	PasswordHash(name string) (string, bool, error)
	GetUserByName(name string) (*partdb.PartDBUser, error)
}

type Session struct {
	Source      string // "modernwms" | "partdb"
	UserID      int
	Username    string
	Role        string
	Email       string
	IsValid     bool
	Permissions *wmsdb.Permissions
}

// AuthenticateUser mirrors modernwms_tui.py's authenticate_user: try
// ModernWMS first (matching the original's permissive check against both
// the raw password and its MD5 digest), fall back to Part-DB bcrypt auth
// when ModernWMS doesn't find/accept the user.
func AuthenticateUser(ctx context.Context, wms WMSAuthenticator, pdb PartDBAuthenticator, usernameOrID, password string) (*Session, error) {
	wmsResult, err := wms.AuthenticateUser(ctx, usernameOrID, password, HashModernWMS(password))
	if err == nil && wmsResult.Found {
		if !wmsResult.IsValid && !strings.EqualFold(wmsResult.UserName, "admin") {
			return nil, ErrAccountDisabled
		}
		perms, permErr := wms.FetchUserPermissions(ctx, wmsResult.Role)
		if permErr != nil {
			return nil, permErr
		}
		return &Session{
			Source:      "modernwms",
			UserID:      wmsResult.ID,
			Username:    wmsResult.UserName,
			Role:        wmsResult.Role,
			Email:       wmsResult.Email,
			IsValid:     wmsResult.IsValid,
			Permissions: perms,
		}, nil
	}

	hash, found, hashErr := pdb.PasswordHash(usernameOrID)
	if hashErr != nil || !found || !VerifyPartDB(password, hash) {
		return nil, ErrInvalidCredentials
	}
	user, userErr := pdb.GetUserByName(usernameOrID)
	if userErr != nil || user == nil {
		return nil, ErrInvalidCredentials
	}
	if user.Disabled {
		return nil, ErrAccountDisabled
	}

	// Mirrors the original's is_admin = group_id==1 or name=='kcollins' or id in (1,2).
	isAdmin := user.GroupID == 1 || strings.EqualFold(user.Name, "kcollins") || user.ID == 1 || user.ID == 2
	role := "PartDB User"
	if isAdmin {
		role = "PartDB Admin"
	}
	perms, permErr := wms.FetchUserPermissions(ctx, role)
	if permErr != nil {
		return nil, permErr
	}
	return &Session{
		Source:      "partdb",
		UserID:      user.ID,
		Username:    user.Name,
		Role:        role,
		Email:       user.Email,
		IsValid:     !user.Disabled,
		Permissions: perms,
	}, nil
}

// moduleMenus maps a TUI module/tab key to the ModernWMS menu names that
// grant access to it, mirroring modernwms_tui.py's is_module_allowed.
// "docker" maps to no menus at all — it's reachable only via the IsAdmin
// short-circuit in IsModuleAllowed, matching the original where the
// Containers tab has no menu-permission path, admin-only in practice.
var moduleMenus = map[string][]string{
	"asn":           {"stockAsn"},
	"warehouse_ops": {"warehouseProcessing", "warehouseMove", "warehouseFreeze", "warehouseAdjust", "warehouseTaking"},
	"stock_lookup":  {"stockManagement"},
	"master_data":   {"baseModule", "commodityManagement", "supplier", "customer", "warehouseSetting", "ownerOfCargo", "freightSetting", "commodityCategorySetting"},
	"user_mgmt":     {"userManagement", "userRoleSetting", "roleMenu"},
	"delivery":      {"deliveryManagement"},
	"docker":        {},
	"settings":      {},
}

var alwaysAllowedModules = map[string]bool{"dashboard": true, "partdb": true, "scripts": true, "lego": true}

func HasMenuPermission(s *Session, menu string) bool {
	if s == nil || s.Permissions == nil {
		return false
	}
	if s.Permissions.IsAdmin {
		return true
	}
	for _, m := range s.Permissions.Menus {
		if m == "*" || m == menu {
			return true
		}
	}
	return false
}

func HasAnyMenuPermission(s *Session, menus []string) bool {
	for _, m := range menus {
		if HasMenuPermission(s, m) {
			return true
		}
	}
	return false
}

// IsModuleAllowed mirrors the original exactly: admins always pass;
// dashboard/partdb/scripts are always visible; everything else needs at
// least one of its mapped menu names (docker's empty mapping means it's
// unreachable except via the admin short-circuit above).
func IsModuleAllowed(s *Session, moduleKey string) bool {
	if s == nil || s.Permissions == nil {
		return false
	}
	if s.Permissions.IsAdmin {
		return true
	}
	if alwaysAllowedModules[moduleKey] {
		return true
	}
	menus, ok := moduleMenus[moduleKey]
	if !ok || len(menus) == 0 {
		return false
	}
	return HasAnyMenuPermission(s, menus)
}

// CheckWritePermission mirrors check_write_permission: callers (cmd/wms,
// the TUI) are responsible for showing the denial and logging it via
// internal/audit — this just answers the question.
func CheckWritePermission(s *Session) bool {
	return s != nil && s.Permissions != nil && s.Permissions.CanWrite
}
