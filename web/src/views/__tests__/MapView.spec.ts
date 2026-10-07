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
      integrations: vi.fn<typeof mod.api.integrations>(),
    },
  }
})

beforeAll(() => {
  // jsdom has no ResizeObserver (Vue Flow measures nodes with it).
  globalThis.ResizeObserver ??= class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof ResizeObserver
})

// Smoke test: the whole map screen mounts (a setup error leaves it blank).
describe('MapView', () => {
  it('mounts with areas and devices', async () => {
    vi.mocked(api.integrations).mockResolvedValue([])
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
      areas: [
        {
          id: 1,
          name: 'Rack',
          color: 'blue',
          direction: 'RIGHT',
          x: 0,
          y: 0,
          width: 100,
          height: 100,
          members: ['tv'],
        },
      ],
    })
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/', component: MapView }],
    })
    const w = mount(MapView, { global: { plugins: [...plugins(), router] } })
    await flushPromises()
    expect(w.find('.map').exists()).toBe(true)
    expect(w.find('.toolbar').exists()).toBe(true)
    expect(w.text()).toContain('1 device')
    w.unmount()
  })

  it('expands a group of clients without opening the side panel', async () => {
    localStorage.clear()
    vi.mocked(api.integrations).mockResolvedValue([])
    const phones = Array.from({ length: 10 }, (_, i) => ({
      id: `mac:${i}`,
      kind: 'client' as const,
      label: `phone-${i}`,
      online: true,
    }))
    vi.mocked(api.topology).mockResolvedValue({
      topology: {
        nodes: [{ id: 'dev:ap', kind: 'device', label: 'ap', online: true }, ...phones],
        edges: phones.map((p) => ({
          id: `e:${p.id}`,
          source: 'dev:ap',
          target: p.id,
          kind: 'inferred' as const,
        })),
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
    const w = mount(MapView, {
      global: { plugins: [...plugins(), router] },
      attachTo: document.body,
    })
    await flushPromises()
    const bubble = w.findAllComponents({ name: 'TopologyNode' }).find((c) => c.props('data').group)
    expect(bubble).toBeDefined()
    bubble!.vm.$emit('toggle') // middle click
    await flushPromises()
    expect(w.find('aside.panel').exists()).toBe(false)
    w.unmount()
  })
})
