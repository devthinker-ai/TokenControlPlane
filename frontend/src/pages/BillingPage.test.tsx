import { describe, expect, it } from 'vitest'
import { PRICING_FALLBACK } from '@/lib/api'
import { planCardBullets, renewPlanFromLicense } from '@/pages/BillingPage'

describe('pricing fallback (phase 21)', () => {
  it('has two plans with unlimited tokens for paid tiers', () => {
    expect(PRICING_FALLBACK.plans).toHaveLength(2)
    const pro = PRICING_FALLBACK.plans.find((p) => p.key === 'pro')!
    const team = PRICING_FALLBACK.plans.find((p) => p.key === 'team')!
    expect(pro.caps.monthly_tokens).toBe(0)
    expect(team.caps.monthly_tokens).toBe(0)
    expect(pro.price).toBe(249)
    expect(team.price).toBe(599)
  })

  it('renders Unlimited tokens for Pro/Team', () => {
    const pro = PRICING_FALLBACK.plans.find((p) => p.key === 'pro')!
    const team = PRICING_FALLBACK.plans.find((p) => p.key === 'team')!
    expect(planCardBullets(pro)).toContain('Unlimited tokens')
    expect(planCardBullets(team)).toContain('Unlimited tokens')
    expect(planCardBullets(pro)).toContain('10 servers · 10 seats')
    expect(planCardBullets(team)).toContain('25 servers · 25 seats')
  })

  it('renewPlan resolves pro and team', () => {
    expect(renewPlanFromLicense('team')).toBe('team')
    expect(renewPlanFromLicense('pro')).toBe('pro')
    expect(renewPlanFromLicense(undefined)).toBe('pro')
  })
})
