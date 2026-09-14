package mail

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"text/template"
)

// InviteData fields for invitation emails.
type InviteData struct {
	AccountName string
	InviterName string
	InviteURL   string
	ExpiresAt   string
	GatewayURL  string
}

// ResetData fields for password-reset emails.
type ResetData struct {
	UserName   string
	ResetURL   string
	ExpiresIn  string
	GatewayURL string
}

// DefaultInviteTemplate is inline-styled HTML suitable for email clients.
const DefaultInviteTemplate = `<!DOCTYPE html>
<html>
<body style="margin:0;padding:0;background:#f4f4f5;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Helvetica,Arial,sans-serif;">
  <table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="background:#f4f4f5;padding:32px 16px;">
    <tr><td align="center">
      <table role="presentation" width="100%" style="max-width:520px;background:#ffffff;border-radius:8px;border:1px solid #e4e4e7;padding:32px;">
        <tr><td>
          <p style="margin:0 0 8px;font-size:13px;color:#71717a;letter-spacing:0.02em;">TokenControlPlane</p>
          <h1 style="margin:0 0 16px;font-size:22px;font-weight:600;color:#18181b;line-height:1.3;">You're invited to join {{.AccountName}}</h1>
          <p style="margin:0 0 24px;font-size:15px;color:#3f3f46;line-height:1.5;">{{.InviterName}} invited you to collaborate on TokenControlPlane. This link expires {{.ExpiresAt}}.</p>
          <table role="presentation" cellspacing="0" cellpadding="0" style="margin:0 0 24px;">
            <tr><td style="border-radius:6px;background:#18181b;">
              <a href="{{.InviteURL}}" style="display:inline-block;padding:12px 24px;font-size:14px;font-weight:600;color:#ffffff;text-decoration:none;">Accept invite</a>
            </td></tr>
          </table>
          <p style="margin:0 0 8px;font-size:13px;color:#71717a;">Or copy this link:</p>
          <p style="margin:0;font-size:12px;color:#52525b;word-break:break-all;line-height:1.4;">{{.InviteURL}}</p>
        </td></tr>
      </table>
      <p style="margin:16px 0 0;font-size:12px;color:#a1a1aa;">{{.GatewayURL}}</p>
    </td></tr>
  </table>
</body>
</html>`

// DefaultResetTemplate is inline-styled HTML for password resets.
const DefaultResetTemplate = `<!DOCTYPE html>
<html>
<body style="margin:0;padding:0;background:#f4f4f5;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Helvetica,Arial,sans-serif;">
  <table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="background:#f4f4f5;padding:32px 16px;">
    <tr><td align="center">
      <table role="presentation" width="100%" style="max-width:520px;background:#ffffff;border-radius:8px;border:1px solid #e4e4e7;padding:32px;">
        <tr><td>
          <p style="margin:0 0 8px;font-size:13px;color:#71717a;letter-spacing:0.02em;">TokenControlPlane</p>
          <h1 style="margin:0 0 16px;font-size:22px;font-weight:600;color:#18181b;line-height:1.3;">Reset your password</h1>
          <p style="margin:0 0 24px;font-size:15px;color:#3f3f46;line-height:1.5;">Hi {{.UserName}}, we received a request to reset your password. This link expires in {{.ExpiresIn}}.</p>
          <table role="presentation" cellspacing="0" cellpadding="0" style="margin:0 0 24px;">
            <tr><td style="border-radius:6px;background:#18181b;">
              <a href="{{.ResetURL}}" style="display:inline-block;padding:12px 24px;font-size:14px;font-weight:600;color:#ffffff;text-decoration:none;">Reset password</a>
            </td></tr>
          </table>
          <p style="margin:0 0 8px;font-size:13px;color:#71717a;">Or copy this link:</p>
          <p style="margin:0 0 24px;font-size:12px;color:#52525b;word-break:break-all;line-height:1.4;">{{.ResetURL}}</p>
          <p style="margin:0;font-size:13px;color:#71717a;">If you didn't request this, you can ignore this email.</p>
        </td></tr>
      </table>
      <p style="margin:16px 0 0;font-size:12px;color:#a1a1aa;">{{.GatewayURL}}</p>
    </td></tr>
  </table>
</body>
</html>`

// ValidateTemplate parses a template string; returns an error if invalid.
func ValidateTemplate(name, src string) error {
	_, err := template.New(name).Parse(src)
	return err
}

// RenderInvite renders the invite template. Empty or broken stored templates
// fall back to DefaultInviteTemplate (never fail the send path).
func RenderInvite(stored string, data InviteData) (string, error) {
	return renderWithFallback("invite", stored, DefaultInviteTemplate, data)
}

// RenderReset renders the reset template with the same fallback policy.
func RenderReset(stored string, data ResetData) (string, error) {
	return renderWithFallback("reset", stored, DefaultResetTemplate, data)
}

func renderWithFallback(name, stored, fallback string, data any) (string, error) {
	src := strings.TrimSpace(stored)
	if src == "" {
		src = fallback
	}
	out, err := execute(name, src, data)
	if err == nil {
		return out, nil
	}
	slog.Warn("mail template parse/execute failed; using default", "template", name, "err", err)
	return execute(name+"_default", fallback, data)
}

func execute(name, src string, data any) (string, error) {
	tpl, err := template.New(name).Parse(src)
	if err != nil {
		return "", fmt.Errorf("parse: %w", err)
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute: %w", err)
	}
	return buf.String(), nil
}
