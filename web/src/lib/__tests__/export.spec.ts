import { toSvg } from 'html-to-image'
import { describe, expect, it, vi } from 'vitest'

import { drawnBoxes, exportFrame, exportName, inlineSvgStyles, mapImage, mapJSON } from '../export'
import type { TopologyResponse } from '../types'

vi.mock('html-to-image', () => ({
  toPng: vi.fn<() => Promise<string>>(),
  toSvg: vi.fn<() => Promise<string>>(),
}))

const node = (x: number, y: number, width = 200, height = 40) => ({ x, y, width, height })

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

  it('reads the boxes of the nodes as drawn', () => {
    document.body.innerHTML = `<div id="vp">
      <div class="vue-flow__node" style="transform: translate(-120px, 340.5px)"></div>
      <div class="vue-flow__node" style="transform: translate(900px, 1200px)"></div>
      <div class="vue-flow__node" style="display: none; transform: translate(5000px, 5000px)"></div>
    </div>`
    const boxes = drawnBoxes(document.getElementById('vp')!)
    expect(boxes.map((b) => [b.x, b.y])).toEqual([
      [-120, 340.5],
      [900, 1200],
    ])
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

describe('taking the image', () => {
  it("frames the map on the pane that holds the screen's pan and zoom, then puts it back", async () => {
    document.body.innerHTML = `<div class="vue-flow__viewport">
      <div class="vue-flow__transformationpane" style="transform: translate(192px, 357px) scale(0.7)">
        <div class="vue-flow__node" style="transform: translate(12px, 12px)"></div>
      </div></div>`
    const viewport = document.querySelector<HTMLElement>('.vue-flow__viewport')!
    const pane = document.querySelector<HTMLElement>('.vue-flow__transformationpane')!
    let during = ''
    vi.mocked(toSvg).mockImplementation(async () => {
      during = pane.style.transform
      return 'data:image/svg+xml,'
    })
    await mapImage('svg', viewport, '#000')
    expect(during).toBe('translate(36px, 36px) scale(1)') // 48 px margin − the node at 12
    expect(pane.style.transform).toBe('translate(192px, 357px) scale(0.7)')
    const options = vi.mocked(toSvg).mock.calls[0]![1]!
    expect(options.style).not.toHaveProperty('transform')
  })
})
