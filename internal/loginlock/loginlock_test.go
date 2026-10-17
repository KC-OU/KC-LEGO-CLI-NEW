package loginlock

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

func testEnv(t *testing.T) {
	t.Helper()
	t.Setenv(config.LoginLockFile, filepath.Join(t.TempDir(), "login_lock.json"))
}

func TestRenameMovesEntryAndRejectsCollision(t *testing.T) {
	testEnv(t)

	if err := Rename("nobody", "7999"); err != nil {
		t.Fatalf("renaming an entry-less username should no-op, got %v", err)
	}

	if _, _, err := RecordFailure("7354"); err != nil {
		t.Fatal(err)
	}
	if err := Rename("7354", "7421"); err != nil {
		t.Fatal(err)
	}
	if until, locked, err := Locked("7354"); err != nil || locked || !until.IsZero() {
		t.Errorf("Locked(%q) after rename = %v, %v, %v, want cleared", "7354", until, locked, err)
	}
	if _, _, err := RecordFailure("7421"); err != nil {
		t.Fatal(err)
	}
	e, err := update("7421", func(e *entry) {})
	if err != nil || e.Fails != 2 {
		t.Fatalf("the renamed account's failure count = %+v, %v, want 2 (1 moved + 1 new)", e, err)
	}

	if _, _, err := RecordFailure("7500"); err != nil {
		t.Fatal(err)
	}
	if err := Rename("7421", "7500"); err == nil {
		t.Error("renaming onto an already-tracked username should fail, not clobber it")
	}
}

func TestLocksAfterMaxFailuresAndClearsOnSuccess(t *testing.T) {
	testEnv(t)
	username := "dave"

	for i := 0; i < MaxFailures-1; i++ {
		justLocked, _, err := RecordFailure(username)
		if err != nil {
			t.Fatal(err)
		}
		if justLocked {
			t.Fatalf("locked after only %d failures, want %d", i+1, MaxFailures)
		}
	}
	if until, locked, err := Locked(username); err != nil || locked {
		t.Fatalf("Locked before the %dth failure = %v, %v, %v, want not locked", MaxFailures, until, locked, err)
	}

	justLocked, until, err := RecordFailure(username)
	if err != nil || !justLocked {
		t.Fatalf("the %dth failure should lock: justLocked=%v err=%v", MaxFailures, justLocked, err)
	}
	if time.Until(until) <= 0 || time.Until(until) > LockFor {
		t.Errorf("lock until = %v, want roughly %v from now", until, LockFor)
	}

	gotUntil, locked, err := Locked(username)
	if err != nil || !locked || !gotUntil.Equal(until) {
		t.Fatalf("Locked after the lock = %v, %v, %v, want locked until %v", gotUntil, locked, err, until)
	}

	if err := ClearFailures(username); err != nil {
		t.Fatal(err)
	}
	if _, locked, err := Locked(username); err != nil || locked {
		t.Errorf("Locked after ClearFailures = %v, %v, want not locked", locked, err)
	}
	// A fresh RecordFailure after clearing starts the count over, not
	// immediately locked.
	if justLocked, _, err := RecordFailure(username); err != nil || justLocked {
		t.Errorf("a single failure right after clearing should not re-lock: justLocked=%v err=%v", justLocked, err)
	}
}

func TestLockExpiresOnItsOwn(t *testing.T) {
	testEnv(t)
	username := "pat"
	for i := 0; i < MaxFailures; i++ {
		if _, _, err := RecordFailure(username); err != nil {
			t.Fatal(err)
		}
	}
	if _, locked, err := Locked(username); err != nil || !locked {
		t.Fatalf("expected locked, got locked=%v err=%v", locked, err)
	}

	// Directly backdate the lock (the store is just a JSON file) instead of
	// sleeping 15 real minutes in a test.
	path := config.Get(config.LoginLockFile)
	entries, err := load(path)
	if err != nil {
		t.Fatal(err)
	}
	e := entries[key(username)]
	e.LockedUntil = time.Now().Add(-time.Minute)
	entries[key(username)] = e
	if err := save(path, entries); err != nil {
		t.Fatal(err)
	}

	if _, locked, err := Locked(username); err != nil || locked {
		t.Errorf("an expired lock should report not locked, got locked=%v err=%v", locked, err)
	}
}

func TestLockoutIsPerUsernameNotGlobal(t *testing.T) {
	testEnv(t)
	for i := 0; i < MaxFailures; i++ {
		if _, _, err := RecordFailure("alex"); err != nil {
			t.Fatal(err)
		}
	}
	if _, locked, err := Locked("alex"); err != nil || !locked {
		t.Fatalf("alex should be locked, got locked=%v err=%v", locked, err)
	}
	if _, locked, err := Locked("sam"); err != nil || locked {
		t.Errorf("sam should be unaffected by alex's lockout, got locked=%v err=%v", locked, err)
	}
}
