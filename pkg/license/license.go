// Package license validates offline RS256 license JWTs (self-hosted monetization).
//
// Fail-open: invalid/expired/missing keys never take down the gateway — they
// fall back to free-tier caps. No phone-home in v1 (honor system).
package license

import (
	"crypto/rsa"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

//go:embed license_pub.pem
var pubPEM embed.FS

const (
	PlanFree = "free"
	PlanPro  = "pro"
	PlanTeam = "team"

	// FreeMonthlyTokens is the unlicensed / free-tier monthly token budget (bytes/4).
	FreeMonthlyTokens int64 = 5_000_000
)

// Caps are plan limits applied to new accounts and createServer/createKey checks.
// MonthlyTokens 0 = unlimited tokens (enforcement already treats 0 as no cap).
type Caps struct {
	Plan          string
	Subject       string
	MaxSeats      int
	MaxServers    int
	MaxKeys       int
	MonthlyTokens int64
	ExpiresAt     time.Time // zero when unlicensed
	Licensed      bool      // true only when a valid key was accepted
	ToolPolicy    bool      // Pro/Team: per-tool + per-key allowlists
}

// Claims is the JWT payload for a signed license key.
type Claims struct {
	Plan       string `json:"plan"`
	MaxSeats   int    `json:"max_seats"`
	MaxServers int    `json:"max_servers"`
	MaxKeys    int    `json:"max_keys"`
	jwt.RegisteredClaims
}

// FreeCaps returns unlicensed free-tier limits (3 servers / 3 seats / 5M tokens).
func FreeCaps() Caps {
	return Caps{
		Plan:          PlanFree,
		MaxSeats:      3,
		MaxServers:    3,
		MaxKeys:       5,
		MonthlyTokens: FreeMonthlyTokens,
		Licensed:      false,
		ToolPolicy:    false,
	}
}

// CapsForPlan maps SaaS/self-hosted plan names to caps (one code path).
// Paid plans: MonthlyTokens=0 (unlimited tokens). Free keeps 5M.
func CapsForPlan(plan string) Caps {
	switch strings.ToLower(plan) {
	case PlanPro:
		return Caps{Plan: PlanPro, MaxSeats: 10, MaxServers: 10, MaxKeys: 20, MonthlyTokens: 0, ToolPolicy: true}
	case PlanTeam:
		return Caps{Plan: PlanTeam, MaxSeats: 25, MaxServers: 25, MaxKeys: 50, MonthlyTokens: 0, ToolPolicy: true}
	default:
		return FreeCaps()
	}
}

// Validate verifies RS256 signature with the embedded public key and checks exp.
func Validate(token string) (Claims, error) {
	pub, err := loadPublicKey()
	if err != nil {
		return Claims{}, err
	}
	return ValidateWithKey(token, pub)
}

// ValidateWithKey is used by tests (ephemeral keys) and Validate.
func ValidateWithKey(token string, pub *rsa.PublicKey) (Claims, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return Claims{}, errors.New("empty license key")
	}
	parsed, err := jwt.ParseWithClaims(token, &Claims{}, func(t *jwt.Token) (any, error) {
		if t.Method.Alg() != jwt.SigningMethodRS256.Alg() {
			return nil, fmt.Errorf("unexpected alg %s", t.Method.Alg())
		}
		return pub, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}))
	if err != nil {
		return Claims{}, err
	}
	claims, ok := parsed.Claims.(*Claims)
	if !ok || !parsed.Valid {
		return Claims{}, errors.New("invalid claims")
	}
	if claims.Plan == "" {
		claims.Plan = PlanPro
	}
	return *claims, nil
}

// Sign creates a license JWT (CLI / tests). Private key never ships in the binary.
func Sign(priv *rsa.PrivateKey, claims Claims) (string, error) {
	if claims.IssuedAt == nil {
		claims.IssuedAt = jwt.NewNumericDate(time.Now().UTC())
	}
	if claims.ExpiresAt == nil {
		claims.ExpiresAt = jwt.NewNumericDate(time.Now().UTC().Add(365 * 24 * time.Hour))
	}
	t := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	return t.SignedString(priv)
}

// Resolve reads TOKENCONTROLPLANE_LICENSE_KEY (or key arg), validates, and fail-opens to free.
// Logs the startup banner / warning to log.
func Resolve(log *slog.Logger, key string) Caps {
	if log == nil {
		log = slog.Default()
	}
	if key == "" {
		key = os.Getenv("TOKENCONTROLPLANE_LICENSE_KEY")
	}
	if strings.TrimSpace(key) == "" {
		caps := FreeCaps()
		log.Warn("TokenControlPlane — unlicensed free tier (3 servers). Set TOKENCONTROLPLANE_LICENSE_KEY for more.")
		return caps
	}
	claims, err := Validate(key)
	if err != nil {
		// Fail open: broken key must never take down an air-gapped gateway.
		log.Error("LICENSE KEY INVALID OR EXPIRED — falling back to free tier", "err", err)
		return FreeCaps()
	}
	return capsFromClaims(log, claims)
}

// ResolveWithKey is Resolve for tests (ephemeral RSA keys) — same fail-open rules.
func ResolveWithKey(log *slog.Logger, key string, pub *rsa.PublicKey) Caps {
	if log == nil {
		log = slog.Default()
	}
	if strings.TrimSpace(key) == "" {
		return FreeCaps()
	}
	claims, err := ValidateWithKey(key, pub)
	if err != nil {
		log.Error("LICENSE KEY INVALID OR EXPIRED — falling back to free tier", "err", err)
		return FreeCaps()
	}
	return capsFromClaims(log, claims)
}

func capsFromClaims(log *slog.Logger, claims Claims) Caps {
	base := CapsForPlan(claims.Plan)
	caps := Caps{
		Plan:          claims.Plan,
		Subject:       claims.Subject,
		MaxSeats:      pick(claims.MaxSeats, base.MaxSeats),
		MaxServers:    pick(claims.MaxServers, base.MaxServers),
		MaxKeys:       pick(claims.MaxKeys, base.MaxKeys),
		MonthlyTokens: base.MonthlyTokens,
		Licensed:      true,
		ToolPolicy:    base.ToolPolicy,
	}
	if claims.ExpiresAt != nil {
		caps.ExpiresAt = claims.ExpiresAt.Time
	}
	log.Info("TokenControlPlane — licensed",
		"plan", caps.Plan,
		"subject", caps.Subject,
		"max_servers", caps.MaxServers,
		"expires", caps.ExpiresAt.UTC().Format(time.RFC3339),
	)
	return caps
}

func pick(n, fallback int) int {
	if n > 0 {
		return n
	}
	return fallback
}

func loadPublicKey() (*rsa.PublicKey, error) {
	b, err := pubPEM.ReadFile("license_pub.pem")
	if err != nil {
		return nil, err
	}
	return ParseRSAPublicKey(b)
}
