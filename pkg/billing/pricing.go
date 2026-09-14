// Package billing — pricing ladder (Phase 12).
//
// Single source of truth for one-time prices and update windows. Caps come
// only from license.CapsForPlan — never duplicated here.
//
// Commercial model (perpetual license + optional paid updates):
//  1. Pay once → you own that version (no auto-charge).
//  2. Updates are time-boxed (12mo standard / 24mo founders).
//  3. Renewal is optional and one-time again; expiry gates updates only,
//     never function (Resolve fail-opens).
//
// Phase 13 (deferred): discounted renewal variants (~45–50% of standard).
// v1 renewal reuses the full plan price / same LS variants.
package billing

import (
	"github.com/devthinker-ai/TokenControlPlane/pkg/license"
)

// DefaultFoundersLimit is the launch offer cap (first N Pro+Team orders).
// Override with TOKENCONTROLPLANE_FOUNDERS_LIMIT: 0 = unlimited, -1 = off.
const DefaultFoundersLimit = 100

// Plan is one rung of the one-time pricing ladder.
type Plan struct {
	Key            string // "pro" | "team"
	Label          string
	PriceUSD       int // standard one-time
	FoundersUSD    int // launch one-time (first FoundersLimit orders)
	WindowMonths   int // standard update window
	FoundersWindow int // founders update window
}

// Ladder is the canonical price/window table. Caps are layered from license.CapsForPlan.
var Ladder = map[string]Plan{
	"pro": {
		Key:            "pro",
		Label:          "Pro",
		PriceUSD:       249,
		FoundersUSD:    179,
		WindowMonths:   12,
		FoundersWindow: 24,
	},
	"team": {
		Key:            "team",
		Label:          "Team",
		PriceUSD:       599,
		FoundersUSD:    449,
		WindowMonths:   12,
		FoundersWindow: 24,
	},
}

// StandardPrice is the list one-time price.
func (p Plan) StandardPrice() int { return p.PriceUSD }

// PriceFor returns founders or standard price.
func (p Plan) PriceFor(founders bool) int {
	if founders {
		return p.FoundersUSD
	}
	return p.PriceUSD
}

// WindowFor returns founders or standard update window in months.
func (p Plan) WindowFor(founders bool) int {
	if founders {
		return p.FoundersWindow
	}
	return p.WindowMonths
}

// CapsFor returns license caps for this plan key (single caps source).
func (p Plan) CapsFor() license.Caps {
	return license.CapsForPlan(p.Key)
}

// PlanFor looks up a ladder plan; ok is false for unknown keys.
func PlanFor(key string) (Plan, bool) {
	p, ok := Ladder[key]
	return p, ok
}

// RenewalNote documents the deferred renewal discount (Phase 13).
// v1: buy again at full PriceUSD — same LS variants, fresh window from purchase.
const RenewalNote = "Phase 13: renewal at ~45–50% of standard (Pro ~$120, Team ~$300) needs dedicated LS variants + checkout flag. v1 renewal = full price."
