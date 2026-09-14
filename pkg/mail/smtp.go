// Package mail sends HTML email via stdlib net/smtp (no external deps).
package mail

import (
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"
)

// TLSMode for SMTP connections.
const (
	TLSModeStartTLS = "starttls" // 587 default
	TLSModeTLS      = "tls"      // 465 implicit
	TLSModePlain    = "plain"    // 25 no TLS
)

// Config is the per-account SMTP settings JSON shape.
type Config struct {
	Enabled  bool   `json:"enabled"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	From     string `json:"from"`
	FromName string `json:"from_name"`
	TLSMode  string `json:"tls_mode"` // starttls | tls | plain
}

// Sender delivers HTML mail using stdlib only.
type Sender struct {
	Cfg Config
}

// Send delivers subject + HTML body to one recipient.
func (s *Sender) Send(to, subject, htmlBody string) error {
	if s.Cfg.Host == "" || s.Cfg.Port <= 0 {
		return fmt.Errorf("SMTP host/port not configured")
	}
	fromAddr, err := s.fromAddress()
	if err != nil {
		return err
	}
	toAddr, err := mail.ParseAddress(to)
	if err != nil {
		return fmt.Errorf("invalid recipient: %w", err)
	}

	msgID, err := generateMessageID(fromAddr.Address)
	if err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("From: " + fromAddr.String() + "\r\n")
	b.WriteString("To: " + toAddr.String() + "\r\n")
	b.WriteString("Subject: " + sanitizeHeader(subject) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	b.WriteString("Message-ID: <" + msgID + ">\r\n")
	b.WriteString("Date: " + time.Now().UTC().Format(time.RFC1123Z) + "\r\n")
	b.WriteString("\r\n")
	b.WriteString(htmlBody)
	raw := []byte(b.String())

	addr := fmt.Sprintf("%s:%d", s.Cfg.Host, s.Cfg.Port)
	mode := strings.ToLower(strings.TrimSpace(s.Cfg.TLSMode))
	if mode == "" {
		mode = TLSModeStartTLS
	}

	var client *smtp.Client
	switch mode {
	case TLSModeTLS:
		// Implicit TLS (465): TLS handshake before SMTP greeting.
		tlsCfg := &tls.Config{ServerName: s.Cfg.Host, MinVersion: tls.VersionTLS12}
		conn, err := tls.Dial("tcp", addr, tlsCfg)
		if err != nil {
			return fmt.Errorf("tls dial: %w", err)
		}
		client, err = smtp.NewClient(conn, s.Cfg.Host)
		if err != nil {
			_ = conn.Close()
			return fmt.Errorf("smtp client: %w", err)
		}
	case TLSModePlain:
		client, err = smtp.Dial(addr)
		if err != nil {
			return fmt.Errorf("smtp dial: %w", err)
		}
	case TLSModeStartTLS:
		client, err = smtp.Dial(addr)
		if err != nil {
			return fmt.Errorf("smtp dial: %w", err)
		}
		tlsCfg := &tls.Config{ServerName: s.Cfg.Host, MinVersion: tls.VersionTLS12}
		if err := client.StartTLS(tlsCfg); err != nil {
			_ = client.Close()
			return fmt.Errorf("starttls: %w", err)
		}
	default:
		return fmt.Errorf("unknown tls_mode %q (use starttls, tls, or plain)", mode)
	}
	defer func() { _ = client.Close() }()

	if s.Cfg.Username != "" {
		auth := smtp.PlainAuth("", s.Cfg.Username, s.Cfg.Password, s.Cfg.Host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	if err := client.Mail(fromAddr.Address); err != nil {
		return fmt.Errorf("mail from: %w", err)
	}
	if err := client.Rcpt(toAddr.Address); err != nil {
		return fmt.Errorf("rcpt to: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("data: %w", err)
	}
	if _, err := w.Write(raw); err != nil {
		_ = w.Close()
		return fmt.Errorf("write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("close data: %w", err)
	}
	return client.Quit()
}

func (s *Sender) fromAddress() (*mail.Address, error) {
	if s.Cfg.From != "" {
		addr, err := mail.ParseAddress(s.Cfg.From)
		if err == nil {
			return addr, nil
		}
	}
	// Fallback: from_name <username> or bare username/from.
	email := s.Cfg.Username
	if email == "" {
		email = s.Cfg.From
	}
	if email == "" {
		return nil, fmt.Errorf("SMTP from address not configured")
	}
	name := s.Cfg.FromName
	if name == "" {
		name = "TokenControlPlane"
	}
	return &mail.Address{Name: name, Address: email}, nil
}

func sanitizeHeader(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' {
			return -1
		}
		return r
	}, s)
}

func generateMessageID(fromEmail string) (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	domain := "localhost"
	if at := strings.LastIndex(fromEmail, "@"); at >= 0 && at+1 < len(fromEmail) {
		domain = fromEmail[at+1:]
	}
	return hex.EncodeToString(b) + "@" + domain, nil
}

// AddrHostPort is a helper for tests.
func AddrHostPort(host string, port int) string {
	return net.JoinHostPort(host, fmt.Sprintf("%d", port))
}
