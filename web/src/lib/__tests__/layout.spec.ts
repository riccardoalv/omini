import { describe, expect, it } from 'vitest'

import { alignOn, clearSaved, layout, layoutKey, positionsFor } from '../layout'

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

describe('layout with areas', () => {
  it('keeps the nodes of an area together, with no other node inside its box', async () => {
    // gw → a, b, c, d in this order; the area holds a and d (not neighbours).
    const ids = ['gw', 'a', 'b', 'c', 'd']
    const nodes = ids.map((id) => ({ id, width: 200, height: 40 }))
    const edges = ids.slice(1).map((id) => ({ id: `e:${id}`, source: 'gw', target: id }))
    const pad = 24
    const pos = await layout(nodes, edges, {}, 'RIGHT', [
      { id: 'rack', children: ['a', 'd'], padding: [pad, pad, pad, pad] },
    ])

    const box = (id: string) => ({ ...pos[id]!, width: 200, height: 40 })
    const members = [box('a'), box('d')]
    const area = {
      left: Math.min(...members.map((m) => m.x)) - pad,
      top: Math.min(...members.map((m) => m.y)) - pad,
      right: Math.max(...members.map((m) => m.x + m.width)) + pad,
      bottom: Math.max(...members.map((m) => m.y + m.height)) + pad,
    }
    for (const id of ['gw', 'b', 'c']) {
      const b = box(id)
      const overlaps =
        b.x < area.right &&
        b.x + b.width > area.left &&
        b.y < area.bottom &&
        b.y + b.height > area.top
      expect(overlaps, `${id} must stay out of the area`).toBe(false)
    }

    // Without the group, a and d are apart and their neighbours sit between them.
    const plain = await layout(nodes, edges, {}, 'RIGHT')
    const [top, bottom] = [plain.a!.y, plain.d!.y].sort((x, y) => x - y)
    expect(plain.b!.y > top! && plain.b!.y < bottom!).toBe(true)
  })
})

describe('top-down layout', () => {
  const overlap = (a: { x: number; y: number }, b: { x: number; y: number }) =>
    a.x < b.x + 200 && a.x + 200 > b.x && a.y < b.y + 40 && a.y + 40 > b.y

  it('packs many leaf children into a grid instead of one wide row', async () => {
    // gw → 16 phones and a VM; the VM → 17 apps.
    const ids = ['gw', 'vm', ...Array.from({ length: 16 }, (_, i) => `p${i}`)]
    const apps = Array.from({ length: 17 }, (_, i) => `a${i}`)
    const nodes = [...ids, ...apps].map((id) => ({ id, width: 200, height: 40 }))
    const edges = [
      ...ids.slice(1).map((id) => ({ id: `e:${id}`, source: 'gw', target: id })),
      ...apps.map((id) => ({ id: `e:${id}`, source: 'vm', target: id })),
    ]
    const pos = await layout(nodes, edges, {}, 'DOWN')

    const xs = Object.values(pos).map((p) => p.x)
    const width = Math.max(...xs) + 200 - Math.min(...xs)
    expect(width).toBeLessThan(2200) // a single row of 17 would be ~3800
    const all = Object.entries(pos)
    const overlapping = all.flatMap(([a, pa]) =>
      all.filter(([b, pb]) => a < b && overlap(pa, pb)).map(([b]) => `${a}/${b}`),
    )
    expect(overlapping).toEqual([])
    // The VM keeps its place in the tree; its apps are below it.
    expect(pos.a0!.y).toBeGreaterThan(pos.vm!.y)
  })

  it('leaves a few children in the row and keeps saved positions', async () => {
    const nodes = ['gw', 'a', 'b'].map((id) => ({ id, width: 200, height: 40 }))
    const edges = [
      { id: 'e:a', source: 'gw', target: 'a' },
      { id: 'e:b', source: 'gw', target: 'b' },
    ]
    const pos = await layout(nodes, edges, { b: { x: 999, y: 999 } }, 'DOWN')
    expect(pos.a!.y).toBe(pos.gw!.y + 40 + 80)
    expect(pos.b).toEqual({ x: 999, y: 999 })
  })
})

describe('dragged positions', () => {
  const nodes = ['a', 'b', 'c', 'dragged'].map((id) => ({ id, width: 200, height: 40 }))

  it('moves automatic nodes off a dragged one, keeping the dragged one still', () => {
    const pos = {
      a: { x: 300, y: 0 },
      b: { x: 300, y: 52 },
      c: { x: 300, y: 104 },
      dragged: { x: 310, y: 60 }, // dropped on top of b
    }
    const out = clearSaved(pos, nodes, { dragged: pos.dragged }, 'RIGHT')
    expect(out.dragged).toEqual({ x: 310, y: 60 })
    expect(out.a).toEqual({ x: 300, y: 0 })
    // b and c move down the column, below the dragged node and each other.
    expect(out.b!.y).toBeGreaterThanOrEqual(60 + 40 + 12)
    expect(out.c!.y).toBeGreaterThanOrEqual(out.b!.y + 40 + 12)
  })

  it('moves along the row top-down, and leaves layouts without dragged nodes alone', () => {
    const pos = { a: { x: 0, y: 100 }, dragged: { x: 50, y: 100 } }
    const out = clearSaved(pos, nodes, { dragged: pos.dragged }, 'DOWN')
    expect(out.a).toEqual({ x: 50 + 200 + 12, y: 100 })
    const same = { a: { x: 0, y: 0 } }
    expect(clearSaved(same, nodes, {}, 'RIGHT')).toBe(same)
  })
})
