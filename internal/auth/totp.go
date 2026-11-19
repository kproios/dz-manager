package auth

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/url"
	"strings"
	"time"

	"rsc.io/qr"
)

// TOTP (RFC 6238): SHA-1, 30-second steps, 6 digits, which every
// authenticator app supports. One step of clock drift is accepted each way.
const (
	totpStep   = 30
	totpDigits = 6
	totpSkew   = 1
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewTOTPSecret returns a random 160-bit secret in base32.
func NewTOTPSecret() string {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b32.EncodeToString(b)
}

func totpCode(secret []byte, step int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	m := hmac.New(sha1.New, secret)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", totpDigits, v%1_000_000)
}

// TOTPAt returns the code for a time (tests and enrolment checks).
func TOTPAt(secretB32 string, t time.Time) string {
	s, err := b32.DecodeString(strings.ToUpper(secretB32))
	if err != nil {
		return ""
	}
	return totpCode(s, t.Unix()/totpStep)
}

// VerifyTOTP checks a code and returns its time step. A step at or before
// lastStep is refused, so a code can never be used twice.
func VerifyTOTP(secretB32, code string, now time.Time, lastStep int64) (int64, bool) {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) != totpDigits {
		return 0, false
	}
	s, err := b32.DecodeString(strings.ToUpper(secretB32))
	if err != nil {
		return 0, false
	}
	cur := now.Unix() / totpStep
	for d := int64(-totpSkew); d <= totpSkew; d++ {
		st := cur + d
		if st <= lastStep {
			continue
		}
		if hmac.Equal([]byte(totpCode(s, st)), []byte(code)) {
			return st, true
		}
	}
	return 0, false
}

// TOTPURI is the otpauth:// link authenticator apps read from the QR code.
func TOTPURI(issuer, account, secretB32 string) string {
	label := url.PathEscape(issuer + ":" + account)
	q := url.Values{"secret": {secretB32}, "issuer": {issuer}, "algorithm": {"SHA1"}, "digits": {"6"}, "period": {"30"}}
	return "otpauth://totp/" + label + "?" + q.Encode()
}

// QRDataURI renders text as a PNG QR code in a data: URI (the page's CSP
// allows data: images; nothing is sent to a third-party QR service).
func QRDataURI(text string) (string, error) {
	c, err := qr.Encode(text, qr.M)
	if err != nil {
		return "", err
	}
	const scale, quiet = 6, 4
	n := c.Size
	img := image.NewGray(image.Rect(0, 0, (n+2*quiet)*scale, (n+2*quiet)*scale))
	for i := range img.Pix {
		img.Pix[i] = 0xff
	}
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			if !c.Black(x, y) {
				continue
			}
			for dy := 0; dy < scale; dy++ {
				for dx := 0; dx < scale; dx++ {
					img.SetGray((x+quiet)*scale+dx, (y+quiet)*scale+dy, color.Gray{})
				}
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

// NewRecoveryCodes returns n one-time codes like "k7m2-9qxd-3hfa".
func NewRecoveryCodes(n int) []string {
	const alphabet = "23456789abcdefghjkmnpqrstuvwxyz"
	out := make([]string, n)
	for i := range out {
		b := make([]byte, 12)
		if _, err := rand.Read(b); err != nil {
			panic(err)
		}
		var sb strings.Builder
		for j, c := range b {
			if j > 0 && j%4 == 0 {
				sb.WriteByte('-')
			}
			sb.WriteByte(alphabet[int(c)%len(alphabet)])
		}
		out[i] = sb.String()
	}
	return out
}

// normRecovery makes a typed recovery code comparable.
func normRecovery(s string) string {
	return strings.ToLower(strings.NewReplacer(" ", "", "-", "").Replace(strings.TrimSpace(s)))
}
