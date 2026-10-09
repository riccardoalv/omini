import { flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, ref, shallowRef } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'

import { prefs } from '@/lib/prefs'
import type { TopologyResponse } from '@/lib/types'

import { useFolding } from '../useFolding'
import { useMapFocus } from '../useMapFocus'
import { useMapGraph } from '../useMapGraph'
import { star, withSetup } from './helpers'

beforeEach(() => {
  // The map is centered on the node later: not in these tests.
  vi.useFakeTimers({ toFake: ['setTimeout'] })
  prefs.expanded = []
  prefs.collapsed = []
  prefs.collapseThreshold = 8
})
afterEach(() => {
  vi.useRealTimers()
})

async function setup(url: string) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/', component: defineComponent({ render: () => null }) }],
  })
  await router.push(url)
  const topo = star('dev:ap', 10)
  for (const n of topo.topology.nodes.slice(1)) n.parent_id = 'dev:ap'
  const g = useMapGraph(shallowRef<TopologyResponse>(topo), ref([]))
  const selectedId = ref<string>()
  const { result } = withSetup(() => {
    const f = useFolding(g.view, g.mapGraph, selectedId)
    return useMapFocus({ nodeById: g.nodeById, view: g.view, selectedId, expand: f.expand })
  }, [router])
  return { focus: result, router, selectedId, g }
}

describe('useMapFocus', () => {
  it('opens the device list from /devices and takes the flag off the address', async () => {
    const { focus, router } = await setup('/?devices=1')
    expect(focus.devicesOpen.value).toBe(true)
    await flushPromises()
    expect(router.currentRoute.value.query.devices).toBeUndefined()
  })

  it('opens ?node=, unfolding the bubble it is in, once', async () => {
    const { focus, router, selectedId, g } = await setup('/?node=mac:3')
    expect(g.view.value.groups).toHaveLength(1)
    focus.focusRequested()
    expect(selectedId.value).toBe('mac:3')
    expect(prefs.expanded).toEqual(['dev:ap'])
    expect(g.view.value.groups).toHaveLength(0)
    await flushPromises()
    expect(router.currentRoute.value.query.node).toBeUndefined()
  })

  it('shows a device picked in the list, closing it', async () => {
    const { focus, selectedId } = await setup('/')
    focus.devicesOpen.value = true
    focus.showDevice('mac:1')
    expect(focus.devicesOpen.value).toBe(false)
    expect(selectedId.value).toBe('mac:1')
    focus.showDevice('nope') // not on the map: nothing happens
    expect(selectedId.value).toBe('mac:1')
  })
})
