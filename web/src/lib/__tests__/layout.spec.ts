import { describe, expect, it } from 'vitest'

import { anchorNewNodes, layoutKey, positionsFor } from '../layout'

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

describe('anchorNewNodes', () => {
  const parents: Record<string, string> = { vm: 'gw', app1: 'vm', app2: 'vm', grp: 'vm' }
  const parentOf = (id: string) => parents[id]

  it('keeps nodes on screen where they are', () => {
    const current = { gw: { x: 0, y: 0 }, vm: { x: 300, y: 500 } }
    const fresh = { gw: { x: 0, y: 900 }, vm: { x: 400, y: 100 } } // the whole layout shifted
    expect(anchorNewNodes(current, fresh, parentOf)).toEqual(current)
  })

  it('places new children next to their parent with the computed offsets', () => {
    const current = { gw: { x: 0, y: 0 }, vm: { x: 300, y: 500 } }
    const fresh = {
      gw: { x: 0, y: 900 },
      vm: { x: 400, y: 100 },
      app1: { x: 700, y: 60 },
      app2: { x: 700, y: 140 },
    }
    const out = anchorNewNodes(current, fresh, parentOf)
    expect(out.app1).toEqual({ x: 600, y: 460 })
    expect(out.app2).toEqual({ x: 600, y: 540 })
    expect(out.vm).toEqual({ x: 300, y: 500 })
  })

  it('uses the fresh position when no ancestor is on screen (first layout)', () => {
    const fresh = { gw: { x: 0, y: 0 }, vm: { x: 300, y: 0 } }
    expect(anchorNewNodes({}, fresh, parentOf)).toEqual(fresh)
  })
})
