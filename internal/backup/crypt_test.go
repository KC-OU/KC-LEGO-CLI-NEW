package backup

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func init() { scryptWorkFactor = 10 }

func writeSample(t *testing.T) (dir, path string, data []byte) {
	t.Helper()
	dir = t.TempDir()
	path = filepath.Join(dir, "modernwms_db_x.db")
	data = bytes.Repeat([]byte("password-hash-row\n"), 5000)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return
}

func TestSealRoundTripRemovesThePlaintextAndKeepsMode0600(t *testing.T) {
	_, path, data := writeSample(t)
	sealed, err := Seal(path, "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if sealed != path+".age" {
		t.Fatalf("sealed = %s", sealed)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("the plaintext backup must be gone after a verified seal")
	}
	if fi, _ := os.Stat(sealed); fi.Mode().Perm() != 0600 {
		t.Errorf("sealed mode = %v", fi.Mode().Perm())
	}
	raw, _ := os.ReadFile(sealed)
	if bytes.Contains(raw, []byte("password-hash-row")) {
		t.Fatal("the sealed file contains plaintext")
	}
	restored := filepath.Join(t.TempDir(), "restored.db")
	if _, err := DecryptFile(sealed, restored, "correct horse"); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(restored); !bytes.Equal(got, data) {
		t.Fatal("decrypted bytes differ from the original")
	}
}

func TestWrongPassphraseAndTamperingAreRejected(t *testing.T) {
	_, path, _ := writeSample(t)
	sealed, err := Seal(path, "right")
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "o.db")
	if _, err := DecryptFile(sealed, out, "wrong"); err == nil {
		t.Fatal("a wrong passphrase must fail")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Error("a failed decrypt must not leave an output file")
	}
	raw, _ := os.ReadFile(sealed)
	raw[len(raw)-20] ^= 0xFF
	tampered := filepath.Join(t.TempDir(), "t.age")
	os.WriteFile(tampered, raw, 0600)
	if _, err := DecryptFile(tampered, out, "right"); err == nil {
		t.Fatal("a modified backup must be detected")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Error("a damaged decrypt must not leave a partial file")
	}
}

func TestEmptyPassphraseAndOverwriteAreRefused(t *testing.T) {
	_, path, _ := writeSample(t)
	if _, err := Seal(path, ""); err == nil {
		t.Fatal("an empty passphrase must be refused")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("a refused seal must leave the plaintext alone")
	}
	if err := os.WriteFile(path+".age", []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Seal(path, "pw"); err == nil {
		t.Fatal("Seal must not overwrite an existing file")
	}
	if b, _ := os.ReadFile(path + ".age"); string(b) != "existing" {
		t.Error("the existing file was clobbered")
	}
	if _, err := os.Stat(path); err != nil {
		t.Error("the plaintext must survive a failed seal")
	}
}

func TestPlainName(t *testing.T) {
	if PlainName("a/b.db.age") != "a/b.db" || PlainName("odd") != "odd.decrypted" {
		t.Error("PlainName")
	}
}
