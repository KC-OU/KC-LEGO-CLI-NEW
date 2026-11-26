package dashboardgrace

import (
	"os"
	"testing"
	"time"
)

func withTempStore(t *testing.T) {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "dashboard-grace-*.json")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	t.Setenv("DASHBOARD_GRACE_FILE", f.Name())
}

func TestMintThenCheckSucceedsForTheSameUser(t *testing.T) {
	withTempStore(t)
	token, expiresAt, err := Mint("user_abc", 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if token == "" {
		t.Fatal("expected a non-empty token")
	}
	if !expiresAt.After(time.Now()) {
		t.Fatalf("expected expiresAt in the future, got %v", expiresAt)
	}
	ok, checkedExpiresAt := Check(token, "user_abc")
	if !ok {
		t.Fatal("expected Check to succeed for the minted user")
	}
	if !checkedExpiresAt.Equal(expiresAt) {
		t.Fatalf("Check's expiresAt = %v, want it to match Mint's own %v exactly", checkedExpiresAt, expiresAt)
	}
}

func TestCheckFailsForADifferentUser(t *testing.T) {
	withTempStore(t)
	token, _, err := Mint("user_abc", 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := Check(token, "user_xyz"); ok {
		t.Fatal("expected Check to fail for a different clerk user id")
	}
}

func TestCheckFailsForAnUnknownToken(t *testing.T) {
	withTempStore(t)
	if ok, _ := Check("never-minted", "user_abc"); ok {
		t.Fatal("expected Check to fail for a token that was never minted")
	}
}

func TestCheckFailsForAnExpiredToken(t *testing.T) {
	withTempStore(t)
	token, _, err := Mint("user_abc", -1*time.Minute) // already expired
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := Check(token, "user_abc"); ok {
		t.Fatal("expected an already-expired token to fail")
	}
}

func TestCheckDoesNotConsumeTheToken(t *testing.T) {
	withTempStore(t)
	token, _, err := Mint("user_abc", 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := Check(token, "user_abc"); !ok {
		t.Fatal("first check should succeed")
	}
	if ok, _ := Check(token, "user_abc"); !ok {
		t.Fatal("a grace token should stay valid across repeated sign-ins within the window")
	}
}

func TestCheckRejectsEmptyInputs(t *testing.T) {
	withTempStore(t)
	if ok, _ := Check("", "user_abc"); ok {
		t.Fatal("an empty token must never match")
	}
	token, _, err := Mint("user_abc", 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := Check(token, ""); ok {
		t.Fatal("an empty clerk user id must never match")
	}
}

func TestCheckReturnsAZeroExpiryOnFailure(t *testing.T) {
	withTempStore(t)
	if _, expiresAt := Check("never-minted", "user_abc"); !expiresAt.IsZero() {
		t.Fatalf("expected a zero expiresAt on failure, got %v", expiresAt)
	}
}

func TestSaveSilentlyPrunesExpiredRecords(t *testing.T) {
	withTempStore(t)
	expired, _, err := Mint("user_old", -1*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	fresh, _, err := Mint("user_new", 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	records, err := load(storePath())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := records[hashToken(expired)]; ok {
		t.Fatal("expected the expired record to have been pruned on the next save")
	}
	if _, ok := records[hashToken(fresh)]; !ok {
		t.Fatal("expected the fresh record to still be present")
	}
}

func TestWindowMinutesDefaultsToTenWithNoSetting(t *testing.T) {
	t.Setenv("DASHBOARD_GRACE_MINUTES", "")
	if got := WindowMinutes(); got != 10 {
		t.Fatalf("WindowMinutes() = %d, want 10", got)
	}
}

func TestWindowMinutesReadsTheConfiguredValue(t *testing.T) {
	t.Setenv("DASHBOARD_GRACE_MINUTES", "7")
	if got := WindowMinutes(); got != 7 {
		t.Fatalf("WindowMinutes() = %d, want 7", got)
	}
}
