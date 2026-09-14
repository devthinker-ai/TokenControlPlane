package api_test

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/devthinker-ai/TokenControlPlane/pkg/mail"
	"github.com/devthinker-ai/TokenControlPlane/pkg/store"
)

// startPlainSMTP spins a minimal SMTP listener (plain, no AUTH) for tests.
func startPlainSMTP(t *testing.T) (host string, port int, got *[]string, closeFn func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	msgs := []string{}
	got = &msgs
	done := make(chan struct{})
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				select {
				case <-done:
					return
				default:
					return
				}
			}
			go func(c net.Conn) {
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(5 * time.Second))
				br := bufio.NewReader(c)
				_, _ = io.WriteString(c, "220 test.local ESMTP\r\n")
				var data strings.Builder
				inData := false
				for {
					line, err := br.ReadString('\n')
					if err != nil {
						return
					}
					if inData {
						data.WriteString(line)
						if strings.TrimRight(line, "\r\n") == "." {
							mu.Lock()
							msgs = append(msgs, data.String())
							*got = msgs
							mu.Unlock()
							_, _ = io.WriteString(c, "250 OK\r\n")
							inData = false
							data.Reset()
						}
						continue
					}
					cmd := strings.ToUpper(strings.TrimSpace(line))
					switch {
					case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
						_, _ = io.WriteString(c, "250-test.local\r\n250 OK\r\n")
					case strings.HasPrefix(cmd, "MAIL"), strings.HasPrefix(cmd, "RCPT"):
						_, _ = io.WriteString(c, "250 OK\r\n")
					case strings.HasPrefix(cmd, "DATA"):
						_, _ = io.WriteString(c, "354 End data with <CR><LF>.<CR><LF>\r\n")
						inData = true
					case strings.HasPrefix(cmd, "QUIT"):
						_, _ = io.WriteString(c, "221 bye\r\n")
						return
					default:
						_, _ = io.WriteString(c, "250 OK\r\n")
					}
				}
			}(conn)
		}
	}()
	addr := ln.Addr().(*net.TCPAddr)
	return "127.0.0.1", addr.Port, got, func() {
		close(done)
		_ = ln.Close()
	}
}

func putSMTPEnabled(t *testing.T, h http.Handler, tok, host string, port int) {
	t.Helper()
	en := true
	resp := authJSON(t, h, http.MethodPut, "/settings/smtp", tok, map[string]any{
		"enabled": en, "host": host, "port": port, "username": "",
		"password": "secret-pass", "from": "gw@test.local", "from_name": "GW",
		"tls_mode": mail.TLSModePlain,
	})
	if resp.Code != 200 {
		t.Fatalf("put smtp: %d %s", resp.Code, resp.Body.String())
	}
}

func TestSMTPSettingsRoundTrip(t *testing.T) {
	h, _ := setupAPI(t)
	tok, _ := registerAdmin(t, h, "smtp@test.local")

	en := true
	resp := authJSON(t, h, http.MethodPut, "/settings/smtp", tok, map[string]any{
		"enabled": en, "host": "smtp.example.com", "port": 587,
		"username": "u", "password": "s3cret", "from": "a@b.com",
		"from_name": "GW", "tls_mode": "starttls",
	})
	if resp.Code != 200 {
		t.Fatalf("put: %d %s", resp.Code, resp.Body.String())
	}
	var dto map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &dto)
	if dto["has_password"] != true {
		t.Fatalf("has_password=%v", dto["has_password"])
	}
	if _, ok := dto["password"]; ok {
		t.Fatal("password must never be returned")
	}

	resp = authJSON(t, h, http.MethodGet, "/settings/smtp", tok, nil)
	_ = json.Unmarshal(resp.Body.Bytes(), &dto)
	if dto["has_password"] != true || dto["host"] != "smtp.example.com" {
		t.Fatalf("get=%v", dto)
	}
	raw := resp.Body.String()
	if strings.Contains(raw, "s3cret") {
		t.Fatal("password leaked in GET")
	}

	// Empty password keeps stored secret.
	resp = authJSON(t, h, http.MethodPut, "/settings/smtp", tok, map[string]any{
		"enabled": en, "host": "smtp.example.com", "port": 587,
		"username": "u", "password": "", "from": "a@b.com",
		"from_name": "GW", "tls_mode": "starttls",
	})
	_ = json.Unmarshal(resp.Body.Bytes(), &dto)
	if dto["has_password"] != true {
		t.Fatal("empty password should keep existing")
	}
}

func TestSMTPTestEmail(t *testing.T) {
	h, _ := setupAPI(t)
	tok, _ := registerAdmin(t, h, "smtptest@test.local")
	host, port, msgs, closeFn := startPlainSMTP(t)
	defer closeFn()
	putSMTPEnabled(t, h, tok, host, port)

	resp := authJSON(t, h, http.MethodPost, "/settings/smtp/test", tok, map[string]any{
		"to": "you@example.com",
	})
	if resp.Code != 200 {
		t.Fatalf("test: %d %s", resp.Code, resp.Body.String())
	}
	var out map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &out)
	if out["sent"] != true {
		t.Fatalf("out=%v", out)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(*msgs) == 0 {
		time.Sleep(20 * time.Millisecond)
	}
	if len(*msgs) == 0 {
		t.Fatal("no mail received by test SMTP")
	}

	// Bad host → readable 4xx
	resp = authJSON(t, h, http.MethodPut, "/settings/smtp", tok, map[string]any{
		"enabled": true, "host": "127.0.0.1", "port": 1,
		"password": "x", "from": "a@b.com", "tls_mode": "plain",
	})
	resp = authJSON(t, h, http.MethodPost, "/settings/smtp/test", tok, map[string]any{
		"to": "you@example.com",
	})
	if resp.Code < 400 || resp.Code >= 500 {
		t.Fatalf("expected 4xx, got %d %s", resp.Code, resp.Body.String())
	}
}

func TestEmailInvite(t *testing.T) {
	h, st := setupAPI(t)
	tok, u := registerAdmin(t, h, "invadmin@test.local")
	acctID, _ := u["account_id"].(string)

	// SMTP off → 409
	resp := authJSON(t, h, http.MethodPost, "/invites/email", tok, map[string]any{
		"email": "newbie@test.local",
	})
	if resp.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d %s", resp.Code, resp.Body.String())
	}

	host, port, msgs, closeFn := startPlainSMTP(t)
	defer closeFn()
	putSMTPEnabled(t, h, tok, host, port)

	resp = authJSON(t, h, http.MethodPost, "/invites/email", tok, map[string]any{
		"email": "newbie@test.local",
	})
	if resp.Code != http.StatusCreated {
		t.Fatalf("email invite: %d %s", resp.Code, resp.Body.String())
	}
	var out map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &out)
	if out["email_sent"] != true {
		t.Fatalf("out=%v", out)
	}
	inv, _ := out["invite"].(map[string]any)
	if inv["code"] == nil || inv["url"] == nil {
		t.Fatalf("invite=%v", inv)
	}
	list, err := st.ListEmailInvites(t.Context(), acctID)
	if err != nil || len(list) != 1 {
		t.Fatalf("email_invites=%v err=%v", list, err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(*msgs) == 0 {
		time.Sleep(20 * time.Millisecond)
	}
	if len(*msgs) == 0 {
		t.Fatal("expected invite mail")
	}

	// SMTP send error → still 201 with email_error + link
	resp = authJSON(t, h, http.MethodPut, "/settings/smtp", tok, map[string]any{
		"enabled": true, "host": "127.0.0.1", "port": 1,
		"password": "x", "from": "a@b.com", "tls_mode": "plain",
	})
	resp = authJSON(t, h, http.MethodPost, "/invites/email", tok, map[string]any{
		"email": "other@test.local",
	})
	if resp.Code != http.StatusCreated {
		t.Fatalf("want 201 on send fail, got %d %s", resp.Code, resp.Body.String())
	}
	_ = json.Unmarshal(resp.Body.Bytes(), &out)
	if out["email_sent"] != false {
		t.Fatalf("expected email_sent false: %v", out)
	}
	if out["email_error"] == nil || out["email_error"] == "" {
		t.Fatalf("expected email_error: %v", out)
	}
	inv, _ = out["invite"].(map[string]any)
	if inv["url"] == nil || inv["url"] == "" {
		t.Fatal("link should still be present")
	}
}

func TestForgotPasswordFlow(t *testing.T) {
	h, st := setupAPI(t)
	tok, u := registerAdmin(t, h, "resetme@test.local")
	acctID, _ := u["account_id"].(string)
	userID, _ := u["id"].(string)

	// Unknown email → 200, no row
	resp := authJSON(t, h, http.MethodPost, "/password-reset/request", "", map[string]any{
		"email": "nobody@test.local",
	})
	if resp.Code != 200 {
		t.Fatalf("unknown: %d", resp.Code)
	}
	var n int
	_ = st.DB().QueryRow(`SELECT COUNT(*) FROM password_resets`).Scan(&n)
	if n != 0 {
		t.Fatalf("unexpected resets=%d", n)
	}

	host, port, msgs, closeFn := startPlainSMTP(t)
	defer closeFn()
	putSMTPEnabled(t, h, tok, host, port)

	resp = authJSON(t, h, http.MethodPost, "/password-reset/request", "", map[string]any{
		"email": "resetme@test.local",
	})
	if resp.Code != 200 {
		t.Fatalf("request: %d %s", resp.Code, resp.Body.String())
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(*msgs) == 0 {
		time.Sleep(20 * time.Millisecond)
	}
	if len(*msgs) == 0 {
		t.Fatal("expected reset mail")
	}
	// Extract token from mail body
	mailBody := (*msgs)[0]
	idx := strings.Index(mailBody, "token=")
	if idx < 0 {
		t.Fatalf("no token in mail: %s", mailBody)
	}
	rest := mailBody[idx+len("token="):]
	token := ""
	for _, c := range rest {
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			token += string(c)
			continue
		}
		break
	}
	if len(token) < 8 {
		t.Fatalf("bad token extract: %q from %s", token, mailBody)
	}

	_ = st.DB().QueryRow(`SELECT COUNT(*) FROM password_resets WHERE user_id = ?`, userID).Scan(&n)
	if n != 1 {
		t.Fatalf("resets=%d acct=%s", n, acctID)
	}

	resp = authJSON(t, h, http.MethodPost, "/password-reset/confirm", "", map[string]any{
		"token": token, "new_password": "newpass12", "confirm_password": "newpass12",
	})
	if resp.Code != 200 {
		t.Fatalf("confirm: %d %s", resp.Code, resp.Body.String())
	}

	// Login with new password
	resp = authJSON(t, h, http.MethodPost, "/login", "", map[string]any{
		"email": "resetme@test.local", "password": "newpass12",
	})
	if resp.Code != 200 {
		t.Fatalf("login new: %d %s", resp.Code, resp.Body.String())
	}

	// Second confirm → 400
	resp = authJSON(t, h, http.MethodPost, "/password-reset/confirm", "", map[string]any{
		"token": token, "new_password": "another12", "confirm_password": "another12",
	})
	if resp.Code != 400 {
		t.Fatalf("reuse want 400, got %d", resp.Code)
	}
}

func TestAdminResetPassword(t *testing.T) {
	h, _ := setupAPI(t)
	tok, _ := registerAdmin(t, h, "admreset@test.local")

	// Create member via invite
	resp := authJSON(t, h, http.MethodPost, "/invites", tok, map[string]any{})
	var inv map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &inv)
	code, _ := inv["code"].(string)
	resp = authJSON(t, h, http.MethodPost, "/join", "", map[string]any{
		"code": code, "email": "memreset@test.local", "password": "password1", "name": "Mem",
	})
	var join map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &join)
	mem, _ := join["user"].(map[string]any)
	memID, _ := mem["id"].(string)

	host, port, _, closeFn := startPlainSMTP(t)
	defer closeFn()
	putSMTPEnabled(t, h, tok, host, port)

	// Generated password
	resp = authJSON(t, h, http.MethodPost, "/users/"+memID+"/reset-password", tok, map[string]any{})
	if resp.Code != 200 {
		t.Fatalf("admin reset: %d %s", resp.Code, resp.Body.String())
	}
	var out map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &out)
	if out["ok"] != true || out["new_password"] == nil || out["new_password"] == "" {
		t.Fatalf("out=%v", out)
	}
	if out["email_sent"] != true {
		t.Fatalf("expected email_sent: %v", out)
	}
	genPwd, _ := out["new_password"].(string)

	resp = authJSON(t, h, http.MethodPost, "/login", "", map[string]any{
		"email": "memreset@test.local", "password": genPwd,
	})
	if resp.Code != 200 {
		t.Fatalf("login gen: %d", resp.Code)
	}

	// Explicit password — not echoed
	resp = authJSON(t, h, http.MethodPost, "/users/"+memID+"/reset-password", tok, map[string]any{
		"new_password": "explicit99",
	})
	out = map[string]any{}
	_ = json.Unmarshal(resp.Body.Bytes(), &out)
	if _, ok := out["new_password"]; ok {
		t.Fatal("must not echo supplied password")
	}
	resp = authJSON(t, h, http.MethodPost, "/login", "", map[string]any{
		"email": "memreset@test.local", "password": "explicit99",
	})
	if resp.Code != 200 {
		t.Fatalf("login explicit: %d", resp.Code)
	}
}

func TestSelfChangePassword(t *testing.T) {
	h, _ := setupAPI(t)
	tok, _ := registerAdmin(t, h, "selfpw@test.local")

	resp := authJSON(t, h, http.MethodPut, "/me/password", tok, map[string]any{
		"current": "wrong", "new": "newpass12", "confirm": "newpass12",
	})
	if resp.Code != http.StatusUnauthorized && resp.Code != http.StatusBadRequest {
		t.Fatalf("wrong current: %d", resp.Code)
	}

	resp = authJSON(t, h, http.MethodPut, "/me/password", tok, map[string]any{
		"current": "password1", "new": "newpass12", "confirm": "newpass12",
	})
	if resp.Code != 200 {
		t.Fatalf("change: %d %s", resp.Code, resp.Body.String())
	}

	resp = authJSON(t, h, http.MethodPost, "/login", "", map[string]any{
		"email": "selfpw@test.local", "password": "password1",
	})
	if resp.Code != 401 {
		t.Fatalf("old password should fail: %d", resp.Code)
	}
	resp = authJSON(t, h, http.MethodPost, "/login", "", map[string]any{
		"email": "selfpw@test.local", "password": "newpass12",
	})
	if resp.Code != 200 {
		t.Fatalf("new password login: %d", resp.Code)
	}
}

func TestTemplateFallback(t *testing.T) {
	h, st := setupAPI(t)
	tok, u := registerAdmin(t, h, "tpl@test.local")
	acctID, _ := u["account_id"].(string)

	// Bad template on PUT → 400
	resp := authJSON(t, h, http.MethodPut, "/settings/templates", tok, map[string]any{
		"invite": "{{.Broken", "reset": "",
	})
	if resp.Code != 400 {
		t.Fatalf("want 400 parse err, got %d %s", resp.Code, resp.Body.String())
	}

	// Store a broken template directly (bypass PUT validation) — send still works via fallback.
	_ = st.SetAccountSetting(t.Context(), acctID, store.SettingTplInvite, "{{.Broken")

	host, port, msgs, closeFn := startPlainSMTP(t)
	defer closeFn()
	putSMTPEnabled(t, h, tok, host, port)

	resp = authJSON(t, h, http.MethodPost, "/invites/email", tok, map[string]any{
		"email": "fallback@test.local",
	})
	if resp.Code != http.StatusCreated {
		t.Fatalf("invite: %d %s", resp.Code, resp.Body.String())
	}
	var out map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &out)
	if out["email_sent"] != true {
		t.Fatalf("fallback send should succeed: %v", out)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(*msgs) == 0 {
		time.Sleep(20 * time.Millisecond)
	}
	if len(*msgs) == 0 {
		t.Fatal("expected fallback mail")
	}
	if !strings.Contains((*msgs)[0], "Accept invite") {
		t.Fatalf("expected default template content: %s", (*msgs)[0])
	}
}
