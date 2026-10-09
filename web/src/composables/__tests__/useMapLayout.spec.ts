import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ref, shallowRef } from 'vue'

import { api } from '@/lib/api'
import { prefs } from '@/lib/prefs'
import type { MapArea, TopologyResponse } from '@/lib/types'

import { useMapGraph } from '../useMapGraph'
import { useMapLayout } from '../useMapLayout'
import { area, star, withSetup } from './helpers'

vi.mock('@/lib/api', async (orig) => {
  const mod = await orig<typeof import('@/lib/api')>()
  return { ...mod, api: { ...mod.api, saveLayout: vi.fn<typeof mod.api.saveLayout>() } }
})

beforeEach(() => {
  vi.clearAllMocks()
  prefs.expanded = []
  prefs.collapsed = []
  prefs.layoutDirection = 'RIGHT'
})

function setup(areas: MapArea[] = []) {
  const data = shallowRef<TopologyResponse>()
  const g = useMapGraph(data, ref(areas))
  const { result } = withSetup(() =>
    useMapLayout({
      data,
      view: g.view,
      direction: g.direction,
      liveAreas: g.liveAreas,
      onMap: g.onMap,
      takeAnchor: () => undefined,
    }),
  )
  return { l: result, data }
}

describe('useMapLayout', () => {
  it('lays the map out when the graph arrives, using the positions saved', async () => {
    const { l, data } = setup()
    data.value = { ...star('dev:fw', 2), layout: { 'mac:1': { x: 900, y: 900 } } }
    await vi.waitFor(() => expect(Object.keys(l.positions.value)).toHaveLength(3))
    expect(l.positions.value['mac:1']).toEqual({ x: 900, y: 900 })
    expect(l.nodeBoxes.value.get('dev:fw')).toMatchObject({ width: 220, height: 60 })
  })

  it('keeps dragged positions rounded, saved for the orientation on screen', async () => {
    const { l } = setup()
    prefs.layoutDirection = 'DOWN'
    await l.keep({ a: { x: 10.6, y: 20.2 } })
    expect(l.positions.value.a).toEqual({ x: 11, y: 20 })
    expect(api.saveLayout).toHaveBeenCalledWith({ 'DOWN:a': { x: 11, y: 20 } })
    await l.keep({})
    expect(api.saveLayout).toHaveBeenCalledTimes(1) // nothing moved: nothing saved
  })

  it('waits for the layout a change brings', async () => {
    const { l, data } = setup([area(1, ['mac:0'])])
    await l.untilLaidOut(() => {
      data.value = star('dev:fw', 1)
    })
    expect(l.positions.value['mac:0']).toBeDefined()
    await flushPromises()
  })
})
