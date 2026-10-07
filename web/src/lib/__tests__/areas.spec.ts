import { describe, expect, it } from 'vitest'

import { areaIdOf, areaNodeId, fitArea, membersOf, rectFrom, regroup } from '../areas'

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

  it('adds nodes dropped inside and removes members dropped outside', () => {
    const rect = { x: 0, y: 0, width: 400, height: 300 }
    const dropped = [box('a', 900, 900), box('new', 50, 50)]
    expect(regroup(['a', 'b'], rect, dropped)).toEqual(['b', 'new'])
  })
})
