package lego

import (
	"path/filepath"
	"testing"
	"time"
)

func TestMobileLoginWithout2FAIsImmediatelyUsable(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "lego.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	tok, err := d.StartMobileLogin("dave", "modernwms", "checker", false)
	if err != nil {
		t.Fatal(err)
	}
	s, err := d.MobileSessionFor(tok)
	if err != nil || s.Username != "dave" {
		t.Fatalf("MobileSessionFor = %+v, %v", s, err)
	}
}

func TestMobileLoginWith2FAIsNotUsableUntilConfirmed(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "lego.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	tok, err := d.StartMobileLogin("dave", "modernwms", "checker", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.MobileSessionFor(tok); err != ErrNoMobileSession {
		t.Fatalf("a pending (not yet 2FA'd) token must be refused, got %v", err)
	}

	if err := d.ConfirmMobile2FA(tok); err != nil {
		t.Fatal(err)
	}
	s, err := d.MobileSessionFor(tok)
	if err != nil || s.Username != "dave" {
		t.Fatalf("after confirming 2FA, MobileSessionFor = %+v, %v", s, err)
	}
}

func TestConfirmMobile2FARefusesAnUnknownToken(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "lego.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	if err := d.ConfirmMobile2FA("nonsense"); err != ErrNoMobileSession {
		t.Fatalf("err = %v, want ErrNoMobileSession", err)
	}
}

func TestMobileSessionForRefusesAnExpiredToken(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "lego.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	tok, err := d.StartMobileLogin("dave", "modernwms", "checker", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`UPDATE mobile_sessions SET expires_at = ? WHERE token = ?`, time.Now().Add(-time.Minute).Format(time.RFC3339), tok); err != nil {
		t.Fatal(err)
	}
	if _, err := d.MobileSessionFor(tok); err != ErrNoMobileSession {
		t.Fatalf("an expired token must be refused, got %v", err)
	}
}

func TestEndMobileSessionLogsOut(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "lego.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	tok, _ := d.StartMobileLogin("dave", "modernwms", "checker", false)
	if err := d.EndMobileSession(tok); err != nil {
		t.Fatal(err)
	}
	if _, err := d.MobileSessionFor(tok); err != ErrNoMobileSession {
		t.Fatalf("a logged-out token must be refused, got %v", err)
	}
}
