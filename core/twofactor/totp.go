package twofactor

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6238's default, the one every authenticator app uses
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Authenticator codes as the apps make them (RFC 6238 TOTP): HMAC-SHA1 over 30-second steps, 6 digits.
const (
	period = 30
	digits = 6
	// steps either side of now that are accepted too: the phone's clock and the server's drift apart
	skew = 1
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewSecret is a new authenticator secret, base32 as the apps take it (160 bits, RFC 4226's recommendation).
func NewSecret() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return b32.EncodeToString(b), nil
}

// codeAt is the code for one time step.
func codeAt(secret []byte, step int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	mac := hmac.New(sha1.New, secret)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", digits, value%1_000_000)
}

// Code is the code an authenticator app shows for the secret at the given time.
func Code(secret string, at time.Time) (string, error) {
	key, err := b32.DecodeString(strings.ToUpper(secret))
	if err != nil {
		return "", err
	}
	return codeAt(key, at.Unix()/period), nil
}

// matchStep is the time step the code belongs to, now or one step either side, or -1 when it belongs to none.
func matchStep(secret, code string, now time.Time) int64 {
	code = normalizeCode(code)
	if len(code) != digits {
		return -1
	}
	key, err := b32.DecodeString(strings.ToUpper(secret))
	if err != nil {
		return -1
	}
	current := now.Unix() / period
	for d := int64(-skew); d <= skew; d++ {
		if subtle.ConstantTimeCompare([]byte(codeAt(key, current+d)), []byte(code)) == 1 {
			return current + d
		}
	}
	return -1
}

// normalizeCode drops what people type around the digits ("123 456").
func normalizeCode(code string) string {
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '-' {
			return -1
		}
		return r
	}, strings.TrimSpace(code))
}

// URI is the otpauth:// link a QR code carries to an authenticator app.
func URI(issuer, account, secret string) string {
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprint(digits))
	q.Set("period", fmt.Sprint(period))
	return "otpauth://totp/" + url.PathEscape(issuer+":"+account) + "?" + q.Encode()
}
