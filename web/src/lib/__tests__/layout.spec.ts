import { describe, expect, it } from 'vitest'

import { layoutKey, positionsFor } from '../layout'

describe('per-direction layout positions', () => {
  it('keys left-to-right positions by node id and top-down ones with a prefix', () => {
    expect(layoutKey('RIGHT', 'dev:aa')).toBe('dev:aa')
    expect(layoutKey('DOWN', 'dev:aa')).toBe('DOWN:dev:aa')
  })

  it('splits the saved layout by direction', () => {
    const saved = {
      'dev:aa': { x: 1, y: 1 },
      'DOWN:dev:aa': { x: 2, y: 2 },
      'DOWN:mac:bb': { x: 3, y: 3 },
    }
    expect(positionsFor(saved, 'RIGHT')).toEqual({ 'dev:aa': { x: 1, y: 1 } })
    expect(positionsFor(saved, 'DOWN')).toEqual({
      'dev:aa': { x: 2, y: 2 },
      'mac:bb': { x: 3, y: 3 },
    })
  })
})
