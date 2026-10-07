import { describe, expect, it } from 'vitest'

import { areaIdOf, areaNodeId, membersOf, rectFrom } from '../areas'

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
