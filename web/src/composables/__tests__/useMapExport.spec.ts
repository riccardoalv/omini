import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ref, shallowRef } from 'vue'

import { download } from '@/lib/export'
import { prefs } from '@/lib/prefs'
import type { TopologyResponse } from '@/lib/types'

import { screenTheme, useMapExport } from '../useMapExport'
import { useMapGraph } from '../useMapGraph'
import { area, star, withSetup } from './helpers'

vi.mock('@/lib/export', async (orig) => {
  const mod = await orig<typeof import('@/lib/export')>()
  return { ...mod, download: vi.fn<typeof mod.download>() }
})

beforeEach(() => {
  vi.clearAllMocks()
  prefs.expanded = []
  prefs.collapsed = []
  prefs.layoutDirection = 'RIGHT'
  prefs.theme = 'dark'
})

function setup() {
  const data = shallowRef<TopologyResponse>(star('dev:ap', 10))
  const g = useMapGraph(data, ref([]))
  const mapEl = document.createElement('div')
  mapEl.innerHTML = '<div class="vue-flow__viewport"></div>'
  // Records what the map looked like while it was laid out for the export.
  const seen: { groups: number; direction: string }[] = []
  const untilLaidOut = vi.fn<(change: () => void) => Promise<void>>(async (change) => {
    change()
    seen.push({ groups: g.view.value.groups.length, direction: g.direction.value })
  })
  const { result } = withSetup(() =>
    useMapExport({
      data,
      view: g.view,
      positions: ref({ 'dev:ap': { x: 10, y: 20 } }),
      visibleAreas: ref([area(4, ['dev:ap'], { name: 'Rack' })]),
      linkLabels: ref(new Map()),
      mapEl: ref(mapEl),
      expandAll: g.expandAll,
      exportDirection: g.exportDirection,
      untilLaidOut,
    }),
  )
  return { e: result, g, seen }
}

describe('useMapExport', () => {
  it('saves the data as JSON without laying the map out', async () => {
    const { e, seen } = setup()
    e.exportOpen.value = true
    await e.exportMap({ format: 'json', theme: 'dark', direction: 'RIGHT' })
    const [content, name] = vi.mocked(download).mock.calls[0]!
    expect(JSON.parse(content).topology.nodes[0].id).toBe('dev:ap')
    expect(name).toMatch(/\.json$/)
    expect(seen).toEqual([])
    expect(e.exportOpen.value).toBe(false)
  })

  it('lays the map out expanded in the orientation picked, then puts it back', async () => {
    const { e, g, seen } = setup()
    expect(g.view.value.groups).toHaveLength(1)
    await e.exportMap({ format: 'drawio', theme: 'light', direction: 'DOWN' })
    expect(seen).toEqual([
      { groups: 0, direction: 'DOWN' },
      { groups: 1, direction: 'RIGHT' },
    ])
    const [content, name, type] = vi.mocked(download).mock.calls[0]!
    expect(name).toMatch(/\.drawio$/)
    expect(type).toBe('application/vnd.jgraph.mxfile')
    expect(content).toContain('phone-9')
    expect(content).toContain('Rack')
    expect(e.exporting.value).toBe(false)
    expect(e.exportError.value).toBe('')
    // The screen's theme is back.
    expect(document.documentElement.dataset.theme).toBe('dark')
  })

  it('resolves the system theme', () => {
    expect(screenTheme()).toBe('dark')
    prefs.theme = 'system'
    window.matchMedia = vi.fn<typeof window.matchMedia>(() => ({ matches: true }) as MediaQueryList)
    expect(screenTheme()).toBe('light')
  })
})
