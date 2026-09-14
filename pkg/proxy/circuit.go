package proxy

import (
	"sync"
	"time"
)

// Circuit states for an upstream MCP server.
const (
	StateClosed   = "closed"
	StateOpen     = "open"
	StateHalfOpen = "half_open"
)

const (
	defaultFailureThreshold = 5
	defaultOpenDuration     = 30 * time.Second
)

// CircuitStatus is the JSON shape for GET /admin/servers/{id}/status.
type CircuitStatus struct {
	State               string     `json:"state"`
	ConsecutiveFailures int        `json:"consecutive_failures"`
	LastFailureAt       *time.Time `json:"last_failure_at"`
}

type circuit struct {
	mu                    sync.Mutex
	state                 string
	failures              int
	openedAt              time.Time
	lastFailureAt         time.Time
	halfOpenProbeInFlight bool
}

// Breaker is a concurrency-safe per-server circuit breaker registry.
//
// WHY not count 4xx / client disconnects: those are not upstream health signals;
// only 5xx, dial failures, and timeouts trip the breaker (AGENTS invariant 8).
type Breaker struct {
	threshold int
	openFor   time.Duration
	now       func() time.Time

	mu   sync.Mutex
	byID map[string]*circuit
}

// NewBreaker creates a breaker with the phase-2 defaults (5 failures / 30s open).
func NewBreaker() *Breaker {
	return &Breaker{
		threshold: defaultFailureThreshold,
		openFor:   defaultOpenDuration,
		now:       func() time.Time { return time.Now().UTC() },
		byID:      make(map[string]*circuit),
	}
}

// SetClock overrides the time source (tests: advance past open cooldown).
func (b *Breaker) SetClock(now func() time.Time) {
	b.now = now
}

// StatusMap is a package-agnostic status snapshot for the dashboard API.
func (b *Breaker) StatusMap(serverID string) map[string]any {
	st := b.Status(serverID)
	return map[string]any{
		"state":                st.State,
		"consecutive_failures": st.ConsecutiveFailures,
		"last_failure_at":      st.LastFailureAt,
	}
}

func (b *Breaker) get(serverID string) *circuit {
	b.mu.Lock()
	defer b.mu.Unlock()
	c, ok := b.byID[serverID]
	if !ok {
		c = &circuit{state: StateClosed}
		b.byID[serverID] = c
	}
	return c
}

// ForceOpen trips the circuit immediately (stdio strike exhaustion).
func (b *Breaker) ForceOpen(serverID string) {
	c := b.get(serverID)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state = StateOpen
	c.openedAt = b.now()
	c.lastFailureAt = c.openedAt
	c.failures = b.threshold
	c.halfOpenProbeInFlight = false
}

// Reset clears circuit state (manual restart / successful stdio start).
func (b *Breaker) Reset(serverID string) {
	c := b.get(serverID)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state = StateClosed
	c.failures = 0
	c.halfOpenProbeInFlight = false
	c.openedAt = time.Time{}
	c.lastFailureAt = time.Time{}
}

// Allow returns false when the circuit is OPEN and the cooldown has not elapsed.
// When OPEN past openFor, transitions to HALF_OPEN and allows exactly one probe.
func (b *Breaker) Allow(serverID string) bool {
	c := b.get(serverID)
	c.mu.Lock()
	defer c.mu.Unlock()

	now := b.now()
	switch c.state {
	case StateOpen:
		if now.Sub(c.openedAt) >= b.openFor {
			c.state = StateHalfOpen
			c.halfOpenProbeInFlight = true
			return true // single probe
		}
		return false
	case StateHalfOpen:
		if c.halfOpenProbeInFlight {
			return false // only one probe at a time
		}
		c.halfOpenProbeInFlight = true
		return true
	default: // closed
		return true
	}
}

// RecordSuccess resets failures and closes the circuit.
// Returns true when the circuit transitions into CLOSED from OPEN/HALF_OPEN.
func (b *Breaker) RecordSuccess(serverID string) (closed bool) {
	c := b.get(serverID)
	c.mu.Lock()
	defer c.mu.Unlock()
	wasOpen := c.state == StateOpen || c.state == StateHalfOpen
	c.failures = 0
	c.state = StateClosed
	c.halfOpenProbeInFlight = false
	return wasOpen
}

// RecordFailure increments consecutive failures; at threshold → OPEN.
// Returns true when the circuit newly transitions to OPEN.
func (b *Breaker) RecordFailure(serverID string) (opened bool) {
	c := b.get(serverID)
	c.mu.Lock()
	defer c.mu.Unlock()
	now := b.now()
	c.lastFailureAt = now
	c.halfOpenProbeInFlight = false

	if c.state == StateHalfOpen {
		c.state = StateOpen
		c.openedAt = now
		c.failures = b.threshold
		return true
	}

	c.failures++
	if c.failures >= b.threshold && c.state != StateOpen {
		c.state = StateOpen
		c.openedAt = now
		return true
	}
	return false
}

// Status returns a snapshot for admin.
func (b *Breaker) Status(serverID string) CircuitStatus {
	c := b.get(serverID)
	c.mu.Lock()
	defer c.mu.Unlock()

	// Lazy OPEN → HALF_OPEN transition for status readers.
	now := b.now()
	state := c.state
	if state == StateOpen && now.Sub(c.openedAt) >= b.openFor {
		state = StateHalfOpen
	}

	st := CircuitStatus{
		State:               state,
		ConsecutiveFailures: c.failures,
	}
	if !c.lastFailureAt.IsZero() {
		t := c.lastFailureAt
		st.LastFailureAt = &t
	}
	return st
}
