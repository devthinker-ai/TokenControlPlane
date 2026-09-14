package mail_test

import (
	"strings"
	"testing"

	"github.com/devthinker-ai/TokenControlPlane/pkg/mail"
)

func TestDefaultTemplatesRender(t *testing.T) {
	html, err := mail.RenderInvite("", mail.InviteData{
		AccountName: "Acme",
		InviterName: "Ada",
		InviteURL:   "https://gw.test/register?invite=ABC",
		ExpiresAt:   "tomorrow",
		GatewayURL:  "https://gw.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "Acme") || !strings.Contains(html, "Accept invite") {
		t.Fatalf("invite html=%s", html)
	}

	html, err = mail.RenderReset("", mail.ResetData{
		UserName: "Bob", ResetURL: "https://gw.test/reset-password?token=X",
		ExpiresIn: "1 hour", GatewayURL: "https://gw.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "Bob") || !strings.Contains(html, "Reset password") {
		t.Fatalf("reset html=%s", html)
	}
}

func TestBrokenTemplateFallsBack(t *testing.T) {
	html, err := mail.RenderInvite("{{.Broken", mail.InviteData{
		AccountName: "Acme", InviterName: "Ada",
		InviteURL: "https://x", ExpiresAt: "t", GatewayURL: "https://x",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "Accept invite") {
		t.Fatalf("expected default fallback: %s", html)
	}
}

func TestValidateTemplate(t *testing.T) {
	if err := mail.ValidateTemplate("x", "{{.Name}}"); err != nil {
		t.Fatal(err)
	}
	if err := mail.ValidateTemplate("x", "{{.Broken"); err == nil {
		t.Fatal("expected parse error")
	}
}
