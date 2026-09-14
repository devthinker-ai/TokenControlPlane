// seed-demo builds a screenshot-ready SQLite DB with realistic dummy data.
//
//	go run ./cmd/seed-demo -o ./demo.db
//	make demo-db && make demo
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"math"
	"os"
	"time"

	"github.com/devthinker-ai/TokenControlPlane/pkg/auth"
	"github.com/devthinker-ai/TokenControlPlane/pkg/session"
	"github.com/devthinker-ai/TokenControlPlane/pkg/store"
)

const (
	demoEmail    = "demo@acme.dev"
	demoPassword = "demo-demo-demo"
	demoKeyProd  = "tcp_demo_prod_KEY_FOR_SCREENSHOTS_ONLY"
	demoKeyCI    = "tcp_demo_ci_KEY_FOR_SCREENSHOTS_ONLY"
)

func main() {
	out := flag.String("o", "./demo.db", "output SQLite path (overwritten)")
	flag.Parse()

	_ = os.Remove(*out)
	_ = os.Remove(*out + "-wal")
	_ = os.Remove(*out + "-shm")
	_ = os.Remove(*out + ".backup")

	st, err := store.Open(*out)
	if err != nil {
		fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	if err := seed(ctx, st); err != nil {
		fatal(err)
	}

	fmt.Printf("Wrote %s\n\n", *out)
	fmt.Println("Login (dashboard):")
	fmt.Printf("  email:    %s\n", demoEmail)
	fmt.Printf("  password: %s\n\n", demoPassword)
	fmt.Println("Sample machine keys (hashes only in DB — these plaintexts work if you re-seed):")
	fmt.Printf("  prod: %s\n", demoKeyProd)
	fmt.Printf("  ci:   %s  (killed)\n", demoKeyCI)
	fmt.Println("\nStart:  make demo   # or: go run ./cmd/gateway --addr :8088 --db ./demo.db")
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "seed-demo: %v\n", err)
	os.Exit(1)
}

func seed(ctx context.Context, st *store.Store) error {
	now := time.Now().UTC()
	period := store.PeriodStartUTC(now)
	acctID := "acct_demo_acme"
	adminID := "user_demo_admin"
	memberID := "user_demo_maya"
	member2ID := "user_demo_leo"

	hash, err := session.HashPassword(demoPassword)
	if err != nil {
		return err
	}

	if err := st.CreateAccount(ctx, store.Account{
		ID:        acctID,
		Name:      "Acme Robotics",
		Plan:      store.PlanTeam,
		CreatedAt: now.Add(-90 * 24 * time.Hour),
	}); err != nil {
		return fmt.Errorf("account: %w", err)
	}
	if err := st.SetAccountOnboarded(ctx, acctID); err != nil {
		return err
	}
	if err := st.InsertLicense(ctx, store.License{
		ID:           "lic_demo_team",
		AccountID:    acctID,
		Plan:         store.PlanTeam,
		Key:          "TCP-DEMO-TEAM-XXXX-SCREENSHOT-ONLY",
		LQOrderID:    sql.NullString{String: "demo-order-1001", Valid: true},
		PurchasedAt:  now.Add(-60 * 24 * time.Hour),
		ExpiresAt:    now.Add(300 * 24 * time.Hour),
		CreatedAt:    now.Add(-60 * 24 * time.Hour),
		WindowMonths: 12,
		IsFounders:   true,
	}); err != nil {
		return fmt.Errorf("license: %w", err)
	}

	users := []store.User{
		{ID: adminID, AccountID: acctID, Email: demoEmail, PasswordHash: hash, Name: "Alex Chen", Role: "admin", CreatedAt: now.Add(-90 * 24 * time.Hour)},
		{ID: memberID, AccountID: acctID, Email: "maya@acme.dev", PasswordHash: hash, Name: "Maya Okonkwo", Role: "member", CreatedAt: now.Add(-40 * 24 * time.Hour)},
		{ID: member2ID, AccountID: acctID, Email: "leo@acme.dev", PasswordHash: hash, Name: "Leo Hartmann", Role: "member", CreatedAt: now.Add(-20 * 24 * time.Hour)},
	}
	for _, u := range users {
		if err := st.CreateUser(ctx, u); err != nil {
			return fmt.Errorf("user %s: %w", u.Email, err)
		}
	}

	// --- MCP servers ---
	srvGitHub := "srv_demo_github"
	srvNotion := "srv_demo_notion"
	srvPG := "srv_demo_postgres"
	srvSlack := "srv_demo_slack"
	srvFS := "srv_demo_fs"

	servers := []store.MCPServer{
		{ID: srvGitHub, AccountID: acctID, Name: "GitHub", BaseURL: "https://mcp.github.com/v1", AuthType: store.AuthTypeOAuthPKCE, Enabled: true, CreatedAt: now.Add(-55 * 24 * time.Hour)},
		{ID: srvNotion, AccountID: acctID, Name: "Notion", BaseURL: "https://mcp.notion.com/mcp", AuthType: store.AuthTypeStatic, AuthValue: "Bearer ntn_demo_xxx", Enabled: true, CreatedAt: now.Add(-50 * 24 * time.Hour)},
		{ID: srvPG, AccountID: acctID, Name: "Postgres", BaseURL: "https://mcp.acme.internal/postgres", AuthType: store.AuthTypeStatic, AuthValue: "Bearer pg_demo", Enabled: true, CreatedAt: now.Add(-45 * 24 * time.Hour)},
		{ID: srvSlack, AccountID: acctID, Name: "Slack", BaseURL: "https://mcp.slack.com/v1", AuthType: store.AuthTypeOAuthDevice, Enabled: false, LastIndexError: "upstream 503 — last index failed", CreatedAt: now.Add(-30 * 24 * time.Hour)},
		{ID: srvFS, AccountID: acctID, Name: "Workspace FS", BaseURL: "", AuthType: store.AuthTypeNone, Transport: store.TransportStdio, Command: "npx", ArgsJSON: `["-y","@modelcontextprotocol/server-filesystem","/tmp/acme"]`, EnvJSON: `{}`, CwdIsolation: true, Enabled: true, CreatedAt: now.Add(-14 * 24 * time.Hour)},
	}
	for _, s := range servers {
		if err := st.CreateServer(ctx, s); err != nil {
			return fmt.Errorf("server %s: %w", s.Name, err)
		}
	}
	_ = st.SetServerIndexError(ctx, srvSlack, "upstream 503 — last index failed")

	toolSets := map[string][]store.ToolDef{
		srvGitHub: {
			{Name: "create_issue", Description: "Open a GitHub issue", InputSchema: `{"type":"object"}`},
			{Name: "list_prs", Description: "List pull requests", InputSchema: `{"type":"object"}`},
			{Name: "search_code", Description: "Search repository code", InputSchema: `{"type":"object"}`},
			{Name: "get_file", Description: "Read a file from a repo", InputSchema: `{"type":"object"}`},
		},
		srvNotion: {
			{Name: "search", Description: "Search Notion workspace", InputSchema: `{"type":"object"}`},
			{Name: "create_page", Description: "Create a page", InputSchema: `{"type":"object"}`},
			{Name: "update_page", Description: "Update page properties", InputSchema: `{"type":"object"}`},
		},
		srvPG: {
			{Name: "query", Description: "Run a read-only SQL query", InputSchema: `{"type":"object"}`},
			{Name: "list_tables", Description: "List tables in schema", InputSchema: `{"type":"object"}`},
			{Name: "explain", Description: "EXPLAIN a query plan", InputSchema: `{"type":"object"}`},
		},
		srvSlack: {
			{Name: "post_message", Description: "Post to a channel", InputSchema: `{"type":"object"}`},
			{Name: "list_channels", Description: "List channels", InputSchema: `{"type":"object"}`},
		},
		srvFS: {
			{Name: "read_file", Description: "Read a local file", InputSchema: `{"type":"object"}`},
			{Name: "write_file", Description: "Write a local file", InputSchema: `{"type":"object"}`},
			{Name: "list_directory", Description: "List directory entries", InputSchema: `{"type":"object"}`},
		},
	}
	for sid, tools := range toolSets {
		for i := range tools {
			tools[i].ID = fmt.Sprintf("tool_%s_%s", sid, tools[i].Name)
			tools[i].ServerID = sid
		}
		if err := st.ReplaceTools(ctx, sid, tools); err != nil {
			return fmt.Errorf("tools %s: %w", sid, err)
		}
	}

	// --- API keys ---
	keyProd := "key_demo_prod"
	keyStaging := "key_demo_staging"
	keyCI := "key_demo_ci"
	keyMaya := "key_demo_maya"

	keys := []store.APIKey{
		{ID: keyProd, AccountID: acctID, KeyHash: auth.HashKey(demoKeyProd), Name: "prod-cursor", MonthlyBudget: 50_000_000, RateLimitRPM: 120, Enabled: true, CreatedAt: now.Add(-50 * 24 * time.Hour), OwnerID: sql.NullString{String: adminID, Valid: true}},
		{ID: keyStaging, AccountID: acctID, KeyHash: auth.HashKey("tcp_demo_staging_KEY_FOR_SCREENSHOTS_ONLY"), Name: "staging", MonthlyBudget: 5_000_000, RateLimitRPM: 60, Enabled: true, CreatedAt: now.Add(-40 * 24 * time.Hour), OwnerID: sql.NullString{String: adminID, Valid: true}},
		{ID: keyCI, AccountID: acctID, KeyHash: auth.HashKey(demoKeyCI), Name: "ci-runners", MonthlyBudget: 1_000_000, RateLimitRPM: 30, Enabled: true, KilledAt: sql.NullTime{Time: now.Add(-2 * time.Hour), Valid: true}, CreatedAt: now.Add(-35 * 24 * time.Hour), OwnerID: sql.NullString{String: adminID, Valid: true}},
		{ID: keyMaya, AccountID: acctID, KeyHash: auth.HashKey("tcp_demo_maya_KEY_FOR_SCREENSHOTS_ONLY"), Name: "maya-local", MonthlyBudget: 2_000_000, RateLimitRPM: 0, Enabled: true, CreatedAt: now.Add(-25 * 24 * time.Hour), OwnerID: sql.NullString{String: memberID, Valid: true}},
	}
	for _, k := range keys {
		if err := st.CreateAPIKey(ctx, k); err != nil {
			return fmt.Errorf("key %s: %w", k.Name, err)
		}
	}

	db := st.DB()
	for _, id := range []string{keyProd, keyStaging, keyMaya} {
		_, _ = db.ExecContext(ctx, `UPDATE api_keys SET last_used_at = ? WHERE id = ?`,
			now.Add(-time.Duration(15+len(id)%40)*time.Minute).Format(time.RFC3339Nano), id)
	}

	// --- LLM providers + routes ---
	pOpenAI := "llp_demo_openai"
	pDeepSeek := "llp_demo_deepseek"
	pOpenRouter := "llp_demo_openrouter"
	providers := []store.LLMProvider{
		{ID: pOpenAI, AccountID: acctID, Name: "OpenAI", BaseURL: "https://api.openai.com/v1", AuthValue: "Bearer sk-demo-openai", DefaultModel: "gpt-4o-mini", Enabled: true, CreatedAt: now.Add(-48 * 24 * time.Hour)},
		{ID: pDeepSeek, AccountID: acctID, Name: "DeepSeek", BaseURL: "https://api.deepseek.com/v1", AuthValue: "Bearer sk-demo-deepseek", DefaultModel: "deepseek-chat", Enabled: true, CreatedAt: now.Add(-20 * 24 * time.Hour)},
		{ID: pOpenRouter, AccountID: acctID, Name: "OpenRouter", BaseURL: "https://openrouter.ai/api/v1", AuthValue: "Bearer sk-or-demo", DefaultModel: "anthropic/claude-sonnet-4", Enabled: true, LastHealthError: "", CreatedAt: now.Add(-10 * 24 * time.Hour)},
	}
	for _, p := range providers {
		if err := st.CreateLLMProvider(ctx, p); err != nil {
			return fmt.Errorf("provider %s: %w", p.Name, err)
		}
	}
	models := []store.LLMModel{
		{ID: "llm_demo_chat", AccountID: acctID, Name: "chat", Model: "gpt-4o-mini", ProviderID: pOpenAI, CreatedAt: now.Add(-48 * 24 * time.Hour)},
		{ID: "llm_demo_code", AccountID: acctID, Name: "code", Model: "gpt-4o", ProviderID: pOpenAI, CreatedAt: now.Add(-48 * 24 * time.Hour)},
		{ID: "llm_demo_deepseek", AccountID: acctID, Name: "deepseek-chat", Model: "deepseek-chat", ProviderID: pDeepSeek, CreatedAt: now.Add(-20 * 24 * time.Hour)},
		{ID: "llm_demo_reason", AccountID: acctID, Name: "reason", Model: "anthropic/claude-sonnet-4", ProviderID: pOpenRouter, CreatedAt: now.Add(-10 * 24 * time.Hour)},
	}
	for _, m := range models {
		if err := st.CreateLLMModel(ctx, m); err != nil {
			return fmt.Errorf("model %s: %w", m.Name, err)
		}
	}

	// Per-provider budget on DeepSeek for prod key (screenshot of lane cap).
	_, err = db.ExecContext(ctx, `
INSERT INTO key_provider_budgets (key_id, provider_id, monthly_budget) VALUES (?, ?, ?)`,
		keyProd, pDeepSeek, 1_000_000)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `
INSERT INTO key_server_budgets (key_id, server_id, monthly_budget) VALUES (?, ?, ?)`,
		keyProd, srvPG, 2_000_000)
	if err != nil {
		return err
	}

	// --- Usage (current month) ---
	usageRows := []struct {
		keyID  string
		tokens int64
		bin    int64
		bout   int64
		reqs   int64
	}{
		{keyProd, 12_450_000, 8_000_000, 41_800_000, 18420},
		{keyStaging, 890_000, 600_000, 2_960_000, 2104},
		{keyCI, 1_000_000, 700_000, 3_300_000, 3200}, // at budget + killed
		{keyMaya, 410_000, 280_000, 1_360_000, 980},
	}
	for _, u := range usageRows {
		_, err := db.ExecContext(ctx, `
INSERT INTO usage (key_id, period_start, tokens_used, bytes_in, bytes_out, requests)
VALUES (?, ?, ?, ?, ?, ?)`,
			u.keyID, period.Format(time.RFC3339Nano), u.tokens, u.bin, u.bout, u.reqs)
		if err != nil {
			return fmt.Errorf("usage: %w", err)
		}
	}

	byServer := []struct {
		key, srv string
		tokens, bin, bout, reqs int64
	}{
		{keyProd, srvGitHub, 5_200_000, 3_000_000, 17_800_000, 8200},
		{keyProd, srvNotion, 2_100_000, 1_400_000, 7_000_000, 3100},
		{keyProd, srvPG, 3_400_000, 2_200_000, 11_400_000, 4900},
		{keyProd, srvFS, 1_750_000, 1_400_000, 5_600_000, 2220},
		{keyStaging, srvGitHub, 520_000, 350_000, 1_730_000, 1200},
		{keyStaging, srvNotion, 370_000, 250_000, 1_230_000, 904},
		{keyMaya, srvNotion, 210_000, 140_000, 700_000, 480},
		{keyMaya, srvFS, 200_000, 140_000, 660_000, 500},
	}
	for _, r := range byServer {
		_, err := db.ExecContext(ctx, `
INSERT INTO usage_by_server (key_id, server_id, period_start, tokens_used, bytes_in, bytes_out, requests)
VALUES (?, ?, ?, ?, ?, ?, ?)`,
			r.key, r.srv, period.Format(time.RFC3339Nano), r.tokens, r.bin, r.bout, r.reqs)
		if err != nil {
			return err
		}
	}

	byProv := []struct {
		key, prov string
		tokens, exact, est, bin, bout, reqs int64
	}{
		{keyProd, pOpenAI, 4_800_000, 4_200_000, 600_000, 2_000_000, 17_200_000, 6200},
		{keyProd, pDeepSeek, 980_000, 940_000, 40_000, 400_000, 3_520_000, 2100},
		{keyProd, pOpenRouter, 1_100_000, 1_050_000, 50_000, 500_000, 3_900_000, 900},
		{keyMaya, pOpenAI, 180_000, 170_000, 10_000, 80_000, 640_000, 220},
	}
	for _, r := range byProv {
		_, err := db.ExecContext(ctx, `
INSERT INTO usage_by_provider (key_id, provider_id, period_start, tokens_used, tokens_exact, tokens_estimated, bytes_in, bytes_out, requests)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			r.key, r.prov, period.Format(time.RFC3339Nano), r.tokens, r.exact, r.est, r.bin, r.bout, r.reqs)
		if err != nil {
			return err
		}
	}

	// Daily chart — last 30 days with a believable weekday curve.
	for i := 29; i >= 0; i-- {
		day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -i)
		wd := float64(day.Weekday())
		weekendDip := 1.0
		if day.Weekday() == time.Saturday || day.Weekday() == time.Sunday {
			weekendDip = 0.35
		}
		growth := 0.55 + 0.45*(1-float64(i)/30)
		noise := 0.85 + 0.3*math.Sin(float64(i)*0.7+wd)
		tokens := int64(180_000 * growth * weekendDip * noise)
		reqs := int64(float64(tokens) / 680)
		_, err := db.ExecContext(ctx, `
INSERT INTO usage_daily (account_id, day, tokens_used, requests) VALUES (?, ?, ?, ?)`,
			acctID, day.Format("2006-01-02"), tokens, reqs)
		if err != nil {
			return err
		}
	}

	// Recent tool calls for "top tools" widgets.
	toolCalls := []struct {
		srv, key, tool string
		ago            time.Duration
	}{
		{srvGitHub, keyProd, "search_code", 12 * time.Minute},
		{srvGitHub, keyProd, "list_prs", 25 * time.Minute},
		{srvGitHub, keyProd, "create_issue", 40 * time.Minute},
		{srvPG, keyProd, "query", 18 * time.Minute},
		{srvPG, keyProd, "query", 55 * time.Minute},
		{srvPG, keyProd, "list_tables", 2 * time.Hour},
		{srvNotion, keyProd, "search", 70 * time.Minute},
		{srvNotion, keyMaya, "create_page", 3 * time.Hour},
		{srvFS, keyMaya, "read_file", 4 * time.Hour},
		{srvFS, keyProd, "list_directory", 5 * time.Hour},
		{srvGitHub, keyStaging, "get_file", 6 * time.Hour},
		{srvNotion, keyStaging, "update_page", 8 * time.Hour},
	}
	for i, tc := range toolCalls {
		if err := st.InsertToolCall(ctx, store.ToolCall{
			ID:        fmt.Sprintf("tc_demo_%02d", i),
			AccountID: acctID,
			ServerID:  tc.srv,
			KeyID:     tc.key,
			ToolName:  tc.tool,
			CreatedAt: now.Add(-tc.ago),
		}); err != nil {
			return err
		}
	}

	// Activity feed (newest first in UI — vary timestamps).
	type act struct {
		kind, subject, summary string
		detail                 any
		ago                    time.Duration
	}
	acts := []act{
		{store.ActivityKeyKilledManual, "ci-runners", "Key 'ci-runners' killed", map[string]any{"key_id": keyCI}, 2 * time.Hour},
		{store.ActivityProviderBudgetExceeded, "DeepSeek", "Per-provider budget exceeded for DeepSeek", map[string]any{"key_id": keyProd, "provider_id": pDeepSeek, "budget": 1_000_000}, 5 * time.Hour},
		{store.ActivityLLMFallback, "chat", "LLM fallback: OpenAI → OpenRouter", map[string]any{"from": "chat", "to": "reason", "reason": "upstream_5xx"}, 9 * time.Hour},
		{store.ActivityCircuitOpen, "Slack", "Circuit opened for Slack", map[string]any{"server_id": srvSlack}, 26 * time.Hour},
		{store.ActivityCircuitClosed, "Postgres", "Circuit closed for Postgres", map[string]any{"server_id": srvPG}, 28 * time.Hour},
		{store.ActivityToolCallDenied, "write_file", "Tool call denied by policy", map[string]any{"tool": "write_file", "key_id": keyMaya}, 36 * time.Hour},
		{store.ActivityBudgetExceeded, "ci-runners", "Monthly budget exceeded", map[string]any{"key_id": keyCI, "budget": 1_000_000}, 48 * time.Hour},
		{store.ActivityOAuthConnected, "GitHub", "OAuth connected for GitHub", map[string]any{"server_id": srvGitHub}, 72 * time.Hour},
		{store.ActivityServerAdded, "Workspace FS", "Server 'Workspace FS' added", map[string]any{"server_id": srvFS}, 14 * 24 * time.Hour},
		{store.ActivityKeyCreated, "maya-local", "Key 'maya-local' created", map[string]any{"key_id": keyMaya}, 25 * 24 * time.Hour},
		{store.ActivityPlanChanged, "team", "Plan upgraded to Team", map[string]any{"plan": "team"}, 60 * 24 * time.Hour},
		{store.ActivityInviteEmailSent, "leo@acme.dev", "Invite emailed to leo@acme.dev", map[string]any{"email": "leo@acme.dev"}, 21 * 24 * time.Hour},
		{store.Activity2FAEnabled, demoEmail, "2FA enabled", map[string]any{"user_id": adminID}, 10 * 24 * time.Hour},
	}
	for _, a := range acts {
		if err := st.InsertActivityEvent(ctx, acctID, a.kind, a.subject, a.summary, a.detail); err != nil {
			return err
		}
	}
	// Fix activity timestamps (InsertActivityEvent stamps Now).
	rows, err := db.QueryContext(ctx, `SELECT id FROM activity_events WHERE account_id = ? ORDER BY rowid ASC`, acctID)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		_ = rows.Scan(&id)
		ids = append(ids, id)
	}
	_ = rows.Close()
	// acts were inserted in chronological order of the slice; map oldest→newest by reversing ago.
	for i, id := range ids {
		if i >= len(acts) {
			break
		}
		_, _ = db.ExecContext(ctx, `UPDATE activity_events SET created_at = ? WHERE id = ?`,
			now.Add(-acts[i].ago).Format(time.RFC3339Nano), id)
	}

	return nil
}
