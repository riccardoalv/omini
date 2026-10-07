import { describe, expect, it } from 'vitest'

import { speedColor, speedTier } from '../speed'

describe('speed tiers', () => {
  it.each([
    [undefined, 'unknown'],
    [0, 'unknown'],
    [10, '100m'],
    [100, '100m'],
    [1000, '1g'],
    [2500, '2.5g'],
    [5000, '5g'],
    [10000, '10g'],
    [40000, '10g'],
  ])('%s Mbps → %s', (mbps, tier) => {
    expect(speedTier(mbps)).toBe(tier)
  })

  it('maps tiers to CSS variables', () => {
    expect(speedColor(2500)).toBe('var(--speed-2_5g)')
    expect(speedColor()).toBe('var(--speed-unknown)')
  })
})
