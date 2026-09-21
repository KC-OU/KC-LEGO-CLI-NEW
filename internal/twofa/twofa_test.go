package twofa

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

func init() { bcryptCost = bcrypt.MinCost }

func withTempStore(t *testing.T) {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "2fa-*.json")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	t.Setenv("TWOFA_FILE", f.Name())
}

// advance moves the package clock forward by d for the rest of the test.
func advance(t *testing.T, d time.Duration) {
	t.Helper()
	old := now
	base := old()
	now = func() time.Time { return base.Add(d) }
	t.Cleanup(func() { now = old })
}

// enrolledUser returns the secret of a freshly enrolled account whose enrolment
// code's step is already spent (the clock is moved a minute on).
func enrolledUser(t *testing.T, user string) string {
	t.Helper()
	withTempStore(t)
	secret, _, err := Enroll(user, "modernwms")
	if err != nil {
		t.Fatal(err)
	}
	code, _ := totp.GenerateCode(secret, now())
	if _, err := Confirm(user, "modernwms", code); err != nil {
		t.Fatal(err)
	}
	advance(t, time.Minute)
	return secret
}

func codeFor(t *testing.T, secret string) string {
	t.Helper()
	c, err := totp.GenerateCode(secret, now())
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestEnrollConfirmVerifyRoundTrip(t *testing.T) {
	withTempStore(t)

	secret, uri, err := Enroll("alice", "modernwms")
	if err != nil {
		t.Fatal(err)
	}
	if secret == "" || uri == "" {
		t.Fatal("expected non-empty secret and otpauth URI")
	}
	if IsEnabled("alice", "modernwms") {
		t.Fatal("must not be enabled before Confirm")
	}

	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	backupCodes, err := Confirm("alice", "modernwms", code)
	if err != nil {
		t.Fatal(err)
	}
	if len(backupCodes) != backupCodeCount {
		t.Fatalf("got %d backup codes, want %d", len(backupCodes), backupCodeCount)
	}
	if !IsEnabled("alice", "modernwms") {
		t.Fatal("must be enabled after Confirm")
	}

	advance(t, time.Minute) // the enrolment code's step is spent; log in with the next one
	code2, err := totp.GenerateCode(secret, now())
	if err != nil {
		t.Fatal(err)
	}
	used, err := Verify("alice", "modernwms", code2)
	if err != nil {
		t.Fatal(err)
	}
	if used {
		t.Fatal("a real TOTP code must not be reported as a backup code")
	}
}

func TestVerifyRejectsWrongCode(t *testing.T) {
	withTempStore(t)
	secret, _, _ := Enroll("bob", "partdb")
	code, _ := totp.GenerateCode(secret, time.Now())
	Confirm("bob", "partdb", code)

	if _, err := Verify("bob", "partdb", "000000"); err != ErrInvalidCode {
		t.Fatalf("got %v, want ErrInvalidCode", err)
	}
}

func TestBackupCodeSingleUse(t *testing.T) {
	withTempStore(t)
	secret, _, _ := Enroll("carol", "modernwms")
	code, _ := totp.GenerateCode(secret, time.Now())
	backupCodes, err := Confirm("carol", "modernwms", code)
	if err != nil {
		t.Fatal(err)
	}

	first := backupCodes[0]
	used, err := Verify("carol", "modernwms", first)
	if err != nil {
		t.Fatal(err)
	}
	if !used {
		t.Fatal("expected the backup code path to be reported as used")
	}
	if RemainingBackupCodes("carol", "modernwms") != backupCodeCount-1 {
		t.Fatalf("expected %d remaining, got %d", backupCodeCount-1, RemainingBackupCodes("carol", "modernwms"))
	}

	if _, err := Verify("carol", "modernwms", first); err != ErrInvalidCode {
		t.Fatalf("reused backup code should fail, got %v", err)
	}
}

func TestDisableRemovesEntry(t *testing.T) {
	withTempStore(t)
	secret, _, _ := Enroll("dave", "modernwms")
	code, _ := totp.GenerateCode(secret, time.Now())
	Confirm("dave", "modernwms", code)

	if !IsEnabled("dave", "modernwms") {
		t.Fatal("expected enabled before Disable")
	}
	if err := Disable("dave", "modernwms"); err != nil {
		t.Fatal(err)
	}
	if IsEnabled("dave", "modernwms") {
		t.Fatal("expected disabled after Disable")
	}
	if _, err := Verify("dave", "modernwms", "000000"); err != ErrNotEnrolled {
		t.Fatalf("got %v, want ErrNotEnrolled", err)
	}
}

// enrolled returns a store with alice enrolled and enabled.
func enrolled(t *testing.T) {
	t.Helper()
	withTempStore(t)
	secret, _, err := Enroll("alice", "modernwms")
	if err != nil {
		t.Fatal(err)
	}
	code, _ := totp.GenerateCode(secret, time.Now())
	if _, err := Confirm("alice", "modernwms", code); err != nil {
		t.Fatal(err)
	}
}

func TestGraceWindow(t *testing.T) {
	enrolled(t)
	const w = 30 * time.Minute

	if _, ok := WithinGrace("alice", "modernwms", "telnet:10.0.0.5", w); ok {
		t.Fatal("no code has been verified yet, so there must be no grace")
	}
	if err := MarkVerified("alice", "modernwms", "telnet:10.0.0.5", w); err != nil {
		t.Fatal(err)
	}
	if ago, ok := WithinGrace("alice", "modernwms", "telnet:10.0.0.5", w); !ok || ago > time.Minute {
		t.Fatalf("same origin just after a verified code must be in grace, got ok=%v ago=%v", ok, ago)
	}
	if _, ok := WithinGrace("alice", "modernwms", "telnet:10.0.0.99", w); ok {
		t.Fatal("a different address must not inherit the grace window")
	}
	if _, ok := WithinGrace("alice", "partdb", "telnet:10.0.0.5", w); ok {
		t.Fatal("grace is per account source too")
	}
	if _, ok := WithinGrace("alice", "modernwms", "telnet:10.0.0.5", 0); ok {
		t.Fatal("a zero window disables grace")
	}
}

func TestGraceExpires(t *testing.T) {
	enrolled(t)
	if err := MarkVerified("alice", "modernwms", "local", time.Hour); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	if _, ok := WithinGrace("alice", "modernwms", "local", time.Millisecond); ok {
		t.Fatal("once the window has passed the code must be required again")
	}
}

// The web terminal can't report an address, so it must never get a window.
func TestGraceNeedsAnOrigin(t *testing.T) {
	enrolled(t)
	if err := MarkVerified("alice", "modernwms", "", time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, ok := WithinGrace("alice", "modernwms", "", time.Hour); ok {
		t.Fatal("an unknown origin must never be trusted")
	}
}

func TestGraceClearedByDisableAndReenroll(t *testing.T) {
	enrolled(t)
	const w = time.Hour
	_ = MarkVerified("alice", "modernwms", "local", w)
	if err := Disable("alice", "modernwms"); err != nil {
		t.Fatal(err)
	}
	if _, ok := WithinGrace("alice", "modernwms", "local", w); ok {
		t.Fatal("disabling 2FA must drop the grace record")
	}
	// Re-enrolling (new secret) must not inherit an old window either.
	enrolled2 := func() {
		secret, _, _ := Enroll("alice", "modernwms")
		code, _ := totp.GenerateCode(secret, time.Now())
		_, _ = Confirm("alice", "modernwms", code)
	}
	enrolled2()
	if _, ok := WithinGrace("alice", "modernwms", "local", w); ok {
		t.Fatal("a fresh enrollment starts with no grace")
	}
}

func TestMarkVerifiedPrunesExpiredOrigins(t *testing.T) {
	enrolled(t)
	_ = MarkVerified("alice", "modernwms", "telnet:1.1.1.1", time.Millisecond)
	time.Sleep(5 * time.Millisecond)
	_ = MarkVerified("alice", "modernwms", "telnet:2.2.2.2", time.Millisecond)

	entries, _ := newStore().load()
	if got := len(entries["modernwms:alice"].VerifiedAt); got != 1 {
		t.Fatalf("expired origins should be dropped on write, %d origins remain", got)
	}
}

func TestACodeCannotBeUsedTwice(t *testing.T) {
	secret := enrolledUser(t, "rita")
	code := codeFor(t, secret)
	if _, err := Verify("rita", "modernwms", code); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify("rita", "modernwms", code); !errors.Is(err, ErrCodeReused) {
		t.Fatalf("second use of the same code = %v, want ErrCodeReused", err)
	}
	// A replay is not a guess: it must not push the account toward lockout.
	for i := 0; i < maxFailures+2; i++ {
		_, _ = Verify("rita", "modernwms", code)
	}
	if _, locked := Locked("rita", "modernwms"); locked {
		t.Fatal("replays must not count toward lockout")
	}
	advance(t, 2*time.Minute)
	if _, err := Verify("rita", "modernwms", codeFor(t, secret)); err != nil {
		t.Fatalf("the next code must work: %v", err)
	}
}

func TestTheEnrolmentCodeIsNotAValidLoginCode(t *testing.T) {
	withTempStore(t)
	secret, _, _ := Enroll("eve", "modernwms")
	code := codeFor(t, secret)
	if _, err := Confirm("eve", "modernwms", code); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify("eve", "modernwms", code); !errors.Is(err, ErrCodeReused) {
		t.Fatalf("Verify with the enrolment code = %v, want ErrCodeReused", err)
	}
}

func TestFiveWrongCodesLockTheAccountAndDoubleEachTime(t *testing.T) {
	secret := enrolledUser(t, "lou")
	for i := 1; i < maxFailures; i++ {
		if _, err := Verify("lou", "modernwms", "000000"); !errors.Is(err, ErrInvalidCode) {
			t.Fatalf("wrong code %d = %v, want ErrInvalidCode", i, err)
		}
	}
	var le *LockedError
	if _, err := Verify("lou", "modernwms", "000000"); !errors.As(err, &le) {
		t.Fatalf("the %dth wrong code = %v, want *LockedError", maxFailures, err)
	}
	if d := le.Until.Sub(now()); d > lockBase || d < lockBase-time.Minute {
		t.Errorf("first lock lasts %v, want about %v", d, lockBase)
	}
	// Locked: even the right code is refused, and the grace shortcut is off.
	if _, err := Verify("lou", "modernwms", codeFor(t, secret)); !errors.As(err, &le) {
		t.Fatalf("a correct code while locked = %v, want *LockedError", err)
	}
	_ = MarkVerified("lou", "modernwms", "telnet:1.2.3.4", time.Hour)
	if _, ok := WithinGrace("lou", "modernwms", "telnet:1.2.3.4", time.Hour); ok {
		t.Error("a locked account must not get the grace shortcut")
	}

	advance(t, lockBase+time.Minute)
	var err error
	for i := 0; i < maxFailures; i++ {
		_, err = verifyWrong()
	}
	if !errors.As(err, &le) {
		t.Fatalf("second lockout = %v", err)
	}
	if d := le.Until.Sub(now()); d <= lockBase {
		t.Errorf("second lock lasts %v, want longer than the first (%v)", d, lockBase)
	}
}

func verifyWrong() (bool, error) { return Verify("lou", "modernwms", "000000") }

func TestLockDurationDoublesUpToAnHour(t *testing.T) {
	want := []time.Duration{5 * time.Minute, 10 * time.Minute, 20 * time.Minute, 40 * time.Minute, time.Hour, time.Hour}
	for i, w := range want {
		if got := lockFor(i + 1); got != w {
			t.Errorf("lockFor(%d) = %v, want %v", i+1, got, w)
		}
	}
}

func TestSuccessClearsFailuresAndUnlockClearsALock(t *testing.T) {
	secret := enrolledUser(t, "sam")
	for i := 0; i < maxFailures-1; i++ {
		_, _ = Verify("sam", "modernwms", "000000")
	}
	if _, err := Verify("sam", "modernwms", codeFor(t, secret)); err != nil {
		t.Fatal(err)
	}
	// The four earlier misses are forgotten: it takes a full five to lock again.
	for i := 0; i < maxFailures-1; i++ {
		_, _ = Verify("sam", "modernwms", "000000")
	}
	if _, locked := Locked("sam", "modernwms"); locked {
		t.Fatal("a success must reset the failure count")
	}
	_, _ = Verify("sam", "modernwms", "000000")
	if _, locked := Locked("sam", "modernwms"); !locked {
		t.Fatal("five misses in a row must lock")
	}
	if err := Unlock("sam", "modernwms"); err != nil {
		t.Fatal(err)
	}
	if _, locked := Locked("sam", "modernwms"); locked {
		t.Fatal("Unlock must clear the lock")
	}
	advance(t, time.Minute)
	if _, err := Verify("sam", "modernwms", codeFor(t, secret)); err != nil {
		t.Fatalf("after unlock a correct code works: %v", err)
	}
	if err := Unlock("nobody", "modernwms"); !errors.Is(err, ErrNotEnrolled) {
		t.Errorf("Unlock of an unknown account = %v", err)
	}
}

func TestBackupCodesCountAsGuessesButResetOnSuccess(t *testing.T) {
	withTempStore(t)
	secret, _, _ := Enroll("bea", "modernwms")
	backups, err := Confirm("bea", "modernwms", codeFor(t, secret))
	if err != nil {
		t.Fatal(err)
	}
	advance(t, time.Minute)
	for i := 0; i < maxFailures-1; i++ {
		_, _ = Verify("bea", "modernwms", "NOTACODE")
	}
	used, err := Verify("bea", "modernwms", backups[0])
	if err != nil || !used {
		t.Fatalf("backup code = %v %v", used, err)
	}
	if _, err := Verify("bea", "modernwms", backups[0]); err == nil {
		t.Error("a backup code is single-use")
	}
}

// Sessions are separate processes; two goroutines racing the same wrong-code
// attempts must not lose counter updates (flock + in-process mutex).
func TestConcurrentWrongCodesAreAllCounted(t *testing.T) {
	enrolledUser(t, "cal")
	var wg sync.WaitGroup
	for i := 0; i < maxFailures; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = Verify("cal", "modernwms", "000000") }()
	}
	wg.Wait()
	if _, locked := Locked("cal", "modernwms"); !locked {
		t.Fatal("five concurrent misses must all be counted and lock the account")
	}
}

func TestSaveLeavesNoTempFilesAndKeepsMode0600(t *testing.T) {
	withTempStore(t)
	if _, _, err := Enroll("tmp", "modernwms"); err != nil {
		t.Fatal(err)
	}
	path := config.Get(config.TwoFAFile)
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0600 {
		t.Fatalf("store mode = %v, err %v", fi.Mode().Perm(), err)
	}
	left, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".2fa-*.tmp"))
	if len(left) != 0 {
		t.Errorf("temp files left behind: %v", left)
	}
}
