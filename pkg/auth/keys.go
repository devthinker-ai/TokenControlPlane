// Package auth validates machine gateway keys (tcp_*) — never mixed with JWT sessions.
package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/devthinker-ai/TokenControlPlane/pkg/store"
)

const (
	budgetExceededBody = `{"error": "Budget limit exceeded. Access suspended by TokenControlPlane."}`
	killSwitchBody     = `{"error":"Access suspended by TokenControlPlane kill switch."}`
)

// CachedKey is the in-memory view of an API key used for hot-path auth.
type CachedKey struct {
	ID               string
	AccountID        string
	KeyHash          string
	Name             string
	MonthlyBudget    int64
	RateLimitRPM     int
	Enabled          bool
	Killed           bool
	TokensUsed         int64            // current calendar-month period only
	ServerTokensUsed   map[string]int64 // serverID → current-month tokens
	ProviderTokensUsed map[string]int64 // providerID → current-month tokens
}

// Config tunes loop auto-kill.
type Config struct {
	LoopThreshold int           // >N requests in LoopWindow → auto-kill (default 120)
	LoopWindow    time.Duration // default 60s
	Logger        *slog.Logger
}

// Validator authenticates Bearer tcp_* keys and enforces kill/budget/RPM.
type Validator struct {
	store  *store.Store
	cfg    Config
	logger *slog.Logger

	mu    sync.RWMutex
	cache map[string]*CachedKey // keyHash → key
	byID  map[string]*CachedKey // keyID → key (same pointers)

	// rate + loop: sliding windows of request timestamps per key ID
	rateMu sync.Mutex
	rate   map[string][]time.Time
	loop   map[string][]time.Time

	now func() time.Time
}

// NewValidator loads keys from the store into an in-memory cache.
func NewValidator(s *store.Store, cfg Config) (*Validator, error) {
	if cfg.LoopThreshold <= 0 {
		cfg.LoopThreshold = envInt("LOOP_THRESHOLD", 120)
	}
	if cfg.LoopWindow <= 0 {
		cfg.LoopWindow = 60 * time.Second
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	v := &Validator{
		store:  s,
		cfg:    cfg,
		logger: logger,
		cache:  make(map[string]*CachedKey),
		byID:   make(map[string]*CachedKey),
		rate:   make(map[string][]time.Time),
		loop:   make(map[string][]time.Time),
		now:    func() time.Time { return time.Now().UTC() },
	}
	if err := v.Reload(context.Background()); err != nil {
		return nil, err
	}
	return v, nil
}

func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

// Reload refreshes the in-memory key cache from SQLite.
func (v *Validator) Reload(ctx context.Context) error {
	keys, err := v.store.ListAPIKeys(ctx)
	if err != nil {
		return err
	}
	period := store.PeriodStartUTC(v.now())
	next := make(map[string]*CachedKey, len(keys))
	byID := make(map[string]*CachedKey, len(keys))
	for _, k := range keys {
		usage, err := v.store.GetUsage(ctx, k.ID, period)
		if err != nil {
			return err
		}
		byServer, err := v.store.MapUsageByServer(ctx, k.ID, period)
		if err != nil {
			return err
		}
		if byServer == nil {
			byServer = map[string]int64{}
		}
		byProvider, err := v.store.MapUsageByProvider(ctx, k.ID, period)
		if err != nil {
			return err
		}
		if byProvider == nil {
			byProvider = map[string]int64{}
		}
		ck := &CachedKey{
			ID:                 k.ID,
			AccountID:          k.AccountID,
			KeyHash:            k.KeyHash,
			Name:               k.Name,
			MonthlyBudget:      k.MonthlyBudget,
			RateLimitRPM:       k.RateLimitRPM,
			Enabled:            k.Enabled,
			Killed:             k.KilledAt.Valid,
			TokensUsed:         usage.TokensUsed,
			ServerTokensUsed:   byServer,
			ProviderTokensUsed: byProvider,
		}
		next[k.KeyHash] = ck
		byID[k.ID] = ck
	}
	v.mu.Lock()
	v.cache = next
	v.byID = byID
	v.mu.Unlock()
	return nil
}

// HashKey returns the SHA-256 hex digest of a plaintext gateway key.
func HashKey(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

type ctxKey int

const keyCtx ctxKey = 1

// KeyFromContext returns the CachedKey attached by Middleware.
func KeyFromContext(ctx context.Context) (*CachedKey, bool) {
	k, ok := ctx.Value(keyCtx).(*CachedKey)
	return k, ok
}

// AddTokensUsed bumps the in-memory token counter after metering (same process).
func (v *Validator) AddTokensUsed(keyID string, tokens int64) {
	v.AddServerTokensUsed(keyID, "", tokens)
}

// AddServerTokensUsed bumps key-level and optional per-server counters.
func (v *Validator) AddServerTokensUsed(keyID, serverID string, tokens int64) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if k := v.byID[keyID]; k != nil {
		k.TokensUsed += tokens
		if serverID != "" {
			if k.ServerTokensUsed == nil {
				k.ServerTokensUsed = map[string]int64{}
			}
			k.ServerTokensUsed[serverID] += tokens
		}
	}
}

// AddProviderTokensUsed bumps key-level and per-provider counters (LLM path).
func (v *Validator) AddProviderTokensUsed(keyID, providerID string, tokens int64) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if k := v.byID[keyID]; k != nil {
		k.TokensUsed += tokens
		if providerID != "" {
			if k.ProviderTokensUsed == nil {
				k.ProviderTokensUsed = map[string]int64{}
			}
			k.ProviderTokensUsed[providerID] += tokens
		}
	}
}

// SetTokensUsed replaces the in-memory token counter (after usage reset).
func (v *Validator) SetTokensUsed(keyID string, tokens int64) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if k := v.byID[keyID]; k != nil {
		k.TokensUsed = tokens
	}
}

// PutKey inserts/updates a key in the cache (used by tests / after create).
func (v *Validator) PutKey(k *CachedKey) {
	v.mu.Lock()
	defer v.mu.Unlock()
	cp := *k
	v.cache[k.KeyHash] = &cp
	v.byID[k.ID] = &cp
}

// Kill marks a key killed in store + cache (immediate effect, no restart).
func (v *Validator) Kill(ctx context.Context, keyID string) error {
	now := v.now()
	if err := v.store.SetKeyKilled(ctx, keyID, &now); err != nil {
		return err
	}
	v.mu.Lock()
	if k := v.byID[keyID]; k != nil {
		k.Killed = true
	}
	v.mu.Unlock()
	return nil
}

// Unkill clears killed_at; if reset, zeros current-period usage.
func (v *Validator) Unkill(ctx context.Context, keyID string, reset bool) error {
	if err := v.store.SetKeyKilled(ctx, keyID, nil); err != nil {
		return err
	}
	if reset {
		period := store.PeriodStartUTC(v.now())
		if err := v.store.ResetUsagePeriod(ctx, keyID, period); err != nil {
			return err
		}
		v.SetTokensUsed(keyID, 0)
	}
	v.mu.Lock()
	if k := v.byID[keyID]; k != nil {
		k.Killed = false
		if reset {
			k.TokensUsed = 0
			k.ServerTokensUsed = map[string]int64{}
			k.ProviderTokensUsed = map[string]int64{}
		}
	}
	v.mu.Unlock()

	v.rateMu.Lock()
	delete(v.loop, keyID)
	delete(v.rate, keyID)
	v.rateMu.Unlock()
	return nil
}

// Middleware validates Authorization and applies enforcement precedence:
//
//  1. 401 — missing/invalid key
//  2. 403 — key disabled (server disabled is checked later in the proxy handler)
//  3. 402 — kill switch OR budget exhausted (distinct bodies)
//  4. 429 — per-minute rate limit (+ Retry-After)
//  5. 503 — upstream circuit open (proxy handler)
//
// WHY pre-flight only: response bytes are only known after the response streams,
// so budget enforcement is per-request. A single oversized tool call completes
// and gets counted; the *next* request is rejected. Mid-stream kill is v1.1.
//
// WHY 402 vs 429: one condition, one code. 402 = budget/quota/kill. 429 = rate only.
func (v *Validator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := bearerToken(r.Header.Get("Authorization"))
		if raw == "" {
			writeJSON(w, http.StatusUnauthorized, `{"error":"missing or invalid Authorization"}`)
			return
		}
		hash := HashKey(raw)

		v.mu.RLock()
		cached, ok := v.cache[hash]
		v.mu.RUnlock()
		if !ok {
			// WHY 401 before 402: invalid key never reveals kill/budget state.
			writeJSON(w, http.StatusUnauthorized, `{"error":"invalid API key"}`)
			return
		}
		if !cached.Enabled {
			writeJSON(w, http.StatusForbidden, `{"error":"API key disabled"}`)
			return
		}

		if cached.Killed {
			writeJSON(w, http.StatusPaymentRequired, killSwitchBody)
			return
		}

		// Auto-kill loop protection: >LOOP_THRESHOLD requests / 60s.
		if v.tripAutoKill(cached) {
			writeJSON(w, http.StatusPaymentRequired, killSwitchBody)
			return
		}

		// Pre-flight budget: current calendar-month Usage only.
		if cached.MonthlyBudget > 0 && cached.TokensUsed >= cached.MonthlyBudget {
			_ = v.store.InsertActivityEvent(context.Background(), cached.AccountID, store.ActivityBudgetExceeded, cached.Name,
				"Key '"+cached.Name+"' hit monthly budget",
				map[string]any{"key_id": cached.ID, "tokens_used": cached.TokensUsed, "budget": cached.MonthlyBudget})
			writeJSON(w, http.StatusPaymentRequired, budgetExceededBody)
			return
		}

		if cached.RateLimitRPM > 0 {
			if retryAfter, limited := v.checkRate(cached.ID, cached.RateLimitRPM); limited {
				_ = v.store.InsertActivityEvent(context.Background(), cached.AccountID, store.ActivityRateLimited, cached.Name,
					"Key '"+cached.Name+"' rate limited (429)",
					map[string]any{"key_id": cached.ID, "retry_after": retryAfter})
				w.Header().Set("Retry-After", retryAfter)
				writeJSON(w, http.StatusTooManyRequests, `{"error":"rate limit exceeded"}`)
				return
			}
		}

		ctx := context.WithValue(r.Context(), keyCtx, cached)
		next.ServeHTTP(w, r.WithContext(ctx))

		_ = v.store.TouchAPIKeyUsed(context.Background(), cached.ID, v.now())
	})
}

func (v *Validator) tripAutoKill(cached *CachedKey) bool {
	now := v.now()
	windowStart := now.Add(-v.cfg.LoopWindow)

	v.rateMu.Lock()
	stamps := v.loop[cached.ID]
	kept := stamps[:0]
	for _, t := range stamps {
		if t.After(windowStart) {
			kept = append(kept, t)
		}
	}
	kept = append(kept, now)
	v.loop[cached.ID] = kept
	n := len(kept)
	threshold := v.cfg.LoopThreshold
	v.rateMu.Unlock()

	if n <= threshold {
		return false
	}
	// Crossed threshold on this request — persist kill immediately.
	if err := v.Kill(context.Background(), cached.ID); err != nil {
		v.logger.Error("auto-kill persist failed", "key_id", cached.ID, "err", err)
	}
	v.logger.Info("auto-killed key", "key_id", cached.ID, "msg",
		"auto-killed key "+cached.ID+": "+itoa(n)+" requests in 60s")
	_ = v.store.InsertActivityEvent(context.Background(), cached.AccountID, store.ActivityKeyKilledAuto, cached.Name,
		"Key '"+cached.Name+"' was auto-killed: "+itoa(n)+" req in 60s",
		map[string]any{"key_id": cached.ID, "requests": n, "window_sec": 60})
	return true
}

func (v *Validator) checkRate(keyID string, rpm int) (retryAfter string, limited bool) {
	now := v.now()
	windowStart := now.Add(-60 * time.Second)

	v.rateMu.Lock()
	defer v.rateMu.Unlock()

	stamps := v.rate[keyID]
	kept := stamps[:0]
	for _, t := range stamps {
		if t.After(windowStart) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= rpm {
		oldest := kept[0]
		sec := int(oldest.Add(60*time.Second).Sub(now).Seconds()) + 1
		if sec < 1 {
			sec = 1
		}
		v.rate[keyID] = kept
		return itoa(sec), true
	}
	v.rate[keyID] = append(kept, now)
	return "", false
}

func writeJSON(w http.ResponseWriter, code int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(body))
}

func bearerToken(h string) string {
	const p = "Bearer "
	if !strings.HasPrefix(h, p) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(h, p))
}

func itoa(n int) string {
	return strconv.Itoa(n)
}

// BudgetExceededJSON is the exact 402 budget body.
func BudgetExceededJSON() string { return budgetExceededBody }

// KillSwitchJSON is the exact 402 kill-switch body.
func KillSwitchJSON() string { return killSwitchBody }

// Encode helpers for admin responses that need maps.
func WriteErrorJSON(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
