package backup

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"filippo.io/age"
)

// A ModernWMS backup holds every user's password hash, so it can be sealed
// with a passphrase (age's scrypt mode: authenticated encryption, so a wrong
// passphrase or a tampered file is rejected instead of yielding garbage). The
// format is the standard age one: `age -d file.age` decrypts it too.

// EncryptedSuffix marks a sealed backup.
const EncryptedSuffix = ".age"

// scryptWorkFactor is age's scrypt cost as a power of two; 0 keeps age's default
// (about a second per open, which is the point). Tests lower it.
var scryptWorkFactor = 0

var ErrEmptyPassphrase = errors.New("an empty passphrase would protect nothing")

// EncryptFile seals src into dst (created 0600, never overwritten) and returns
// the SHA-256 of the plaintext, so the caller can prove the result decrypts to
// the same bytes before deleting anything.
func EncryptFile(src, dst, passphrase string) (plainSHA string, err error) {
	if passphrase == "" {
		return "", ErrEmptyPassphrase
	}
	rcpt, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return "", err
	}
	if scryptWorkFactor > 0 {
		rcpt.SetWorkFactor(scryptWorkFactor)
	}
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	defer func() {
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			os.Remove(dst) // never leave a half-written "backup" behind
		}
	}()
	w, err := age.Encrypt(out, rcpt)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(w, h), in); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// DecryptFile opens a sealed backup into dst (created 0600, never overwritten)
// and returns the SHA-256 of what it wrote.
func DecryptFile(src, dst, passphrase string) (plainSHA string, err error) {
	if passphrase == "" {
		return "", ErrEmptyPassphrase
	}
	id, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return "", err
	}
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()
	r, err := age.Decrypt(in, id)
	if err != nil {
		return "", fmt.Errorf("cannot open %s (wrong passphrase, or not an age backup): %w", src, err)
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	defer func() {
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			os.Remove(dst)
		}
	}()
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(out, h), r); err != nil {
		return "", fmt.Errorf("the backup is damaged or was changed: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Seal encrypts the plaintext backup at path into path+".age", proves the sealed
// copy decrypts to the same bytes, and only then removes the plaintext. It
// returns the sealed file's path.
func Seal(path, passphrase string) (string, error) {
	sealed := path + EncryptedSuffix
	want, err := EncryptFile(path, sealed, passphrase)
	if err != nil {
		return "", err
	}
	check := sealed + ".verify"
	got, err := DecryptFile(sealed, check, passphrase)
	os.Remove(check)
	if err != nil || got != want {
		os.Remove(sealed)
		if err == nil {
			err = errors.New("the sealed copy did not decrypt to the same bytes")
		}
		return "", fmt.Errorf("sealing failed, plaintext backup kept: %w", err)
	}
	if err := os.Remove(path); err != nil {
		return sealed, fmt.Errorf("sealed, but could not remove the plaintext %s: %w", path, err)
	}
	return sealed, nil
}

// PlainName is the file name a sealed backup restores to ("x.db.age" -> "x.db").
func PlainName(sealed string) string {
	if strings.HasSuffix(sealed, EncryptedSuffix) {
		return strings.TrimSuffix(sealed, EncryptedSuffix)
	}
	return sealed + ".decrypted"
}
