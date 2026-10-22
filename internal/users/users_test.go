package users

import (
	"context"
	"testing"
)

// RenameUnified's guards (protected identity, numeric-login shape) all run
// before it ever touches s.WMS/s.PDB, so a zero-value Service is enough to
// exercise them without a real database.
func TestRenameUnifiedGuards(t *testing.T) {
	s := &Service{}
	ctx := context.Background()

	if err := s.RenameUnified(ctx, "kcollins", "someone-else", "Admin"); err == nil {
		t.Error("expected renaming a protected identity to fail")
	}
	if err := s.RenameUnified(ctx, "admin", "root", "Admin"); err == nil {
		t.Error("expected renaming the other protected identity to fail")
	}
	if err := s.RenameUnified(ctx, "7354", "not-numeric", "Picker"); err == nil {
		t.Error("expected a Picker rename to a non-numeric id to fail")
	}
	if err := s.RenameUnified(ctx, "7354", "", "Picker"); err == nil {
		t.Error("expected an empty new username to fail")
	}
}
