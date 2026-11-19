package auth

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTOTPRFC6238(t *testing.T) {
	// RFC 6238 appendix B, SHA-1 secret "12345678901234567890" (last 6 digits).
	const sec = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	for ts, want := range map[int64]string{59: "287082", 1111111109: "081804", 1234567890: "005924", 2000000000: "279037"} {
		if got := TOTPAt(sec, time.Unix(ts, 0)); got != want {
			t.Errorf("T=%d: %s, want %s", ts, got, want)
		}
	}
	now := time.Unix(1234567890, 0)
	step, ok := VerifyTOTP(sec, "005 924", now, 0)
	if !ok {
		t.Fatal("valid code refused")
	}
	if _, ok := VerifyTOTP(sec, "005924", now, step); ok {
		t.Fatal("reused code accepted")
	}
	if _, ok := VerifyTOTP(sec, TOTPAt(sec, now.Add(-30*time.Second)), now, 0); !ok {
		t.Fatal("one step of drift refused")
	}
	if _, ok := VerifyTOTP(sec, TOTPAt(sec, now.Add(-90*time.Second)), now, 0); ok {
		t.Fatal("three steps of drift accepted")
	}
}

func TestPasswords(t *testing.T) {
	h := HashPassword("a long enough password")
	if !strings.HasPrefix(h, "$argon2id$v=19$m=65536,t=3,p=2$") {
		t.Fatalf("hash format %s", h)
	}
	if !VerifyPassword("a long enough password", h) || VerifyPassword("a long enough passwore", h) || VerifyPassword("x", "") {
		t.Fatal("verify wrong")
	}
	for pw, ok := range map[string]bool{"short": false, "aaaaaaaaaaaaaaaa": false, "Steve's password!": false, "correct horse battery": true} {
		if err := CheckPasswordPolicy(pw, "steve"); (err == nil) != ok {
			t.Errorf("%q: %v", pw, err)
		}
	}
}

func TestRecoveryCodes(t *testing.T) {
	c := NewRecoveryCodes(10)
	if len(c) != 10 || len(c[0]) != 14 || c[0] == c[1] {
		t.Fatalf("codes %v", c)
	}
	if normRecovery(" "+strings.ToUpper(c[0])+" ") != strings.ReplaceAll(c[0], "-", "") {
		t.Fatal("normalising")
	}
}

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "127.0.0.1:5000"
	r.Header.Set("CF-Connecting-IP", "203.0.113.9")
	if ClientIP(r) != "203.0.113.9" {
		t.Fatal("proxy header from a local proxy ignored")
	}
	r.RemoteAddr = "198.51.100.7:5000"
	if ClientIP(r) != "198.51.100.7" {
		t.Fatal("proxy header trusted from a public peer")
	}
}

func TestQR(t *testing.T) {
	uri := TOTPURI(Issuer, "Steve", NewTOTPSecret())
	if !strings.HasPrefix(uri, "otpauth://totp/DZ%20Manager:Steve?") {
		t.Fatalf("uri %s", uri)
	}
	d, err := QRDataURI(uri)
	if err != nil || !strings.HasPrefix(d, "data:image/png;base64,") {
		t.Fatal(err)
	}
}
