import { flushPromises, mount } from '@vue/test-utils'
import { beforeAll, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

import { plugins } from '@/components/__tests__/helpers'
import { api } from '@/lib/api'
import { download, mapImage } from '@/lib/export'
import { prefs } from '@/lib/prefs'

import MapView from '../MapView.vue'

vi.mock('@/lib/api', async (orig) => {
  const mod = await orig<typeof import('@/lib/api')>()
  return {
    ...mod,
    api: {
      ...mod.api,
      topology: vi.fn<typeof mod.api.topology>(),
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

  it('offers the subnets as a filter, not as areas', async () => {
    vi.mocked(api.integrations).mockResolvedValue([])
    vi.mocked(api.createArea).mockImplementation(async (a) => ({
      ...a,
      id: 10 + vi.mocked(api.createArea).mock.calls.length,
    }))
    vi.mocked(api.topology).mockResolvedValue({
      topology: {
        nodes: [
          {
            id: 'dev:fw',
            kind: 'device',
            label: 'fw',
            online: true,
            device: {
              key: 'fw',
              name: 'fw',
              interfaces: [
                { name: 'igc1', description: 'LAN', ips: ['192.168.1.1/24'] },
                { name: 'igc2', description: 'LAB', ips: ['10.0.5.1/24'] },
              ],
            },
          },
          { id: 'pc', kind: 'client', label: 'pc', online: true, ip: '192.168.1.20' },
          { id: 'srv', kind: 'client', label: 'srv', online: true, ip: '10.0.5.9' },
        ],
        edges: [
          { id: 'e1', source: 'dev:fw', target: 'pc', kind: 'inferred' },
          { id: 'e2', source: 'dev:fw', target: 'srv', kind: 'inferred' },
        ],
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
    // No area is made: the subnets are a filter in the toolbar.
    expect(api.createArea).not.toHaveBeenCalled()
    const pick = w.get('[data-test=net-filter]')
    expect(pick.findAll('option').map((o) => o.text())).toEqual([
      'All networks',
      'LAB · 10.0.5.0/24',
      'LAN · 192.168.1.0/24',
    ])
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

  it('draws Wi-Fi networks as nodes that can be dragged but not selected', async () => {
    vi.mocked(api.integrations).mockResolvedValue([])
    vi.mocked(api.topology).mockResolvedValue({
      topology: {
        nodes: [
          { id: 'dev:ap', kind: 'device', label: 'ap', online: true },
          { id: 'mac:1', kind: 'client', label: 'phone', online: true, band: '5ghz' },
        ],
        edges: [
          {
            id: 'e1',
            source: 'dev:ap',
            target: 'mac:1',
            kind: 'wifi',
            source_port: 'Home · 5 GHz',
          },
        ],
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
    const flow = w.findComponent({ name: 'VueFlow' })
    const net = (
      flow.props('nodes') as { id: string; selectable?: boolean; draggable?: boolean }[]
    ).find((n) => n.id === 'wifi:dev:ap:Home · 5 GHz')
    expect(net).toBeDefined()
    expect(net!.selectable).toBe(false)
    expect(net!.draggable).not.toBe(false)
    w.unmount()
  })

  it("folds a Wi-Fi network's clients into a bubble", async () => {
    localStorage.clear()
    vi.mocked(api.integrations).mockResolvedValue([])
    const phones = ['a', 'b', 'c'].map((x) => ({
      id: `mac:${x}`,
      kind: 'client' as const,
      label: x,
      online: true,
      band: '5ghz' as const,
    }))
    vi.mocked(api.topology).mockResolvedValue({
      topology: {
        nodes: [{ id: 'dev:ap', kind: 'device', label: 'ap', online: true }, ...phones],
        edges: phones.map((p) => ({
          id: `e:${p.id}`,
          source: 'dev:ap',
          target: p.id,
          kind: 'wifi' as const,
          source_port: 'Home · 5 GHz',
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
    const w = mount(MapView, { global: { plugins: [...plugins(), router] } })
    await flushPromises()
    const nodes = () => w.findAllComponents({ name: 'TopologyNode' })
    const net = nodes().find((c) => c.props('data').node?.kind === 'ssid')
    expect(net).toBeDefined()
    net!.vm.$emit('toggle')
    await flushPromises()
    const bubble = nodes().find((c) => c.props('data').group)
    expect(bubble?.props('data').group.parentId).toBe('wifi:dev:ap:Home · 5 GHz')
    expect(bubble?.props('data').group.clients).toHaveLength(3)
    w.unmount()
  })

  it('exports the map data as JSON from the toolbar', async () => {
    vi.mocked(api.integrations).mockResolvedValue([])
    vi.mocked(api.topology).mockResolvedValue({
      topology: { nodes: [{ id: 'dev:fw', kind: 'device', label: 'fw', online: true }], edges: [] },
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
    await w.get('[data-test=export]').trigger('click')
    // The dialog is loaded on demand.
    await vi.waitFor(() => w.get('form#export-form'))
    await w.get('[data-test=format-json]').setValue(true)
    await w.get('form#export-form').trigger('submit')
    await flushPromises()
    const [content, name] = vi.mocked(download).mock.calls[0]!
    expect(JSON.parse(content).topology.nodes[0].id).toBe('dev:fw')
    expect(name).toMatch(/^omini-map-\d{4}-\d{2}-\d{2}\.json$/)
    w.unmount()
  })

  it('exports images with every group expanded, then folds them back', async () => {
    localStorage.clear()
    prefs.expanded = []
    prefs.collapsed = []
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
    let drawn: string[] = []
    vi.mocked(mapImage).mockImplementation(async (_format, viewport) => {
      drawn = [...viewport.querySelectorAll('.vue-flow__node')].map((n) =>
        n.getAttribute('data-id')!,
      )
      return 'data:image/png;base64,'
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
    const bubbles = () =>
      w.findAllComponents({ name: 'TopologyNode' }).filter((c) => c.props('data').group).length
    expect(bubbles()).toBe(1)
    await w.get('[data-test=export]').trigger('click')
    // The dialog is loaded on demand.
    await vi.waitFor(() => w.get('form#export-form'))
    await w.get('form#export-form').trigger('submit') // PNG by default
    await vi.waitFor(() => expect(vi.mocked(mapImage)).toHaveBeenCalled(), { timeout: 3000 })
    expect(drawn.filter((id) => id.startsWith('mac:'))).toHaveLength(10)
    await flushPromises()
    await vi.waitFor(() => expect(bubbles()).toBe(1), { timeout: 3000 })
    w.unmount()
  })
})
