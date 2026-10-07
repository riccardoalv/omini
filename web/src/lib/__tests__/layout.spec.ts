import { describe, expect, it } from 'vitest'

import { alignOn, layoutKey, positionsFor } from '../layout'

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

describe('alignOn', () => {
  const current = { gw: { x: 0, y: 0 }, vm: { x: 300, y: 500 } }
  // After expanding vm, the fresh layout moved everything.
  const fresh = {
    gw: { x: 0, y: 200 },
    vm: { x: 400, y: 100 },
    app1: { x: 700, y: 60 },
    app2: { x: 700, y: 140 },
  }

  it('keeps the anchor where it is and moves the rest with it', () => {
    const out = alignOn(current, fresh, 'vm')
    expect(out.vm).toEqual({ x: 300, y: 500 })
    expect(out.app1).toEqual({ x: 600, y: 460 })
    expect(out.app2).toEqual({ x: 600, y: 540 })
    expect(out.gw).toEqual({ x: -100, y: 600 })
  })

  it('never shifts positions the user saved', () => {
    const out = alignOn(current, { ...fresh, gw: { x: 0, y: 0 } }, 'vm', new Set(['gw']))
    expect(out.gw).toEqual({ x: 0, y: 0 })
    expect(out.vm).toEqual({ x: 300, y: 500 })
  })

  it('uses the fresh layout without an anchor on screen', () => {
    expect(alignOn({}, fresh, 'vm')).toEqual(fresh)
    expect(alignOn(current, fresh, undefined)).toEqual(fresh)
  })
})
