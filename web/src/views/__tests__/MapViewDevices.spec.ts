import { flushPromises, mount } from '@vue/test-utils'
import { beforeAll, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

import { plugins } from '@/components/__tests__/helpers'
import { api } from '@/lib/api'

import MapView from '../MapView.vue'

vi.mock('@/lib/api', async (orig) => {
  const mod = await orig<typeof import('@/lib/api')>()
  return {
    ...mod,
    api: {
      ...mod.api,
      topology: vi.fn<typeof mod.api.topology>(),
      inventory: vi.fn<typeof mod.api.inventory>(),
      createArea: vi.fn<typeof mod.api.createArea>(),
      integrations: vi.fn<typeof mod.api.integrations>(),
    },
  }
})

vi.mock('@/lib/export', async (orig) => {
  const mod = await orig<typeof import('@/lib/export')>()
  return {
    ...mod,
    download: vi.fn<typeof mod.download>(),
    mapImage: vi.fn<typeof mod.mapImage>(),
  }
})

beforeAll(() => {
  // jsdom has no ResizeObserver (Vue Flow measures nodes with it).
  globalThis.ResizeObserver ??= class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof ResizeObserver
  // Nor DOMMatrixReadOnly (Vue Flow reads the zoom with it after a new layout).
  globalThis.DOMMatrixReadOnly ??= class {
    m22 = 1
  } as unknown as typeof DOMMatrixReadOnly
})

describe('MapView device list', () => {
  it('opens from the devices chip and from /devices, and shows a device on the map', async () => {
    vi.mocked(api.integrations).mockResolvedValue([])
    vi.mocked(api.inventory).mockResolvedValue([
      {
        id: 'tv',
        kind: 'client',
        label: 'tv',
        online: true,
        first_seen: '2026-10-07T00:00:00Z',
        last_seen: '2026-10-07T00:00:00Z',
      },
    ] as never)
    vi.mocked(api.topology).mockResolvedValue({
      topology: {
        nodes: [
          { id: 'dev:fw', kind: 'device', label: 'fw', online: true },
          { id: 'tv', kind: 'client', label: 'tv', online: true },
        ],
        edges: [{ id: 'e', source: 'dev:fw', target: 'tv', kind: 'inferred' }],
      },
      statuses: [],
      generated_at: '2026-10-07T00:00:00Z',
      layout: {},
      areas: [],
    })
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/', component: MapView }],
    })
    const w = mount(MapView, { global: { plugins: [...plugins(), router] } })
    await flushPromises()
    expect(w.find('[data-test=devices-drawer]').exists()).toBe(false)
    await w.get('[data-test=devices-chip]').trigger('click')
    await flushPromises()
    expect(w.find('[data-test=devices-drawer]').exists()).toBe(true)
    // A click on a name closes the list and opens the device.
    await w.get('[data-test=device-name]').trigger('click')
    expect(w.find('[data-test=devices-drawer]').exists()).toBe(false)
    w.unmount()

    await router.push('/?devices=1')
    const again = mount(MapView, { global: { plugins: [...plugins(), router] } })
    await flushPromises()
    expect(again.find('[data-test=devices-drawer]').exists()).toBe(true)
    again.unmount()
  })
})

describe('MapView areas and bubbles', () => {
  it('draws an area around the bubble its members were folded into, not where they were', async () => {
    vi.mocked(api.integrations).mockResolvedValue([])
    vi.mocked(api.inventory).mockResolvedValue([])
    const tvs = ['tv1', 'tv2', 'tv3'].map((id) => ({
      id,
      kind: 'client' as const,
      label: id,
      online: true,
    }))
    vi.mocked(api.topology).mockResolvedValue({
      topology: {
        nodes: [{ id: 'dev:fw', kind: 'device', label: 'fw', online: true }, ...tvs],
        edges: tvs.map((t) => ({
          id: `e:${t.id}`,
          source: 'dev:fw',
          target: t.id,
          kind: 'inferred' as const,
        })),
      },
      statuses: [],
      generated_at: '2026-10-07T00:00:00Z',
      layout: {},
      areas: [
        {
          id: 1,
          name: 'Living',
          color: 'blue',
          direction: 'RIGHT',
          x: 5000,
          y: 5000,
          width: 100,
          height: 100,
          members: ['tv1', 'tv2'],
        },
      ],
    })
    const { prefs } = await import('@/lib/prefs')
    prefs.collapsed = ['dev:fw'] // the TVs are folded into a bubble
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/', component: MapView }],
    })
    const w = mount(MapView, { global: { plugins: [...plugins(), router] } })
    await flushPromises()
    const areaNode = w.findAllComponents({ name: 'AreaNode' })
    expect(areaNode).toHaveLength(1)
    // Not at its old place (5000, 5000): fitted around the bubble.
    expect(areaNode[0]!.props('area').x).not.toBe(5000)
    prefs.collapsed = []
    w.unmount()
  })
})
