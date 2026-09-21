package auth

import "testing"

func TestHashModernWMS(t *testing.T) {
	// known MD5("password") vector
	got := HashModernWMS("password")
	want := "5f4dcc3b5aa765d61d8327deb882cf99"
	if got != want {
		t.Errorf("HashModernWMS = %q, want %q", got, want)
	}
}

func TestVerifyModernWMS(t *testing.T) {
	if !VerifyModernWMS("password", "5f4dcc3b5aa765d61d8327deb882cf99") {
		t.Error("expected MD5 match to verify")
	}
	if !VerifyModernWMS("password", "password") {
		t.Error("expected raw plaintext match to verify")
	}
	if VerifyModernWMS("password", "wrong") {
		t.Error("expected mismatch to fail")
	}
}

func TestPartDBBcryptRoundTrip(t *testing.T) {
	hash, err := HashPartDB("s3cret!")
	if err != nil {
		t.Fatalf("HashPartDB: %v", err)
	}
	if !VerifyPartDB("s3cret!", hash) {
		t.Error("expected correct password to verify")
	}
	if VerifyPartDB("wrong", hash) {
		t.Error("expected wrong password to fail")
	}
}

func TestPBKDF2RoundTrip(t *testing.T) {
	key, salt := HashPBKDF2("adminpass", nil)
	if !VerifyPBKDF2("adminpass", key, salt) {
		t.Error("expected correct password to verify")
	}
	if VerifyPBKDF2("wrongpass", key, salt) {
		t.Error("expected wrong password to fail")
	}
}

func TestGenerateTempPassword(t *testing.T) {
	for i := 0; i < 20; i++ {
		pw := GenerateTempPassword(10)
		if len(pw) != 10 {
			t.Fatalf("expected length 10, got %d (%q)", len(pw), pw)
		}
		upper, lower, digit, symbol := classesPresent(pw)
		if !upper || !lower || !digit || !symbol {
			t.Fatalf("password %q missing a character class: upper=%v lower=%v digit=%v symbol=%v", pw, upper, lower, digit, symbol)
		}
	}

	if pw := GenerateTempPassword(3); len(pw) != 8 {
		t.Errorf("expected minimum length 8, got %d", len(pw))
	}
}
