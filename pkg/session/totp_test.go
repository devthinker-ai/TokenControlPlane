package session_test

import (
	"encoding/base32"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"

	"github.com/devthinker-ai/TokenControlPlane/pkg/session"
)

func TestGenerateSecretBase32NoPadding(t *testing.T) {
	sec, err := session.GenerateSecret()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sec, "=") {
		t.Fatalf("secret has padding: %q", sec)
	}
	if sec != strings.ToUpper(sec) {
		t.Fatalf("want uppercase, got %q", sec)
	}
	decoded, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(sec)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded) != 20 {
		t.Fatalf("want 20 bytes, got %d", len(decoded))
	}
}

func TestVerifyWindow(t *testing.T) {
	// Fixed secret + known time — generate a valid code via pquerna, then verify windows.
	secret := "JBSWY3DPEHPK3PXP" // 16 chars base32 = 10 bytes; pquerna accepts
	now := time.Unix(1111111111, 0).UTC()
	code, err := totp.GenerateCodeCustom(secret, now, totp.ValidateOpts{
		Period:    30,
		Skew:      0,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		t.Fatal(err)
	}
	ok, err := session.Verify(secret, code, now)
	if err != nil || !ok {
		t.Fatalf("t0: ok=%v err=%v", ok, err)
	}
	ok, _ = session.Verify(secret, code, now.Add(30*time.Second))
	if !ok {
		t.Fatal("t+1 window should accept")
	}
	ok, _ = session.Verify(secret, code, now.Add(-30*time.Second))
	if !ok {
		t.Fatal("t-1 window should accept")
	}
	ok, _ = session.Verify(secret, code, now.Add(90*time.Second))
	if ok {
		t.Fatal("t+2 window should reject")
	}
	ok, err = session.Verify(secret, "123", now)
	if err != nil || ok {
		t.Fatalf("short code: want false,nil got %v %v", ok, err)
	}
	ok, err = session.Verify(secret, "abcdef", now)
	if err != nil || ok {
		t.Fatalf("non-digit: want false,nil got %v %v", ok, err)
	}
}

func TestRecoveryCodeShape(t *testing.T) {
	c, err := session.RecoveryCode()
	if err != nil {
		t.Fatal(err)
	}
	if !session.LooksLikeRecoveryCode(c) {
		t.Fatalf("shape: %q", c)
	}
	codes, err := session.GenerateRecoveryCodes()
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != 10 {
		t.Fatalf("want 10, got %d", len(codes))
	}
	h := session.HashRecoveryCode(codes[0])
	if len(h) != 64 {
		t.Fatalf("sha256 hex len %d", len(h))
	}
}

func TestQRDataURL(t *testing.T) {
	uri := session.ProvisioningURI("JBSWY3DPEHPK3PXP", "a@example.com", "")
	if !strings.HasPrefix(uri, "otpauth://totp/") {
		t.Fatalf("uri: %s", uri)
	}
	qr, err := session.QRDataURL(uri)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(qr, "data:image/png;base64,") {
		t.Fatalf("qr prefix: %s", qr[:min(40, len(qr))])
	}
}

func TestMFATokenLifecycle(t *testing.T) {
	m := session.MustManager("test-jwt-secret-phase19-xxxxxxxx")
	now := time.Unix(1_700_000_000, 0).UTC()
	tok, jti, err := m.IssueMFA("usr_1", now)
	if err != nil {
		t.Fatal(err)
	}
	if jti == "" || tok == "" {
		t.Fatal("empty")
	}
	claims, err := m.ParseMFA(tok, now)
	if err != nil {
		t.Fatal(err)
	}
	if claims.UserID != "usr_1" || claims.ID != jti {
		t.Fatalf("claims %+v", claims)
	}
	_, err = m.ParseMFA(tok, now.Add(6*time.Minute))
	if err == nil {
		t.Fatal("expected expired")
	}
	gate := session.NewMFAGate(func() time.Time { return now })
	if !gate.MarkUsed(jti, now.Add(5*time.Minute)) {
		t.Fatal("first use")
	}
	if gate.MarkUsed(jti, now.Add(5*time.Minute)) {
		t.Fatal("second use should fail")
	}
}
