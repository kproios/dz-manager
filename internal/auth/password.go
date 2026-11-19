package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// argon2id parameters (RFC 9106 second recommendation, 64 MiB).
const (
	argonTime    = 3
	argonMemory  = 64 * 1024
	argonThreads = 2
	argonKeyLen  = 32
)

// MinPasswordLen is the shortest password accepted.
const MinPasswordLen = 12

// CheckPasswordPolicy returns a message when a new password is not acceptable.
func CheckPasswordPolicy(pw, login string) error {
	n := utf8.RuneCountInString(pw)
	if n < MinPasswordLen {
		return fmt.Errorf("use at least %d characters (a few words are easiest)", MinPasswordLen)
	}
	if n > 256 {
		return errors.New("use at most 256 characters")
	}
	if login != "" && strings.Contains(strings.ToLower(pw), strings.ToLower(login)) {
		return errors.New("the password must not contain the user name")
	}
	if strings.Count(pw, string([]rune(pw)[0])) == n {
		return errors.New("the password is one character repeated")
	}
	return nil
}

// HashPassword returns an argon2id PHC string.
func HashPassword(pw string) string {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		panic(err)
	}
	key := argon2.IDKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMemory, argonTime, argonThreads, b64.EncodeToString(salt), b64.EncodeToString(key))
}

// dummyHash is verified when the user does not exist, so timing doesn't tell.
var dummyHash = HashPassword("dz-manager-dummy-password")

// VerifyPassword checks pw against a PHC string from HashPassword.
func VerifyPassword(pw, phc string) bool {
	if phc == "" {
		verify(pw, dummyHash)
		return false
	}
	return verify(pw, phc)
}

func verify(pw, phc string) bool {
	parts := strings.Split(phc, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil || m > 1<<20 || t > 20 || p == 0 {
		return false
	}
	b64 := base64.RawStdEncoding
	salt, err1 := b64.DecodeString(parts[4])
	want, err2 := b64.DecodeString(parts[5])
	if err1 != nil || err2 != nil || len(want) == 0 {
		return false
	}
	got := argon2.IDKey([]byte(pw), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}
