import { describe, expect, it } from 'vitest'

import {
  alignOn,
  clearSaved,
  LAYER_GAP,
  layout,
  layoutKey,
  modelOrder,
  positionsFor,
} from '../layout'

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
    expect(pos.a!.y).toBe(pos.gw!.y + 40 + LAYER_GAP.DOWN)
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

describe('modelOrder', () => {
  it('walks from the roots, grouping children by the port or network they use', () => {
    const edges = [
      { source: 'ap', target: 'tv', source_port: '5 GHz' },
      { source: 'ap', target: 'phone', source_port: '2.4 GHz' },
      { source: 'ap', target: 'laptop', source_port: '5 GHz' },
      { source: 'ap', target: 'bulb', source_port: '2.4 GHz' },
      { source: 'gw', target: 'ap', source_port: 'Port 2' },
      { source: 'gw', target: 'nas', source_port: 'Port 1' },
    ]
    const ids = ['tv', 'phone', 'laptop', 'bulb', 'ap', 'nas', 'gw']
    expect(modelOrder(ids, edges, (id) => id)).toEqual([
      'gw',
      'nas', // Port 1
      'ap', // Port 2
      'bulb', // 2.4 GHz, by name
      'phone',
      'laptop', // 5 GHz
      'tv',
    ])
  })
})

describe('sibling order in the layout', () => {
  it('keeps the model order of children, so groups do not interleave', async () => {
    // ap → net A (a1, a2) and net B (b1, b2): given in that order.
    const ids = ['ap', 'netA', 'netB', 'a1', 'a2', 'b1', 'b2']
    const nodes = ids.map((id) => ({ id, width: 150, height: 30 }))
    const edges = [
      { id: 'e1', source: 'ap', target: 'netA' },
      { id: 'e2', source: 'ap', target: 'netB' },
      { id: 'e3', source: 'netA', target: 'a1' },
      { id: 'e4', source: 'netA', target: 'a2' },
      { id: 'e5', source: 'netB', target: 'b1' },
      { id: 'e6', source: 'netB', target: 'b2' },
    ]
    const pos = await layout(nodes, edges, {}, 'RIGHT')
    const order = ['a1', 'a2', 'b1', 'b2'].sort((x, y) => pos[x]!.y - pos[y]!.y)
    expect(order).toEqual(['a1', 'a2', 'b1', 'b2'])
    expect(pos.netA!.y).toBeLessThan(pos.netB!.y)
  })
})

describe('top-down grids keep their place', () => {
  it("puts a parent's grid of leaves under it, before the next subtree's nodes", async () => {
    // gw → a (5 leaves: a grid) and b → c → d. Layer 2 holds a's grid and c.
    const leaves = Array.from({ length: 5 }, (_, i) => `a${i}`)
    const ids = ['gw', 'a', ...leaves, 'b', 'c', 'd']
    const nodes = ids.map((id) => ({ id, width: 200, height: 40 }))
    const edges = [
      { id: 'e:a', source: 'gw', target: 'a' },
      { id: 'e:b', source: 'gw', target: 'b' },
      ...leaves.map((k) => ({ id: `e:${k}`, source: 'a', target: k })),
      { id: 'e:c', source: 'b', target: 'c' },
      { id: 'e:d', source: 'c', target: 'd' },
    ]
    const pos = await layout(nodes, edges, {}, 'DOWN')
    const right = Math.max(...leaves.map((k) => pos[k]!.x + 200))
    expect(pos.a!.x).toBeLessThan(pos.b!.x)
    expect(right).toBeLessThanOrEqual(pos.c!.x) // a's grid stays left of b's subtree
  })
})

describe('areas keep their place', () => {
  it('puts an area where its first node comes, not after every other one', async () => {
    // sw → p1 (in an area, with a child), p2, p5 — given in this order.
    const ids = ['sw', 'p1', 'vm', 'p2', 'p5']
    const nodes = ids.map((id) => ({ id, width: 200, height: 40 }))
    const edges = [
      { id: 'e1', source: 'sw', target: 'p1' },
      { id: 'e2', source: 'p1', target: 'vm' },
      { id: 'e3', source: 'sw', target: 'p2' },
      { id: 'e4', source: 'sw', target: 'p5' },
    ]
    const pos = await layout(nodes, edges, {}, 'RIGHT', [
      { id: 'rack', children: ['p1', 'vm'], padding: [24, 24, 24, 24] },
    ])
    expect(pos.p1!.y).toBeLessThan(pos.p2!.y)
    expect(pos.p2!.y).toBeLessThan(pos.p5!.y)
  })
})

describe('tree layout', () => {
  // wan → fw → sw → {ap → {c1, c2, c3}, srv}; a second WAN also feeds fw.
  const nodes = [
    { id: 'wan', width: 160, height: 50 },
    { id: 'wan2', width: 160, height: 50 },
    { id: 'fw', width: 220, height: 60 },
    { id: 'sw', width: 220, height: 60 },
    { id: 'ap', width: 220, height: 60 },
    { id: 'srv', width: 220, height: 60 },
    { id: 'c1', width: 200, height: 38 },
    { id: 'c2', width: 200, height: 38 },
    { id: 'c3', width: 200, height: 38 },
  ]
  const edges = [
    { id: '1', source: 'wan', target: 'fw' },
    { id: '2', source: 'wan2', target: 'fw' },
    { id: '3', source: 'fw', target: 'sw' },
    { id: '4', source: 'sw', target: 'ap' },
    { id: '5', source: 'sw', target: 'srv' },
    { id: '6', source: 'ap', target: 'c1' },
    { id: '7', source: 'ap', target: 'c2' },
    { id: '8', source: 'ap', target: 'c3' },
  ]
  const center = (pos: Record<string, { y: number }>, id: string) =>
    pos[id]!.y + nodes.find((n) => n.id === id)!.height / 2

  it('puts each level in one column and a parent in the middle of its children', async () => {
    const pos = await layout(nodes, edges, {}, 'RIGHT')
    // Columns: every node of a level starts at the same x.
    expect(pos.wan!.x).toBe(pos.wan2!.x)
    expect(pos.ap!.x).toBe(pos.srv!.x)
    expect(new Set(['c1', 'c2', 'c3'].map((id) => pos[id]!.x)).size).toBe(1)
    expect(pos.fw!.x).toBe(pos.wan!.x + 160 + LAYER_GAP.RIGHT)
    // The AP sits in the middle of its clients; the switch in the middle of its two.
    expect(center(pos, 'ap')).toBeCloseTo((center(pos, 'c1') + center(pos, 'c3')) / 2)
    expect(center(pos, 'sw')).toBeCloseTo((center(pos, 'ap') + center(pos, 'srv')) / 2)
    // The second WAN stands next to the first, both in front of the firewall.
    expect(Math.abs(pos.wan!.y - pos.wan2!.y)).toBeGreaterThanOrEqual(50)
    expect(center(pos, 'fw')).toBeCloseTo((center(pos, 'wan') + center(pos, 'wan2')) / 2)
  })

  it('lays top down in rows', async () => {
    const pos = await layout(nodes, edges, {}, 'DOWN')
    expect(pos.ap!.y).toBe(pos.srv!.y)
    expect(pos.sw!.y).toBe(pos.fw!.y + 60 + LAYER_GAP.DOWN)
    const mid = (id: string) => pos[id]!.x + nodes.find((n) => n.id === id)!.width / 2
    expect(mid('sw')).toBeCloseTo((mid('ap') + mid('srv')) / 2)
  })

  it('leaves room for an area: its padding across and its border before the level', async () => {
    const pad = 24
    const title = 30
    const group = {
      id: 'lan',
      children: ['ap', 'c1', 'c2', 'c3'],
      padding: [pad + title, pad, pad, pad] as [number, number, number, number],
    }
    const pos = await layout(nodes, edges, {}, 'RIGHT', [group])
    // The AP column moves right by the area's left padding.
    expect(pos.ap!.x).toBe(pos.sw!.x + 220 + LAYER_GAP.RIGHT + pad)
    // The server (outside) stays clear of the area's box, title included.
    const members = ['ap', 'c1', 'c2', 'c3'].map((id) => ({
      ...pos[id]!,
      ...nodes.find((n) => n.id === id)!,
    }))
    const top = Math.min(...members.map((m) => m.y)) - pad - title
    const bottom = Math.max(...members.map((m) => m.y + m.height)) + pad
    const srv = pos.srv!
    expect(srv.y >= bottom || srv.y + 60 <= top).toBe(true)
  })
})

describe('nodes without links in an area', () => {
  it('stand with the rest of their area, not apart', async () => {
    // fw → sw → {pc, tv}; "pve" has no link yet but is in the LAN area with sw.
    const nodes = ['fw', 'sw', 'pc', 'tv', 'pve'].map((id) => ({ id, width: 200, height: 40 }))
    const edges = [
      { id: '1', source: 'fw', target: 'sw' },
      { id: '2', source: 'sw', target: 'pc' },
      { id: '3', source: 'sw', target: 'tv' },
    ]
    const lan = {
      id: 'lan',
      children: ['sw', 'pc', 'tv', 'pve'],
      padding: [54, 24, 24, 24] as [number, number, number, number],
    }
    const pos = await layout(nodes, edges, {}, 'RIGHT', [lan])
    // Same column as the switch (a sibling under the firewall), not a root of its own.
    expect(pos.pve!.x).toBe(pos.sw!.x)
    // The firewall stays out of the area.
    const members = ['sw', 'pc', 'tv', 'pve'].map((id) => pos[id]!)
    expect(pos.fw!.x + 200).toBeLessThan(Math.min(...members.map((m) => m.x)) - 24)
  })
})

describe('dragged nodes in areas', () => {
  it('are placed with their area unless the whole area was moved', async () => {
    const nodes = ['fw', 'sw', 'pve', 'vm1', 'vm2'].map((id) => ({ id, width: 200, height: 40 }))
    const edges = [
      { id: '1', source: 'fw', target: 'sw' },
      { id: '2', source: 'sw', target: 'pve' },
      { id: '3', source: 'pve', target: 'vm1' },
      { id: '4', source: 'pve', target: 'vm2' },
    ]
    const rack = {
      id: 'rack',
      children: ['pve', 'vm1', 'vm2'],
      padding: [54, 24, 24, 24] as [number, number, number, number],
    }
    // One VM dragged far away once: ignored, it stays in the area's band.
    const pos = await layout(
      nodes,
      edges,
      { vm1: { x: 5000, y: 5000 }, fw: { x: -900, y: 0 } },
      'RIGHT',
      [rack],
    )
    expect(pos.vm1!.x).toBe(pos.vm2!.x)
    expect(pos.fw).toEqual({ x: -900, y: 0 }) // outside areas, a dragged node stays
    // The whole area moved: its members keep their places.
    const moved = { pve: { x: 3000, y: 0 }, vm1: { x: 3400, y: -50 }, vm2: { x: 3400, y: 50 } }
    const kept = await layout(nodes, edges, moved, 'RIGHT', [rack])
    expect(kept.vm1).toEqual(moved.vm1)
  })
})
