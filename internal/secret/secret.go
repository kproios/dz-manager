// Package secret encrypts small values at rest with AES-256-GCM using the
// host-held DZM_SECRET_KEY. A DB backup without the key reveals nothing.
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
)

// ErrDecrypt is returned when a value cannot be opened (wrong key or tampering).
var ErrDecrypt = errors.New("secret: cannot decrypt (wrong DZM_SECRET_KEY or corrupted value)")

// Box seals and opens values.
type Box struct {
	aead cipher.AEAD
	mac  []byte
}

// New creates a Box from a 32-byte key.
func New(key []byte) (*Box, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("secret: key must be 32 bytes, got %d", len(key))
	}
	enc := derive(key, "dzm/aead")
	block, err := aes.NewCipher(enc)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead, mac: derive(key, "dzm/mac")}, nil
}

func derive(key []byte, label string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(label))
	return m.Sum(nil)
}

// Seal encrypts plaintext; aad binds the ciphertext to its context (e.g. a row id).
func (b *Box) Seal(plaintext, aad []byte) []byte {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		panic(err)
	}
	return b.aead.Seal(nonce, nonce, plaintext, aad)
}

// Open decrypts a value produced by Seal with the same aad.
func (b *Box) Open(ciphertext, aad []byte) ([]byte, error) {
	ns := b.aead.NonceSize()
	if len(ciphertext) < ns {
		return nil, ErrDecrypt
	}
	pt, err := b.aead.Open(nil, ciphertext[:ns], ciphertext[ns:], aad)
	if err != nil {
		return nil, ErrDecrypt
	}
	return pt, nil
}

// SealString is Seal for strings, returning base64 for cookie/text storage.
func (b *Box) SealString(plaintext string, aad string) string {
	return base64.RawURLEncoding.EncodeToString(b.Seal([]byte(plaintext), []byte(aad)))
}

// OpenString reverses SealString.
func (b *Box) OpenString(enc string, aad string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(enc)
	if err != nil {
		return "", ErrDecrypt
	}
	pt, err := b.Open(raw, []byte(aad))
	return string(pt), err
}

// Hash returns a keyed, hex-encoded hash for lookups of tokens without storing
// them in clear (HMAC-SHA256 so a stolen DB can't be brute-forced offline
// without the key).
func (b *Box) Hash(value string) string {
	m := hmac.New(sha256.New, b.mac)
	m.Write([]byte(value))
	return hex.EncodeToString(m.Sum(nil))
}

// Token returns n random bytes encoded as URL-safe base64.
func Token(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

// UUID returns a random RFC 4122 version 4 UUID.
func UUID() string {
	var u [16]byte
	if _, err := rand.Read(u[:]); err != nil {
		panic(err)
	}
	u[6] = (u[6] & 0x0f) | 0x40
	u[8] = (u[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", u[0:4], u[4:6], u[6:8], u[8:10], u[10:16])
}

// Equal compares two strings in constant time.
func Equal(a, b string) bool { return hmac.Equal([]byte(a), []byte(b)) }
