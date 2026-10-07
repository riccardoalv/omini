import type { GraphNode } from '@vue-flow/core'
import { describe, expect, it, vi } from 'vitest'

import { exportFrame, exportName, inlineSvgStyles, mapJSON } from '../export'
import type { TopologyResponse } from '../types'

const node = (x: number, y: number, width = 200, height = 40) =>
  ({
    id: `${x},${y}`,
    computedPosition: { x, y, z: 0 },
    position: { x, y },
    dimensions: { width, height },
  }) as unknown as GraphNode

describe('map export', () => {
  it('frames every node with a margin, at full size when it fits', () => {
    const f = exportFrame([node(-100, 50), node(900, 650)])
    // nodes span x -100…1100, y 50…690; plus 48 px around
    expect(f.width).toBe(1200 + 96)
    expect(f.height).toBe(640 + 96)
    expect(f.transform).toBe('translate(148px, -2px) scale(1)')
  })

  it('scales a huge map down to the largest side', () => {
    const f = exportFrame([node(0, 0), node(20000, 0)])
    expect(Math.max(f.width, f.height)).toBeLessThanOrEqual(8192)
    expect(f.transform).toMatch(/scale\(0\.\d+\)/)
  })

  it('names files by date and exports the data as JSON', () => {
    expect(exportName('png', new Date('2026-10-07T12:00:00Z'))).toBe('omini-map-2026-10-07.png')
    const data = {
      topology: { nodes: [{ id: 'a', kind: 'device', label: 'a', online: true }], edges: [] },
      statuses: [],
      generated_at: '2026-10-07T00:00:00Z',
      layout: { a: { x: 1, y: 2 } },
      areas: [],
    } as TopologyResponse
    const json = JSON.parse(mapJSON(data))
    expect(Object.keys(json).sort()).toEqual(['areas', 'generated_at', 'layout', 'topology'])
    expect(json.topology.nodes[0].id).toBe('a')
    vi.restoreAllMocks()
  })
})

describe('SVG styles while exporting', () => {
  it('inlines the computed fill and stroke of the wires, then restores them', () => {
    document.body.innerHTML = `
      <style>.wire { fill: none; stroke: rgb(1, 2, 3); }</style>
      <div id="vp"><svg><path class="wire" d="M0 0L10 10"/><path style="opacity: 0.5" d="M0 0"/></svg></div>`
    const [wire, other] = [...document.querySelectorAll('path')]
    const restore = inlineSvgStyles(document.getElementById('vp')!)
    expect(['none', 'rgba(0, 0, 0, 0)']).toContain(wire!.style.fill) // jsdom says transparent
    expect(wire!.style.stroke).toBe('rgb(1, 2, 3)')
    restore()
    expect(wire!.getAttribute('style')).toBeNull()
    expect(other!.getAttribute('style')).toBe('opacity: 0.5')
  })
})
