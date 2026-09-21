package auth

import (
	"context"
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/partdb"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/wmsdb"
)

type fakeWMS struct {
	authResult *wmsdb.UserAuthResult
	authErr    error
	perms      map[string]*wmsdb.Permissions
}

func (f *fakeWMS) AuthenticateUser(ctx context.Context, usernameOrID, password, passwordMD5 string) (*wmsdb.UserAuthResult, error) {
	if f.authErr != nil {
		return nil, f.authErr
	}
	return f.authResult, nil
}

func (f *fakeWMS) FetchUserPermissions(ctx context.Context, role string) (*wmsdb.Permissions, error) {
	if p, ok := f.perms[role]; ok {
		return p, nil
	}
	return &wmsdb.Permissions{RoleName: role, Menus: nil, CanWrite: false, IsAdmin: false}, nil
}

type fakePartDB struct {
	hash  string
	found bool
	user  *partdb.PartDBUser
}

func (f *fakePartDB) PasswordHash(name string) (string, bool, error) {
	return f.hash, f.found, nil
}

func (f *fakePartDB) GetUserByName(name string) (*partdb.PartDBUser, error) {
	return f.user, nil
}

func adminPerms() *wmsdb.Permissions {
	return &wmsdb.Permissions{RoleName: "admin", Menus: []string{"*"}, CanWrite: true, IsAdmin: true}
}

func pickerPerms() *wmsdb.Permissions {
	return &wmsdb.Permissions{RoleName: "picker", Menus: []string{"stockAsn", "stockManagement"}, CanWrite: true, IsAdmin: false}
}

func viewOnlyPerms() *wmsdb.Permissions {
	return &wmsdb.Permissions{RoleName: "viewonly", Menus: []string{"stockManagement", "baseModule"}, CanWrite: false, IsAdmin: false}
}

func TestAuthenticateUser_ModernWMSFound(t *testing.T) {
	wms := &fakeWMS{
		authResult: &wmsdb.UserAuthResult{Found: true, ID: 1, UserName: "admin", UserNum: "7354", Role: "admin", IsValid: true},
		perms:      map[string]*wmsdb.Permissions{"admin": adminPerms()},
	}
	pdb := &fakePartDB{}

	s, err := AuthenticateUser(context.Background(), wms, pdb, "admin", "secret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Source != "modernwms" || s.Username != "admin" || !s.Permissions.IsAdmin {
		t.Fatalf("unexpected session: %+v", s)
	}
}

func TestAuthenticateUser_FallsBackToPartDB(t *testing.T) {
	wms := &fakeWMS{
		authResult: &wmsdb.UserAuthResult{Found: false},
		perms:      map[string]*wmsdb.Permissions{"PartDB Admin": adminPerms()},
	}
	hash, err := HashPartDB("correct-horse")
	if err != nil {
		t.Fatalf("hashing: %v", err)
	}
	pdb := &fakePartDB{
		hash:  hash,
		found: true,
		user:  &partdb.PartDBUser{ID: 1, Name: "kcollins", GroupID: 1, Disabled: false},
	}

	s, err := AuthenticateUser(context.Background(), wms, pdb, "kcollins", "correct-horse")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Source != "partdb" || s.Role != "PartDB Admin" {
		t.Fatalf("unexpected session: %+v", s)
	}
}

func TestAuthenticateUser_PartDBWrongPassword(t *testing.T) {
	wms := &fakeWMS{authResult: &wmsdb.UserAuthResult{Found: false}}
	hash, _ := HashPartDB("correct-horse")
	pdb := &fakePartDB{hash: hash, found: true, user: &partdb.PartDBUser{ID: 5, Name: "bob"}}

	_, err := AuthenticateUser(context.Background(), wms, pdb, "bob", "wrong")
	if err != ErrInvalidCredentials {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestAuthenticateUser_DisabledAccount(t *testing.T) {
	wms := &fakeWMS{
		authResult: &wmsdb.UserAuthResult{Found: true, UserName: "bob", Role: "picker", IsValid: false},
	}
	pdb := &fakePartDB{}

	_, err := AuthenticateUser(context.Background(), wms, pdb, "bob", "secret")
	if err != ErrAccountDisabled {
		t.Fatalf("expected ErrAccountDisabled, got %v", err)
	}
}

func TestIsModuleAllowed_Admin(t *testing.T) {
	s := &Session{Permissions: adminPerms()}
	for _, m := range []string{"dashboard", "asn", "warehouse_ops", "user_mgmt", "docker"} {
		if !IsModuleAllowed(s, m) {
			t.Errorf("admin should be allowed module %q", m)
		}
	}
}

func TestIsModuleAllowed_Picker(t *testing.T) {
	s := &Session{Permissions: pickerPerms()}
	if !IsModuleAllowed(s, "asn") {
		t.Error("picker should be allowed asn (has stockAsn menu)")
	}
	if !IsModuleAllowed(s, "stock_lookup") {
		t.Error("picker should be allowed stock_lookup (has stockManagement menu)")
	}
	if IsModuleAllowed(s, "user_mgmt") {
		t.Error("picker should not be allowed user_mgmt")
	}
	if IsModuleAllowed(s, "docker") {
		t.Error("picker should not be allowed docker")
	}
	if !CheckWritePermission(s) {
		t.Error("picker should have write permission")
	}
}

func TestIsModuleAllowed_ViewOnly(t *testing.T) {
	s := &Session{Permissions: viewOnlyPerms()}
	if !IsModuleAllowed(s, "stock_lookup") {
		t.Error("viewonly should see stock_lookup (stockManagement menu)")
	}
	if IsModuleAllowed(s, "asn") {
		t.Error("viewonly should not see asn")
	}
	if CheckWritePermission(s) {
		t.Error("viewonly must not have write permission")
	}
	if !IsModuleAllowed(s, "dashboard") || !IsModuleAllowed(s, "partdb") || !IsModuleAllowed(s, "scripts") {
		t.Error("dashboard/partdb/scripts must always be allowed")
	}
}
