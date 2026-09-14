// Package policy is the in-memory tool/server visibility cache for the proxy hot path.
// Effective access is computed once here and used by HTTP proxy + stdio bridge.
package policy

import (
	"context"
	"sync"

	"github.com/devthinker-ai/TokenControlPlane/pkg/store"
)

// Cache holds server tool enable maps and per-key grant/budget sets.
// Proxy hot path = map lookups only (no SQLite).
type Cache struct {
	mu      sync.RWMutex
	servers map[string]map[string]bool // serverID → name → enabled
	keys    map[string]keyPolicy       // keyID → policy
}

type keyPolicy struct {
	toolMode        string
	toolGrants      map[string]struct{}
	serverMode      string
	serverGrants    map[string]struct{}
	serverBudgets   map[string]int64 // serverID → monthly budget (0 omitted)
	providerMode    string
	providerGrants  map[string]struct{}
	providerBudgets map[string]int64 // providerID → monthly budget (0 omitted)
}

// NewCache creates an empty cache (call Warm after Open).
func NewCache() *Cache {
	return &Cache{
		servers: map[string]map[string]bool{},
		keys:    map[string]keyPolicy{},
	}
}

// Warm loads all server tools + key policies from the store.
func (c *Cache) Warm(ctx context.Context, st *store.Store) error {
	servers, err := st.ListAllServerToolMaps(ctx)
	if err != nil {
		return err
	}
	toolPolicies, err := st.ListAllKeyPolicies(ctx)
	if err != nil {
		return err
	}
	serverModes, serverGrants, serverBudgets, err := st.ListAllKeyServerPolicies(ctx)
	if err != nil {
		return err
	}
	providerModes, providerGrants, providerBudgets, err := st.ListAllKeyProviderPolicies(ctx)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.servers = servers
	c.keys = map[string]keyPolicy{}
	// Union of key IDs from all policy maps.
	ids := map[string]struct{}{}
	for id := range toolPolicies {
		ids[id] = struct{}{}
	}
	for id := range serverModes {
		ids[id] = struct{}{}
	}
	for id := range providerModes {
		ids[id] = struct{}{}
	}
	for id := range ids {
		tp := toolPolicies[id]
		toolMode := tp.Mode
		if toolMode == "" {
			toolMode = store.ToolPolicyAll
		}
		tg := map[string]struct{}{}
		for _, name := range tp.Allowed {
			tg[name] = struct{}{}
		}
		serverMode := serverModes[id]
		if serverMode == "" {
			serverMode = store.ServerPolicyAll
		}
		sg := map[string]struct{}{}
		for _, sid := range serverGrants[id] {
			sg[sid] = struct{}{}
		}
		sb := serverBudgets[id]
		if sb == nil {
			sb = map[string]int64{}
		}
		providerMode := providerModes[id]
		if providerMode == "" {
			providerMode = store.ProviderPolicyAll
		}
		pg := map[string]struct{}{}
		for _, pid := range providerGrants[id] {
			pg[pid] = struct{}{}
		}
		pb := providerBudgets[id]
		if pb == nil {
			pb = map[string]int64{}
		}
		c.keys[id] = keyPolicy{
			toolMode: toolMode, toolGrants: tg,
			serverMode: serverMode, serverGrants: sg, serverBudgets: sb,
			providerMode: providerMode, providerGrants: pg, providerBudgets: pb,
		}
	}
	return nil
}

// InvalidateServer reloads one server's tool enable map.
func (c *Cache) InvalidateServer(ctx context.Context, st *store.Store, serverID string) error {
	m, err := st.ListEnabledToolMap(ctx, serverID)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.servers[serverID] = m
	return nil
}

// InvalidateKey reloads one key's tool + server + provider policy.
func (c *Cache) InvalidateKey(ctx context.Context, st *store.Store, keyID string) error {
	tp, err := st.GetKeyToolPolicy(ctx, keyID)
	if err != nil {
		c.mu.Lock()
		delete(c.keys, keyID)
		c.mu.Unlock()
		return err
	}
	sp, err := st.GetKeyServerPolicy(ctx, keyID)
	if err != nil {
		c.mu.Lock()
		delete(c.keys, keyID)
		c.mu.Unlock()
		return err
	}
	budgets, err := st.ListKeyServerBudgets(ctx, keyID)
	if err != nil {
		return err
	}
	pp, err := st.GetKeyProviderPolicy(ctx, keyID)
	if err != nil {
		c.mu.Lock()
		delete(c.keys, keyID)
		c.mu.Unlock()
		return err
	}
	pbList, err := st.ListKeyProviderBudgets(ctx, keyID)
	if err != nil {
		return err
	}
	tg := map[string]struct{}{}
	for _, name := range tp.Allowed {
		tg[name] = struct{}{}
	}
	sg := map[string]struct{}{}
	for _, sid := range sp.Allowed {
		sg[sid] = struct{}{}
	}
	sb := map[string]int64{}
	for _, b := range budgets {
		sb[b.ServerID] = b.MonthlyBudget
	}
	pg := map[string]struct{}{}
	for _, pid := range pp.Allowed {
		pg[pid] = struct{}{}
	}
	pb := map[string]int64{}
	for _, b := range pbList {
		pb[b.ProviderID] = b.MonthlyBudget
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.keys[keyID] = keyPolicy{
		toolMode: tp.Mode, toolGrants: tg,
		serverMode: sp.Mode, serverGrants: sg, serverBudgets: sb,
		providerMode: pp.Mode, providerGrants: pg, providerBudgets: pb,
	}
	return nil
}

// RemoveKey drops a deleted key from the cache.
func (c *Cache) RemoveKey(keyID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.keys, keyID)
}

// RemoveServer drops a deleted server's tools from the cache and prunes grants/budgets.
func (c *Cache) RemoveServer(serverID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.servers, serverID)
	for id, kp := range c.keys {
		delete(kp.serverGrants, serverID)
		delete(kp.serverBudgets, serverID)
		c.keys[id] = kp
	}
}

// AllowedServer returns whether keyID may access serverID.
func (c *Cache) AllowedServer(keyID, serverID string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	kp, ok := c.keys[keyID]
	if !ok || kp.serverMode != store.ServerPolicyCustom {
		return true
	}
	_, granted := kp.serverGrants[serverID]
	return granted
}

// ServerBudget returns the per-server monthly token budget (0 = no cap).
func (c *Cache) ServerBudget(keyID, serverID string) int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	kp, ok := c.keys[keyID]
	if !ok {
		return 0
	}
	return kp.serverBudgets[serverID]
}

// AllowedProvider returns whether keyID may access providerID.
func (c *Cache) AllowedProvider(keyID, providerID string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	kp, ok := c.keys[keyID]
	if !ok || kp.providerMode != store.ProviderPolicyCustom {
		return true
	}
	_, granted := kp.providerGrants[providerID]
	return granted
}

// ProviderBudget returns the per-provider monthly token budget (0 = no cap).
func (c *Cache) ProviderBudget(keyID, providerID string) int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	kp, ok := c.keys[keyID]
	if !ok {
		return 0
	}
	return kp.providerBudgets[providerID]
}

// RemoveProvider drops grants/budgets for a deleted provider.
func (c *Cache) RemoveProvider(providerID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, kp := range c.keys {
		delete(kp.providerGrants, providerID)
		delete(kp.providerBudgets, providerID)
		c.keys[id] = kp
	}
}

// Allowed returns whether toolName is visible/callable for keyID on serverID.
func (c *Cache) Allowed(serverID, keyID, toolName string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.allowedServerLocked(keyID, serverID) {
		return false
	}
	return c.allowedToolLocked(serverID, keyID, toolName)
}

func (c *Cache) allowedServerLocked(keyID, serverID string) bool {
	kp, ok := c.keys[keyID]
	if !ok || kp.serverMode != store.ServerPolicyCustom {
		return true
	}
	_, granted := kp.serverGrants[serverID]
	return granted
}

func (c *Cache) allowedToolLocked(serverID, keyID, toolName string) bool {
	srv, ok := c.servers[serverID]
	if ok {
		if en, exists := srv[toolName]; exists && !en {
			return false
		}
	}
	kp, ok := c.keys[keyID]
	if !ok || kp.toolMode != store.ToolPolicyCustom {
		return true
	}
	_, granted := kp.toolGrants[toolName]
	return granted
}

// EffectiveTools returns visible tool names for keyID on serverID.
func (c *Cache) EffectiveTools(serverID, keyID string) map[string]struct{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := map[string]struct{}{}
	if !c.allowedServerLocked(keyID, serverID) {
		return out
	}
	srv := c.servers[serverID]
	kp, hasKey := c.keys[keyID]
	custom := hasKey && kp.toolMode == store.ToolPolicyCustom

	if custom {
		for name := range kp.toolGrants {
			if srv != nil {
				if en, exists := srv[name]; exists && !en {
					continue
				}
			}
			out[name] = struct{}{}
		}
		return out
	}
	for name, en := range srv {
		if en {
			out[name] = struct{}{}
		}
	}
	return out
}

// FilterToolsList applies catalog-miss rules to an upstream tools/list name set.
func (c *Cache) FilterToolsList(serverID, keyID string, upstreamNames []string) map[string]struct{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := map[string]struct{}{}
	if !c.allowedServerLocked(keyID, serverID) {
		return out
	}
	srv := c.servers[serverID]
	kp, hasKey := c.keys[keyID]
	custom := hasKey && kp.toolMode == store.ToolPolicyCustom

	for _, name := range upstreamNames {
		if srv != nil {
			if en, exists := srv[name]; exists && !en {
				continue
			}
		}
		if custom {
			if _, ok := kp.toolGrants[name]; !ok {
				continue
			}
		}
		out[name] = struct{}{}
	}
	return out
}

// IsCustom returns whether the key uses tool allowlist mode.
func (c *Cache) IsCustom(keyID string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	kp, ok := c.keys[keyID]
	return ok && kp.toolMode == store.ToolPolicyCustom
}
