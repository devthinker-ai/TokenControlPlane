package session

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/base32"
	"encoding/hex"
	"fmt"
	"math/big"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	qrcode "github.com/skip2/go-qrcode"
)

// Recovery alphabet matches invite codes: unambiguous (no 0/O/1/l/I).
const recoveryAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789abcdefghjkmnpqrstuvwxyz"

const (
	totpDigits      = 6
	totpPeriod      = 30
	secretBytes     = 20
	recoveryLen     = 8
	recoveryCount   = 10
	issuerName      = "TokenControlPlane"
	mfaIssuer       = "tokencontrolplane-mfa"
	mfaTTL          = 5 * time.Minute
	mfaMaxAttempts  = 5
	mfaRetryAfterSec = 60
)

// GenerateSecret returns a 20-byte TOTP secret as base32 RFC 4648 uppercase, no padding
// (pquerna/otp's encoding).
func GenerateSecret() (string, error) {
	b := make([]byte, secretBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b), nil
}

// ProvisioningURI builds an otpauth:// URI for authenticator apps.
func ProvisioningURI(secret, email, accountName string) string {
	_ = accountName // reserved for future display; issuer+email is the standard label
	label := issuerName + ":" + email
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", issuerName)
	q.Set("digits", fmt.Sprintf("%d", totpDigits))
	q.Set("period", fmt.Sprintf("%d", totpPeriod))
	q.Set("algorithm", "SHA1")
	return "otpauth://totp/" + url.PathEscape(label) + "?" + q.Encode()
}

// QRDataURL returns a PNG data URL (~240px) for the provisioning URI.
func QRDataURL(uri string) (string, error) {
	png, err := qrcode.Encode(uri, qrcode.Medium, 240)
	if err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png), nil
}

// Verify checks a 6-digit TOTP code against secret at now (±1 window).
// Wrong-length / invalid input returns false, nil (not an error).
func Verify(secret, code string, now time.Time) (bool, error) {
	code = strings.TrimSpace(code)
	if len(code) != totpDigits {
		return false, nil
	}
	for _, c := range code {
		if c < '0' || c > '9' {
			return false, nil
		}
	}
	ok, err := totp.ValidateCustom(code, secret, now.UTC(), totp.ValidateOpts{
		Period:    totpPeriod,
		Skew:      1,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		// Invalid secret encoding etc. — treat as not ok without surfacing.
		return false, nil
	}
	return ok, nil
}

// RecoveryCode returns one plaintext recovery code in xxxx-xxxx form.
func RecoveryCode() (string, error) {
	b := make([]byte, recoveryLen)
	max := big.NewInt(int64(len(recoveryAlphabet)))
	for i := 0; i < recoveryLen; i++ {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b[i] = recoveryAlphabet[n.Int64()]
	}
	return string(b[:4]) + "-" + string(b[4:]), nil
}

// GenerateRecoveryCodes returns recoveryCount plaintext codes.
func GenerateRecoveryCodes() ([]string, error) {
	out := make([]string, recoveryCount)
	for i := 0; i < recoveryCount; i++ {
		c, err := RecoveryCode()
		if err != nil {
			return nil, err
		}
		out[i] = c
	}
	return out, nil
}

// HashRecoveryCode returns sha256 hex of the trimmed code.
func HashRecoveryCode(code string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(code)))
	return hex.EncodeToString(sum[:])
}

// LooksLikeRecoveryCode reports whether code matches xxxx-xxxx (8 alnum + hyphen).
func LooksLikeRecoveryCode(code string) bool {
	code = strings.TrimSpace(code)
	if len(code) != 9 || code[4] != '-' {
		return false
	}
	for i, c := range code {
		if i == 4 {
			continue
		}
		ok := (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '2' && c <= '9')
		if !ok {
			return false
		}
	}
	return true
}

// MFAClaims is a short-lived pre-session token after password OK + 2FA enrolled.
// Deliberately separate from session.Claims — do not mix.
type MFAClaims struct {
	UserID string `json:"mfa"`
	jwt.RegisteredClaims
}

// MFAGate tracks single-use jti + attempt counters for mfa_tokens (in-memory).
type MFAGate struct {
	mu       sync.Mutex
	used     map[string]time.Time // jti → exp
	attempts map[string]int       // jti → count
	now      func() time.Time
}

// NewMFAGate creates an empty gate. Optional now injector for tests.
func NewMFAGate(now func() time.Time) *MFAGate {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	g := &MFAGate{
		used:     make(map[string]time.Time),
		attempts: make(map[string]int),
		now:      now,
	}
	return g
}

func (g *MFAGate) purge(now time.Time) {
	for jti, exp := range g.used {
		if now.After(exp) {
			delete(g.used, jti)
			delete(g.attempts, jti)
		}
	}
}

// IssueMFA returns a 5-minute HS256 JWT with claim mfa:<user_id> and a fresh jti.
func (m *Manager) IssueMFA(userID string, now time.Time) (token string, jti string, err error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	jtiBytes := make([]byte, 16)
	if _, err := rand.Read(jtiBytes); err != nil {
		return "", "", err
	}
	jti = hex.EncodeToString(jtiBytes)
	claims := MFAClaims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    mfaIssuer,
			Subject:   userID,
			ID:        jti,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(mfaTTL)),
		},
	}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := t.SignedString(m.secret)
	return signed, jti, err
}

// ParseMFA validates signature/exp/issuer. Does not check jti single-use.
func (m *Manager) ParseMFA(tokenStr string, now time.Time) (*MFAClaims, error) {
	tok, err := jwt.ParseWithClaims(tokenStr, &MFAClaims{}, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return m.secret, nil
	}, jwt.WithTimeFunc(func() time.Time {
		if now.IsZero() {
			return time.Now().UTC()
		}
		return now
	}))
	if err != nil {
		return nil, err
	}
	claims, ok := tok.Claims.(*MFAClaims)
	if !ok || !tok.Valid {
		return nil, fmt.Errorf("invalid mfa token")
	}
	if claims.Issuer != mfaIssuer || claims.UserID == "" || claims.ID == "" {
		return nil, fmt.Errorf("invalid mfa token")
	}
	return claims, nil
}

// CheckAndCountAttempt returns (allowed, remaining). On 6th attempt returns false.
// Caller should respond 429 when !allowed.
func (g *MFAGate) CheckAndCountAttempt(jti string, exp time.Time) (allowed bool, n int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now()
	g.purge(now)
	if exp.Before(now) {
		return false, mfaMaxAttempts
	}
	g.attempts[jti]++
	n = g.attempts[jti]
	if n > mfaMaxAttempts {
		return false, n
	}
	return true, n
}

// MarkUsed records jti as consumed until exp. Returns false if already used or expired.
func (g *MFAGate) MarkUsed(jti string, exp time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now()
	g.purge(now)
	if !exp.After(now) {
		return false
	}
	if _, ok := g.used[jti]; ok {
		return false
	}
	g.used[jti] = exp
	return true
}

// IsUsed reports whether jti was already consumed.
func (g *MFAGate) IsUsed(jti string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now()
	g.purge(now)
	_, ok := g.used[jti]
	return ok
}

// MFAMaxAttempts and MFARetryAfterSec are exported for handlers/tests.
const (
	MFAMaxAttempts   = mfaMaxAttempts
	MFARetryAfterSec = mfaRetryAfterSec
)
