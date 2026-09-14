package license_test

import (
	"crypto/rand"
	"crypto/rsa"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/devthinker-ai/TokenControlPlane/pkg/license"
)

func TestSignValidateRoundTrip(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tok, err := license.Sign(priv, license.Claims{
		Plan:       license.PlanPro,
		MaxSeats:   10,
		MaxServers: 10,
		MaxKeys:    20,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "TokenControlPlane",
			IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
			ExpiresAt: jwt.NewNumericDate(time.Now().UTC().Add(24 * time.Hour)),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := license.ValidateWithKey(tok, &priv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if got.Subject != "TokenControlPlane" || got.Plan != license.PlanPro || got.MaxServers != 10 {
		t.Fatalf("claims=%+v", got)
	}
}

func TestExpiredFallsBackViaResolve(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tok, err := license.Sign(priv, license.Claims{
		Plan: license.PlanPro,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "expired-co",
			IssuedAt:  jwt.NewNumericDate(time.Now().UTC().Add(-48 * time.Hour)),
			ExpiresAt: jwt.NewNumericDate(time.Now().UTC().Add(-24 * time.Hour)),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = license.ValidateWithKey(tok, &priv.PublicKey)
	if err == nil {
		t.Fatal("expected expiry error")
	}
}

func TestTamperedPayload(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tok, err := license.Sign(priv, license.Claims{
		Plan:       license.PlanTeam,
		MaxServers: 25,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "tamper",
			ExpiresAt: jwt.NewNumericDate(time.Now().UTC().Add(time.Hour)),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	b := []byte(tok)
	if len(b) < 40 {
		t.Fatal("token too short")
	}
	b[len(b)/2] ^= 0x01
	_, err = license.ValidateWithKey(string(b), &priv.PublicKey)
	if err == nil {
		t.Fatal("expected signature error")
	}
}

func TestResolveEmptyIsFree(t *testing.T) {
	t.Setenv("TOKENCONTROLPLANE_LICENSE_KEY", "")
	_ = os.Unsetenv("TOKENCONTROLPLANE_LICENSE_KEY")
	caps := license.Resolve(slog.Default(), "")
	if caps.Licensed || caps.MaxServers != 3 || caps.MonthlyTokens != license.FreeMonthlyTokens {
		t.Fatalf("caps=%+v", caps)
	}
}

func TestResolveInvalidFailsOpen(t *testing.T) {
	caps := license.Resolve(slog.Default(), "not.a.jwt")
	if caps.Licensed || caps.Plan != license.PlanFree {
		t.Fatalf("expected free fallback, got %+v", caps)
	}
}

func TestCapsForPlan(t *testing.T) {
	free := license.CapsForPlan(license.PlanFree)
	if free.MaxSeats != 3 || free.MaxServers != 3 || free.MaxKeys != 5 ||
		free.MonthlyTokens != license.FreeMonthlyTokens || free.ToolPolicy {
		t.Fatalf("free=%+v", free)
	}
	pro := license.CapsForPlan(license.PlanPro)
	if pro.MaxServers != 10 || pro.MaxKeys != 20 || pro.MaxSeats != 10 ||
		pro.MonthlyTokens != 0 || !pro.ToolPolicy {
		t.Fatalf("pro=%+v", pro)
	}
	team := license.CapsForPlan(license.PlanTeam)
	if team.MaxServers != 25 || team.MaxKeys != 50 || team.MonthlyTokens != 0 ||
		!team.ToolPolicy {
		t.Fatalf("team=%+v", team)
	}
	unknown := license.CapsForPlan("nope")
	if unknown.MaxServers != 3 {
		t.Fatalf("unknown=%+v", unknown)
	}
}

func TestResolveProTeamFailOpen(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	proTok, err := license.Sign(priv, license.Claims{
		Plan:       license.PlanPro,
		MaxServers: 10,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "pro-co",
			ExpiresAt: jwt.NewNumericDate(time.Now().UTC().Add(time.Hour)),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	proCaps := license.ResolveWithKey(slog.Default(), proTok, &priv.PublicKey)
	if !proCaps.Licensed || proCaps.MaxServers != 10 || proCaps.MonthlyTokens != 0 {
		t.Fatalf("pro resolve: %+v", proCaps)
	}

	teamTok, err := license.Sign(priv, license.Claims{
		Plan:       license.PlanTeam,
		MaxServers: 25,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "team-co",
			ExpiresAt: jwt.NewNumericDate(time.Now().UTC().Add(time.Hour)),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	teamCaps := license.ResolveWithKey(slog.Default(), teamTok, &priv.PublicKey)
	if !teamCaps.Licensed || teamCaps.MaxServers != 25 || teamCaps.MonthlyTokens != 0 {
		t.Fatalf("team resolve: %+v", teamCaps)
	}

	expired, err := license.Sign(priv, license.Claims{
		Plan: license.PlanPro,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "gone",
			ExpiresAt: jwt.NewNumericDate(time.Now().UTC().Add(-time.Hour)),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	bad := license.ResolveWithKey(slog.Default(), expired, &priv.PublicKey)
	if bad.Licensed || bad.Plan != license.PlanFree {
		t.Fatalf("expired must fail-open to free: %+v", bad)
	}
}
