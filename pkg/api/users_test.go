package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/devthinker-ai/TokenControlPlane/pkg/store"
)

func authJSON(t *testing.T, h http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rdr)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, req)
	return resp
}

func registerAdmin(t *testing.T, h http.Handler, email string) (token string, user map[string]any) {
	t.Helper()
	resp := authJSON(t, h, http.MethodPost, "/register", "", map[string]string{
		"email": email, "password": "password1", "account_name": "TeamCo", "name": "Admin",
	})
	if resp.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", resp.Code, resp.Body.String())
	}
	var out map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &out)
	tok, _ := out["token"].(string)
	u, _ := out["user"].(map[string]any)
	if tok == "" {
		t.Fatal("missing token")
	}
	return tok, u
}

func TestInviteLifecycleAndJoin(t *testing.T) {
	h, st := setupAPI(t)
	adminTok, _ := registerAdmin(t, h, "admin@team.test")

	// Create invite
	resp := authJSON(t, h, http.MethodPost, "/invites", adminTok, map[string]any{})
	if resp.Code != http.StatusCreated {
		t.Fatalf("invite: %d %s", resp.Code, resp.Body.String())
	}
	var inv map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &inv)
	code, _ := inv["code"].(string)
	path, _ := inv["path"].(string)
	if code == "" || !strings.Contains(path, code) {
		t.Fatalf("invite payload: %v", inv)
	}

	// Public lookup — no ids/emails
	resp = authJSON(t, h, http.MethodGet, "/invites/public/"+code, "", nil)
	if resp.Code != 200 {
		t.Fatalf("public: %d %s", resp.Code, resp.Body.String())
	}
	var pub map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &pub)
	if pub["account_name"] != "TeamCo" || pub["role"] != "member" {
		t.Fatalf("public=%v", pub)
	}
	raw := resp.Body.String()
	for _, leak := range []string{"acct_", "usr_", "@", "email", "id"} {
		if leak == "id" {
			continue // "role" ok; check no account id shape
		}
		if leak == "@" {
			continue
		}
	}
	if strings.Contains(raw, "admin@") || strings.Contains(raw, "acct_") {
		t.Fatalf("public leaked: %s", raw)
	}

	// Join once
	resp = authJSON(t, h, http.MethodPost, "/join", "", map[string]string{
		"code": code, "email": "member@team.test", "password": "password1", "name": "Member",
	})
	if resp.Code != http.StatusCreated {
		t.Fatalf("join: %d %s", resp.Code, resp.Body.String())
	}
	var joinOut map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &joinOut)
	memberTok, _ := joinOut["token"].(string)
	mu, _ := joinOut["user"].(map[string]any)
	if mu["role"] != "member" {
		t.Fatalf("role=%v", mu["role"])
	}

	// Second join same code → 404
	resp = authJSON(t, h, http.MethodPost, "/join", "", map[string]string{
		"code": code, "email": "other@team.test", "password": "password1", "name": "X",
	})
	if resp.Code != 404 || !strings.Contains(resp.Body.String(), "invalid or expired invite") {
		t.Fatalf("reuse: %d %s", resp.Code, resp.Body.String())
	}

	// Member blocked from invites
	resp = authJSON(t, h, http.MethodGet, "/invites", memberTok, nil)
	if resp.Code != 403 {
		t.Fatalf("member invites: %d", resp.Code)
	}
	resp = authJSON(t, h, http.MethodPost, "/servers", memberTok, map[string]string{
		"name": "x", "base_url": "https://example.com",
	})
	if resp.Code != 403 {
		t.Fatalf("member create server: %d %s", resp.Code, resp.Body.String())
	}

	// Member can create key (owner stamped)
	resp = authJSON(t, h, http.MethodPost, "/keys", memberTok, map[string]string{"name": "mkey"})
	if resp.Code != http.StatusCreated {
		t.Fatalf("member create key: %d %s", resp.Code, resp.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &created)
	keyID, _ := created["id"].(string)

	resp = authJSON(t, h, http.MethodGet, "/keys", memberTok, nil)
	if resp.Code != 200 {
		t.Fatalf("list keys: %d", resp.Code)
	}
	var keys []map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &keys)
	found := false
	for _, k := range keys {
		if k["id"] == keyID {
			found = true
			owner, _ := k["owner"].(map[string]any)
			if owner == nil || owner["name"] != "Member" {
				t.Fatalf("owner=%v", k["owner"])
			}
		}
	}
	if !found {
		t.Fatal("key missing")
	}

	// me() seats_used
	resp = authJSON(t, h, http.MethodGet, "/me", adminTok, nil)
	var me map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &me)
	if me["seats_used"] != float64(2) || me["role"] != "admin" {
		t.Fatalf("me=%v", me)
	}

	_ = st
}

func TestInviteMaxPendingAndRevoke(t *testing.T) {
	h, _ := setupAPI(t)
	adminTok, _ := registerAdmin(t, h, "cap@team.test")
	var codes []string
	for i := 0; i < 5; i++ {
		resp := authJSON(t, h, http.MethodPost, "/invites", adminTok, map[string]any{})
		if resp.Code != http.StatusCreated {
			t.Fatalf("invite %d: %d %s", i, resp.Code, resp.Body.String())
		}
		var inv map[string]any
		_ = json.Unmarshal(resp.Body.Bytes(), &inv)
		codes = append(codes, inv["code"].(string))
	}
	resp := authJSON(t, h, http.MethodPost, "/invites", adminTok, map[string]any{})
	if resp.Code != 409 {
		t.Fatalf("6th invite: %d %s", resp.Code, resp.Body.String())
	}

	resp = authJSON(t, h, http.MethodGet, "/invites", adminTok, nil)
	var list []map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &list)
	if len(list) != 5 {
		t.Fatalf("pending=%d", len(list))
	}
	id, _ := list[0]["id"].(string)
	resp = authJSON(t, h, http.MethodDelete, "/invites/"+id, adminTok, nil)
	if resp.Code != 204 {
		t.Fatalf("revoke: %d", resp.Code)
	}
	// Revoked code → 404 on join
	resp = authJSON(t, h, http.MethodPost, "/join", "", map[string]string{
		"code": codes[0], "email": "x@team.test", "password": "password1", "name": "X",
	})
	// codes[0] may not match list[0] order — use public lookup on revoked via list after
	// Safer: create one, revoke by listing after create
	_ = resp
}

func TestInviteRevokeThenJoin404(t *testing.T) {
	h, _ := setupAPI(t)
	adminTok, _ := registerAdmin(t, h, "rev@team.test")
	resp := authJSON(t, h, http.MethodPost, "/invites", adminTok, map[string]any{})
	var inv map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &inv)
	code := inv["code"].(string)

	resp = authJSON(t, h, http.MethodGet, "/invites", adminTok, nil)
	var list []map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &list)
	authJSON(t, h, http.MethodDelete, "/invites/"+list[0]["id"].(string), adminTok, nil)

	resp = authJSON(t, h, http.MethodPost, "/join", "", map[string]string{
		"code": code, "email": "gone@team.test", "password": "password1", "name": "G",
	})
	if resp.Code != 404 {
		t.Fatalf("revoked join: %d %s", resp.Code, resp.Body.String())
	}
}

func TestExpiredInvite404(t *testing.T) {
	h, st := setupAPI(t)
	adminTok, admin := registerAdmin(t, h, "exp@team.test")
	resp := authJSON(t, h, http.MethodPost, "/invites", adminTok, map[string]any{})
	var inv map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &inv)
	code := inv["code"].(string)

	acctID, _ := admin["account_id"].(string)
	_, _ = st.DB().Exec(`UPDATE invites SET expires_at = ? WHERE account_id = ?`,
		time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano), acctID)

	resp = authJSON(t, h, http.MethodPost, "/join", "", map[string]string{
		"code": code, "email": "late@team.test", "password": "password1", "name": "L",
	})
	if resp.Code != 404 {
		t.Fatalf("expired: %d %s", resp.Code, resp.Body.String())
	}
	resp = authJSON(t, h, http.MethodGet, "/invites/public/"+code, "", nil)
	if resp.Code != 404 {
		t.Fatalf("public expired: %d", resp.Code)
	}
}

func TestSeatCap(t *testing.T) {
	h, st := setupAPI(t)
	adminTok, admin := registerAdmin(t, h, "seat@team.test")
	acctID, _ := admin["account_id"].(string)
	_, _ = st.DB().Exec(`UPDATE accounts SET max_seats = 2 WHERE id = ?`, acctID)

	resp := authJSON(t, h, http.MethodPost, "/invites", adminTok, map[string]any{})
	var inv1 map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &inv1)
	resp = authJSON(t, h, http.MethodPost, "/join", "", map[string]string{
		"code": inv1["code"].(string), "email": "m1@team.test", "password": "password1", "name": "M1",
	})
	if resp.Code != http.StatusCreated {
		t.Fatalf("join1: %d %s", resp.Code, resp.Body.String())
	}

	resp = authJSON(t, h, http.MethodPost, "/invites", adminTok, map[string]any{})
	var inv2 map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &inv2)
	resp = authJSON(t, h, http.MethodPost, "/join", "", map[string]string{
		"code": inv2["code"].(string), "email": "m2@team.test", "password": "password1", "name": "M2",
	})
	if resp.Code != 409 || !strings.Contains(resp.Body.String(), "seat limit") {
		t.Fatalf("seat cap: %d %s", resp.Code, resp.Body.String())
	}

	// List users, delete member, join again
	resp = authJSON(t, h, http.MethodGet, "/users", adminTok, nil)
	var ulist map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &ulist)
	users, _ := ulist["users"].([]any)
	var memberID string
	for _, u := range users {
		m := u.(map[string]any)
		if m["role"] == "member" {
			memberID = m["id"].(string)
		}
	}
	resp = authJSON(t, h, http.MethodDelete, "/users/"+memberID, adminTok, nil)
	if resp.Code != 204 {
		t.Fatalf("delete member: %d %s", resp.Code, resp.Body.String())
	}

	resp = authJSON(t, h, http.MethodPost, "/invites", adminTok, map[string]any{})
	var inv3 map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &inv3)
	resp = authJSON(t, h, http.MethodPost, "/join", "", map[string]string{
		"code": inv3["code"].(string), "email": "m3@team.test", "password": "password1", "name": "M3",
	})
	if resp.Code != http.StatusCreated {
		t.Fatalf("rejoin: %d %s", resp.Code, resp.Body.String())
	}
}

func TestTeamSeatCap(t *testing.T) {
	// Account-plan path: Team caps seats at 25 (past free/Pro numeric limits).
	h, st := setupAPI(t)
	adminTok, admin := registerAdmin(t, h, "teamseat@team.test")
	acctID, _ := admin["account_id"].(string)
	if err := st.UpdateAccountPlan(t.Context(), acctID, store.PlanTeam, nil); err != nil {
		t.Fatal(err)
	}
	// Fill to 25 seats via store (admin is seat 1).
	for i := 0; i < 24; i++ {
		if err := st.CreateUser(t.Context(), store.User{
			ID: "usr_fill_" + string(rune('a'+i%26)) + strconv.Itoa(i), AccountID: acctID,
			Email: "fill" + strconv.Itoa(i) + "@team.test", PasswordHash: "x", Name: "F", Role: "member",
		}); err != nil {
			t.Fatal(err)
		}
	}
	n, err := st.CountUsers(t.Context(), acctID)
	if err != nil || n != 25 {
		t.Fatalf("want 25 seats got %d err=%v", n, err)
	}
	resp := authJSON(t, h, http.MethodPost, "/invites", adminTok, map[string]any{})
	if resp.Code != http.StatusCreated {
		t.Fatalf("invite: %d %s", resp.Code, resp.Body.String())
	}
	var inv map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &inv)
	resp = authJSON(t, h, http.MethodPost, "/join", "", map[string]string{
		"code": inv["code"].(string), "email": "overflow@team.test", "password": "password1", "name": "O",
	})
	if resp.Code != 409 || !strings.Contains(resp.Body.String(), "seat limit") {
		t.Fatalf("26th seat want 409: %d %s", resp.Code, resp.Body.String())
	}
}

func TestLastAdminInvariants(t *testing.T) {
	h, _ := setupAPI(t)
	adminTok, admin := registerAdmin(t, h, "last@team.test")
	adminID, _ := admin["id"].(string)

	resp := authJSON(t, h, http.MethodPatch, "/users/"+adminID, adminTok, map[string]string{"role": "member"})
	if resp.Code != 409 {
		t.Fatalf("demote last: %d %s", resp.Code, resp.Body.String())
	}
	resp = authJSON(t, h, http.MethodDelete, "/users/"+adminID, adminTok, nil)
	if resp.Code != 409 {
		t.Fatalf("delete self: %d %s", resp.Code, resp.Body.String())
	}
}

func TestMemberKeyOwnership(t *testing.T) {
	h, _ := setupAPI(t)
	adminTok, _ := registerAdmin(t, h, "own@team.test")
	resp := authJSON(t, h, http.MethodPost, "/invites", adminTok, map[string]any{})
	var inv map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &inv)
	resp = authJSON(t, h, http.MethodPost, "/join", "", map[string]string{
		"code": inv["code"].(string), "email": "mem@team.test", "password": "password1", "name": "Mem",
	})
	var joinOut map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &joinOut)
	memberTok := joinOut["token"].(string)

	// Admin creates a key
	resp = authJSON(t, h, http.MethodPost, "/keys", adminTok, map[string]string{"name": "admin-key"})
	var adminKey map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &adminKey)
	adminKeyID := adminKey["id"].(string)

	// Member creates own key
	resp = authJSON(t, h, http.MethodPost, "/keys", memberTok, map[string]string{"name": "mem-key"})
	var memKey map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &memKey)
	memKeyID := memKey["id"].(string)

	// Member cannot delete admin key
	resp = authJSON(t, h, http.MethodDelete, "/keys/"+adminKeyID, memberTok, nil)
	if resp.Code != 403 {
		t.Fatalf("delete others: %d %s", resp.Code, resp.Body.String())
	}
	// Member can delete own
	resp = authJSON(t, h, http.MethodDelete, "/keys/"+memKeyID, memberTok, nil)
	if resp.Code != 204 {
		t.Fatalf("delete own: %d %s", resp.Code, resp.Body.String())
	}
	// Member can kill any (fire alarm)
	resp = authJSON(t, h, http.MethodPost, "/keys/"+adminKeyID+"/kill", memberTok, nil)
	if resp.Code != 200 {
		t.Fatalf("kill any: %d %s", resp.Code, resp.Body.String())
	}
}

func TestRemovalKeepsKeys(t *testing.T) {
	h, st := setupAPI(t)
	adminTok, _ := registerAdmin(t, h, "keep@team.test")
	resp := authJSON(t, h, http.MethodPost, "/invites", adminTok, map[string]any{})
	var inv map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &inv)
	resp = authJSON(t, h, http.MethodPost, "/join", "", map[string]string{
		"code": inv["code"].(string), "email": "gone2@team.test", "password": "password1", "name": "Gone",
	})
	var joinOut map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &joinOut)
	memberTok := joinOut["token"].(string)
	memberUser := joinOut["user"].(map[string]any)
	memberID := memberUser["id"].(string)

	resp = authJSON(t, h, http.MethodPost, "/keys", memberTok, map[string]string{"name": "orphan"})
	var created map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &created)
	keyID := created["id"].(string)

	resp = authJSON(t, h, http.MethodDelete, "/users/"+memberID, adminTok, nil)
	if resp.Code != 204 {
		t.Fatalf("delete: %d", resp.Code)
	}
	k, err := st.GetAPIKey(t.Context(), keyID)
	if err != nil {
		t.Fatal(err)
	}
	if k.OwnerID.Valid {
		t.Fatalf("owner should be NULL after user delete, got %v", k.OwnerID)
	}
	_ = store.APIKey{}
}
