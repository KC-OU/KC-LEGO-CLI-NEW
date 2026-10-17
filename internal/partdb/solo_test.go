package partdb

import "testing"

// TestZeroValueDBReturnsErrUnavailable covers the solo-mode case (see
// internal/config.SoloMode): cmd/wms deliberately starts with a nil *DB
// instead of aborting when there's no Part-DB sqlite file to open, so every
// exported method must return a clear error instead of panicking.
func TestZeroValueDBReturnsErrUnavailable(t *testing.T) {
	var d *DB // nil, same as cmd/wms/tui.go sets in solo mode

	if _, err := d.SearchParts("anything"); err != ErrUnavailable {
		t.Errorf("SearchParts: err = %v, want ErrUnavailable", err)
	}
	if _, err := d.GetUserByName("kc"); err != ErrUnavailable {
		t.Errorf("GetUserByName: err = %v, want ErrUnavailable", err)
	}
	if _, _, err := d.PasswordHash("kc"); err != ErrUnavailable {
		t.Errorf("PasswordHash: err = %v, want ErrUnavailable", err)
	}
	if _, err := d.AllParts(); err != ErrUnavailable {
		t.Errorf("AllParts: err = %v, want ErrUnavailable", err)
	}
	if err := d.ResetPassword("kc", "hash", false); err != ErrUnavailable {
		t.Errorf("ResetPassword: err = %v, want ErrUnavailable", err)
	}
}
