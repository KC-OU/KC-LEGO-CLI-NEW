// Package auth implements the three password schemes the original suite
// used side by side: MD5 for ModernWMS's own auth_string column, bcrypt for
// Part-DB (done natively here instead of shelling out to PHP), and
// PBKDF2-HMAC-SHA256 for the sync dashboard's credentials.json, plus the
// shared temp-password generator.
package auth

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"math/big"
	"strings"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/pbkdf2"
)

func HashModernWMS(password string) string {
	sum := md5.Sum([]byte(password))
	return hex.EncodeToString(sum[:])
}

// VerifyModernWMS matches the original's permissive login check, which
// compares a stored auth_string against both the raw password and its MD5
// hex digest.
func VerifyModernWMS(password, storedAuthString string) bool {
	return password == storedAuthString || HashModernWMS(password) == storedAuthString
}

func HashPartDB(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func VerifyPartDB(password, hash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

const pbkdf2Iterations = 100_000
const pbkdf2KeyLen = 32

// HashPBKDF2 matches sync_service.py/set_password.py exactly: SHA-256,
// 100,000 rounds, a random 16-byte salt if none is supplied.
func HashPBKDF2(password string, salt []byte) (keyHex, saltHex string) {
	if salt == nil {
		salt = make([]byte, 16)
		if _, err := rand.Read(salt); err != nil {
			panic(err) // crypto/rand failing means the system RNG is broken
		}
	}
	key := pbkdf2.Key([]byte(password), salt, pbkdf2Iterations, pbkdf2KeyLen, sha256.New)
	return hex.EncodeToString(key), hex.EncodeToString(salt)
}

func VerifyPBKDF2(password, keyHex, saltHex string) bool {
	salt, err := hex.DecodeString(saltHex)
	if err != nil {
		return false
	}
	gotKeyHex, _ := HashPBKDF2(password, salt)
	return hmac.Equal([]byte(gotKeyHex), []byte(keyHex))
}

const tempPasswordSymbols = "!@#$%&*?"
const tempPasswordUpper = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
const tempPasswordLower = "abcdefghijklmnopqrstuvwxyz"
const tempPasswordDigits = "0123456789"

// GenerateTempPassword produces a cryptographically random password
// guaranteeing at least one upper, lower, digit, and symbol character,
// matching the original's generate_temp_password. Minimum length is 8
// regardless of a smaller requested length.
func GenerateTempPassword(length int) string {
	if length < 8 {
		length = 8
	}
	all := tempPasswordUpper + tempPasswordLower + tempPasswordDigits + tempPasswordSymbols

	chars := make([]byte, length)
	chars[0] = randChar(tempPasswordUpper)
	chars[1] = randChar(tempPasswordLower)
	chars[2] = randChar(tempPasswordDigits)
	chars[3] = randChar(tempPasswordSymbols)
	for i := 4; i < length; i++ {
		chars[i] = randChar(all)
	}

	shuffle(chars)
	return string(chars)
}

func randChar(set string) byte {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(set))))
	if err != nil {
		panic(err)
	}
	return set[n.Int64()]
}

func shuffle(b []byte) {
	for i := len(b) - 1; i > 0; i-- {
		j, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			panic(err)
		}
		b[i], b[int(j.Int64())] = b[int(j.Int64())], b[i]
	}
}

// classesPresent is a small test helper for asserting character-class
// coverage without exporting the character sets themselves.
func classesPresent(password string) (upper, lower, digit, symbol bool) {
	for _, r := range password {
		switch {
		case strings.ContainsRune(tempPasswordUpper, r):
			upper = true
		case strings.ContainsRune(tempPasswordLower, r):
			lower = true
		case strings.ContainsRune(tempPasswordDigits, r):
			digit = true
		case strings.ContainsRune(tempPasswordSymbols, r):
			symbol = true
		}
	}
	return
}
