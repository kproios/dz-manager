package secret

import (
	"bytes"
	"errors"
	"testing"
)

func TestRoundTripAndWrongKey(t *testing.T) {
	a, _ := New(bytes.Repeat([]byte{1}, 32))
	b, _ := New(bytes.Repeat([]byte{2}, 32))
	ct := a.Seal([]byte("rcon-pass"), []byte("row-1"))
	if pt, err := a.Open(ct, []byte("row-1")); err != nil || string(pt) != "rcon-pass" {
		t.Fatalf("round trip: %v %q", err, pt)
	}
	if _, err := b.Open(ct, []byte("row-1")); !errors.Is(err, ErrDecrypt) {
		t.Fatal("wrong key must fail")
	}
	if _, err := a.Open(ct, []byte("row-2")); !errors.Is(err, ErrDecrypt) {
		t.Fatal("wrong aad must fail")
	}
	ct[len(ct)-1] ^= 1
	if _, err := a.Open(ct, []byte("row-1")); !errors.Is(err, ErrDecrypt) {
		t.Fatal("tampered ciphertext must fail")
	}
	if a.Hash("x") == b.Hash("x") || a.Hash("x") != a.Hash("x") {
		t.Fatal("hash must be keyed and deterministic")
	}
	if _, err := New(make([]byte, 31)); err == nil {
		t.Fatal("short key accepted")
	}
}
