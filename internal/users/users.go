// Package users holds the dual-database user-management orchestration
// shared by the CLI (`wms users ...`) and the TUI's Users screen, so the
// create/reset/modify/toggle/delete logic against both ModernWMS and
// Part-DB lives in exactly one place instead of being duplicated per
// front end.
package users

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/auth"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/partdb"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/wmsdb"
)

// NumericLoginUsernameRE is a Picker/Checker account's required user-ID
// shape: 4-5 digits, easy to type on a handheld barcode scanner's number
// keys alone (an MC9100 over telnet's awkward alpha-shift layer is what
// prompted this) — shared by both the CLI (`wms users create/reset`) and
// the TUI's Users screen, so the rule can't drift between the two.
var NumericLoginUsernameRE = regexp.MustCompile(`^\d{4,5}$`)

// WantsNumericLogin is which roles get the numeric user-ID/PIN format — the
// handheld's keypad is what pickers/checkers actually carry; admins still
// sign in from a full keyboard (desktop/web), so theirs are untouched.
func WantsNumericLogin(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "picker", "checker":
		return true
	default:
		return false
	}
}

type Service struct {
	WMS *wmsdb.Client
	PDB *partdb.DB
}

func New(wms *wmsdb.Client, pdb *partdb.DB) *Service {
	return &Service{WMS: wms, PDB: pdb}
}

type Row struct {
	System, ID, Username, RoleOrGroup, Status, TempPW, Email string
}

// ListAll returns both systems' user tables as display rows, matching the
// original's combined "Registered Users" table.
func (s *Service) ListAll(ctx context.Context) ([]Row, []error) {
	var rows []Row
	var errs []error

	wmsUsers, err := s.WMS.ListUsers(ctx)
	if err != nil {
		errs = append(errs, fmt.Errorf("ModernWMS: %w", err))
	}
	for _, u := range wmsUsers {
		rows = append(rows, Row{"ModernWMS", strconv.Itoa(u.ID), u.UserName, u.Role, validLabel(u.IsValid), tempPWLabel(u.MustChangePW), u.Email})
	}

	pdbUsers, err := s.PDB.ListUsers()
	if err != nil {
		errs = append(errs, fmt.Errorf("Part-DB: %w", err))
	}
	for _, u := range pdbUsers {
		rows = append(rows, Row{"Part-DB", strconv.Itoa(u.ID), u.Name, u.GroupName, validLabel(!u.Disabled), tempPWLabel(u.NeedPWChange), u.Email})
	}
	return rows, errs
}

func validLabel(v bool) string {
	if v {
		return "ACTIVE"
	}
	return "DISABLED"
}

func tempPWLabel(v bool) string {
	if v {
		return "YES (FORCED)"
	}
	return "NO"
}

// CreateUnified creates the same account with the same password in both
// systems, best-effort: success if either side succeeds.
func (s *Service) CreateUnified(ctx context.Context, username, role, email, password string) error {
	_, wmsErr := s.WMS.CreateUser(ctx, username, role, email, auth.HashModernWMS(password), true)

	pdbHash, hashErr := auth.HashPartDB(password)
	var pdbErr error
	if hashErr != nil {
		pdbErr = hashErr
	} else {
		_, pdbErr = s.PDB.CreateUser(username, role, email, pdbHash, true)
	}

	if wmsErr != nil && pdbErr != nil {
		return fmt.Errorf("ModernWMS: %v; Part-DB: %v", wmsErr, pdbErr)
	}
	return nil
}

// ResetUnified resets to the same password in both systems, best-effort.
func (s *Service) ResetUnified(ctx context.Context, username, password string, isTemp bool) error {
	wmsErr := s.WMS.ResetPassword(ctx, username, auth.HashModernWMS(password), isTemp)

	pdbHash, hashErr := auth.HashPartDB(password)
	var pdbErr error
	if hashErr != nil {
		pdbErr = hashErr
	} else {
		pdbErr = s.PDB.ResetPassword(username, pdbHash, isTemp)
	}

	if wmsErr != nil && pdbErr != nil {
		return fmt.Errorf("user %q not found in either system", username)
	}
	return nil
}

// ModifyBoth applies a role/email update to both systems unconditionally
// (best effort), matching the original's module_user_management option 4.
func (s *Service) ModifyBoth(ctx context.Context, username string, role, email *string) error {
	wmsErr := s.WMS.ModifyUser(ctx, username, role, email, nil)
	pdbErr := s.PDB.ModifyUser(username, role, email, nil)
	if wmsErr != nil && pdbErr != nil {
		return fmt.Errorf("ModernWMS: %v; Part-DB: %v", wmsErr, pdbErr)
	}
	return nil
}

// ToggleBoth enables/disables the account in both systems, best effort.
func (s *Service) ToggleBoth(ctx context.Context, username string, isValid bool) error {
	wmsErr := s.WMS.ToggleActive(ctx, username, isValid)
	pdbErr := s.PDB.ToggleActive(username, isValid)
	if wmsErr != nil && pdbErr != nil {
		return fmt.Errorf("ModernWMS: %v; Part-DB: %v", wmsErr, pdbErr)
	}
	return nil
}

// protectedIdentities mirrors the original's combined delete-guard list
// across both systems: admin/1 (ModernWMS), kcollins/2 (Part-DB).
var protectedIdentities = map[string]bool{"admin": true, "1": true, "kcollins": true, "2": true}

// DeleteBoth removes the account from both systems, best effort, after the
// shared protected-account guard (the per-DB functions also guard their own
// side, this is the "before even dispatching" check from the original).
func (s *Service) DeleteBoth(ctx context.Context, username string) error {
	if protectedIdentities[strings.ToLower(username)] {
		return fmt.Errorf("protected administrator account %q cannot be deleted", username)
	}
	wmsErr := s.WMS.DeleteUser(ctx, username)
	pdbErr := s.PDB.DeleteUser(username)
	if wmsErr != nil && pdbErr != nil {
		return fmt.Errorf("ModernWMS: %v; Part-DB: %v", wmsErr, pdbErr)
	}
	return nil
}

// RenameUnified changes a user's ID/username in both systems, best effort.
// A numeric-login role (see WantsNumericLogin) still must land on a 4-5
// digit id afterward -- the handheld keypad rule applies to a rename just
// as much as it does to creation. Non-numeric-login accounts (admins, and
// the dashboard's own accounts) can rename to any valid username, text or
// numeric -- the ID is mandatory-numeric only where a physical keypad is
// what actually logs in with it.
func (s *Service) RenameUnified(ctx context.Context, usernameOrID, newUsername, role string) error {
	newUsername = strings.TrimSpace(newUsername)
	if newUsername == "" {
		return fmt.Errorf("new username is required")
	}
	if protectedIdentities[strings.ToLower(usernameOrID)] {
		return fmt.Errorf("protected administrator account %q cannot be renamed", usernameOrID)
	}
	if WantsNumericLogin(role) && !NumericLoginUsernameRE.MatchString(newUsername) {
		return fmt.Errorf("%s accounts need a 4-5 digit numeric user ID (for a handheld scanner's keypad) — got %q", role, newUsername)
	}
	wmsErr := s.WMS.RenameUser(ctx, usernameOrID, newUsername)
	pdbErr := s.PDB.RenameUser(usernameOrID, newUsername)
	if wmsErr != nil && pdbErr != nil {
		return fmt.Errorf("ModernWMS: %v; Part-DB: %v", wmsErr, pdbErr)
	}
	return nil
}

func (s *Service) DeleteOne(ctx context.Context, system, username string) error {
	switch strings.ToLower(system) {
	case "wms", "modernwms":
		return s.WMS.DeleteUser(ctx, username)
	case "partdb", "pdb":
		return s.PDB.DeleteUser(username)
	default:
		return fmt.Errorf("system must be \"wms\" or \"partdb\"")
	}
}

func (s *Service) ToggleOne(ctx context.Context, system, username string, isValid bool) error {
	switch strings.ToLower(system) {
	case "wms", "modernwms":
		return s.WMS.ToggleActive(ctx, username, isValid)
	case "partdb", "pdb":
		return s.PDB.ToggleActive(username, isValid)
	default:
		return fmt.Errorf("system must be \"wms\" or \"partdb\"")
	}
}
