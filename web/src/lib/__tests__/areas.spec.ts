import { describe, expect, it } from 'vitest'

import {
  areaIdOf,
  areaNodeId,
  clusterFrames,
  collapseAreas,
  fitArea,
  membersOf,
  rectFrom,
  withDescendants,
} from '../areas'

describe('map areas', () => {
  it('maps area ids to flow node ids and back', () => {
    expect(areaNodeId(7)).toBe('area:7')
    expect(areaIdOf('area:7')).toBe(7)
    expect(areaIdOf('mac:aa:bb')).toBeUndefined()
    expect(areaIdOf('area:x')).toBeUndefined()
  })

  it('builds a rectangle drawn in any direction', () => {
    expect(rectFrom({ x: 300, y: 50 }, { x: 100, y: 250 })).toEqual({
      x: 100,
      y: 50,
      width: 200,
      height: 200,
    })
  })

  it('takes the nodes whose center is inside the area', () => {
    const area = { x: 0, y: 0, width: 300, height: 200 }
    const boxes = [
      { id: 'inside', x: 20, y: 20, width: 200, height: 60 },
      { id: 'half-out', x: 200, y: 20, width: 200, height: 60 }, // center x = 300: on the edge
      { id: 'mostly-out', x: 250, y: 20, width: 200, height: 60 },
      { id: 'below', x: 20, y: 190, width: 200, height: 60 },
    ]
    expect(membersOf(area, boxes)).toEqual(['inside', 'half-out'])
  })
})

describe('area membership', () => {
  const area = {
    id: 1,
    name: 'Rack',
    color: 'blue' as const,
    direction: 'RIGHT' as const,
    x: 0,
    y: 0,
    width: 100,
    height: 100,
    members: ['a', 'b', 'gone'],
  }
  const box = (id: string, x: number, y: number) => ({ id, x, y, width: 200, height: 60 })

  it('is drawn around its members wherever the layout put them', () => {
    const boxes = new Map([
      ['a', box('a', 500, 300)],
      ['b', box('b', 500, 400)],
      ['other', box('other', 0, 0)],
    ])
    expect(fitArea(area, boxes)).toEqual({ x: 476, y: 276, width: 248, height: 208 })
  })

  it('keeps the stored rectangle when no member is on the map', () => {
    expect(fitArea(area, new Map())).toEqual({ x: 0, y: 0, width: 100, height: 100 })
  })
})

describe('withDescendants', () => {
  it('adds every node below the members, once, even with cycles', () => {
    const edges = [
      { source: 'gw', target: 'pve' },
      { source: 'pve', target: 'vm' },
      { source: 'vm', target: 'app' },
      { source: 'vm', target: 'group:vm' },
      { source: 'gw', target: 'phone' },
      { source: 'app', target: 'vm' }, // never expected, but must not loop
    ]
    expect(withDescendants(['pve'], edges).sort()).toEqual(['app', 'group:vm', 'pve', 'vm'])
    expect(withDescendants([], edges)).toEqual([])
  })
})

describe('collapseAreas', () => {
  const node = (id: string, online = true) => ({ id, kind: 'client' as const, label: id, online })
  const e = (source: string, target: string) => ({
    id: `${source}>${target}`,
    source,
    target,
    kind: 'inferred' as const,
  })
  const rack = {
    id: 3,
    name: 'Rack',
    color: 'blue' as const,
    direction: 'RIGHT' as const,
    x: 0,
    y: 0,
    width: 100,
    height: 100,
    members: ['pve'],
  }

  it('replaces the devices of an area (and their children) with one bubble', () => {
    const view = {
      nodes: [node('gw'), node('pve'), node('vm'), node('vm2', false), node('tv')],
      edges: [e('gw', 'pve'), e('pve', 'vm'), e('pve', 'vm2'), e('gw', 'tv'), e('vm', 'tv2')],
      groups: [],
    }
    const out = collapseAreas(view, [rack])
    expect(out.nodes.map((n) => n.id)).toEqual(['gw', 'tv'])
    const bubble = out.groups[0]!
    expect(bubble).toMatchObject({
      id: 'area-bubble:3',
      parentId: 'gw',
      online: 2,
      area: { name: 'Rack' },
    })
    expect(bubble.clients.map((n) => n.id)).toEqual(['pve', 'vm', 'vm2'])
    expect(out.edges.map((x) => `${x.source}>${x.target}`)).toEqual(['gw>area-bubble:3', 'gw>tv'])
  })

  it('leaves the view alone without collapsed areas', () => {
    const view = { nodes: [node('gw')], edges: [], groups: [] }
    expect(collapseAreas(view, [])).toBe(view)
  })
})

describe('clusterFrames', () => {
  it('frames each linked group of nodes apart', () => {
    const box = (id: string, x: number, y: number) =>
      [id, { id, x, y, width: 100, height: 40 }] as const
    const boxes = new Map([
      box('sw1', 0, 0),
      box('a', 200, 0),
      box('sw2', 0, 400),
      box('b', 200, 400),
      box('c', 200, 200),
    ])
    const edges = [
      { source: 'sw1', target: 'a' },
      { source: 'sw2', target: 'b' },
      { source: 'sw1', target: 'c' },
    ]
    // sw1 + a linked, b alone (its switch is not highlighted), c not highlighted.
    const frames = clusterFrames(new Set(['sw1', 'a', 'b', 'missing']), edges, boxes, 10)
    expect(frames).toEqual([
      { id: 'a', x: -10, y: -10, width: 320, height: 60 },
      { id: 'b', x: 190, y: 390, width: 120, height: 60 },
    ])
  })
})
