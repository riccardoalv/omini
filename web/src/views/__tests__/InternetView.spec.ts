import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

import { plugins } from '@/components/__tests__/helpers'
import { api } from '@/lib/api'
import type { InternetResponse } from '@/lib/types'

import InternetView from '../InternetView.vue'

vi.mock('@/lib/api', async (orig) => {
  const mod = await orig<typeof import('@/lib/api')>()
  return {
    ...mod,
    api: {
      ...mod.api,
      internet: vi.fn<typeof mod.api.internet>(),
      destinations: vi.fn<typeof mod.api.destinations>(),
      topology: vi.fn<typeof mod.api.topology>(),
    },
  }
})

const usage = (over: Partial<InternetResponse> = {}): InternetResponse => ({
  devices: [
    { node: 'mac:aa', label: 'Phone', on_map: true, rx: 3_000_000, tx: 100_000 },
    { node: 'ip:192.168.1.77', label: '192.168.1.77', on_map: false, rx: 1_000, tx: 0 },
  ],
  total: { node: '', rx: 3_001_000, tx: 100_000 },
  since: '',
  seconds: 86400,
  series: [
    { at: new Date(Date.now() - 7200_000).toISOString(), rx: 1_000_000, tx: 50_000 },
    { at: new Date(Date.now() - 3600_000).toISOString(), rx: 2_001_000, tx: 50_000 },
  ],
  step_s: 3600,
  now: { rx_bps: 12_000_000, tx_bps: 1_000_000 },
  countries: [
    { country: 'US', rx: 2_000_000, tx: 60_000 },
    { country: 'BR', rx: 1_001_000, tx: 40_000 },
  ],
  orgs: [
    { org: 'Netflix Streaming Services Inc.', countries: ['US'], rx: 2_000_000, tx: 60_000 },
    { org: 'Globo Comunicacao e Participacoes SA', countries: ['BR'], rx: 1_001_000, tx: 40_000 },
  ],
  home: 'BR',
  attribution: 'IP geolocation by DB-IP (db-ip.com)',
  listening: true,
  exporters: [
    {
      ip: '192.168.1.1',
      kind: 'netflow',
      flows: 10,
      last_at: new Date().toISOString(),
      version: 'v9',
    },
  ],
  ...over,
})

async function mountIt() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/internet', component: InternetView },
      { path: '/', name: 'map', component: InternetView },
    ],
  })
  await router.push('/internet')
  const w = mount(InternetView, {
    global: { plugins: [...plugins(), router] },
    attachTo: document.body,
  })
  await flushPromises()
  return { w, router }
}

describe('InternetView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    Element.prototype.scrollIntoView = vi.fn<() => void>() // jsdom has none
    vi.mocked(api.topology).mockResolvedValue({
      topology: {
        nodes: [{ id: 'mac:aa', label: 'Phone', kind: 'client', online: true }],
        edges: [],
      },
    } as unknown as Awaited<ReturnType<typeof api.topology>>)
  })

  it('shows the usage, the world, where it goes and who uses it most', async () => {
    vi.mocked(api.internet).mockResolvedValue(usage())
    const { w } = await mountIt()
    expect(api.internet).toHaveBeenCalledWith('today')
    expect(w.get('[data-test=internet-kpis]').text()).toContain('12.0 Mbps')
    expect(w.find('[data-test=traffic-chart]').exists()).toBe(true)
    const world = w.get('[data-test=world-map]')
    expect(world.find('[data-country=US].on').exists()).toBe(true)
    expect(world.find('[data-country=FR].on').exists()).toBe(false)
    expect(world.findAll('.arc')).toHaveLength(1) // home (Brazil) → the US
    expect(w.get('[data-test=internet-orgs]').text()).toContain('Netflix')
    expect(w.get('[data-test=internet-top]').text()).toContain('Phone')

    // Picking a country keeps only who runs places there.
    await world.get('[data-country=BR]').trigger('click')
    expect(w.get('[data-test=internet-orgs]').text()).not.toContain('Netflix')
    expect(w.get('[data-test=country-filter]').text()).toContain('Brazil')
    await w.get('[data-test=country-filter]').trigger('click')
    expect(w.get('[data-test=internet-orgs]').text()).toContain('Netflix')
    w.unmount()
  })

  it('opens a device of the ranking in the full list, with where it went', async () => {
    vi.mocked(api.internet).mockResolvedValue(usage())
    vi.mocked(api.destinations).mockResolvedValue({
      hours: 24,
      destinations: [
        {
          name: 'www.netflix.com',
          country: 'US',
          org: 'Netflix Streaming Services Inc.',
          rx: 2_900_000,
          tx: 1_000,
          ports: [{ proto: 6, port: 443, bytes: 1 }],
        },
      ],
    })
    const { w, router } = await mountIt()
    expect(w.find('[data-test=internet-device]').exists()).toBe(false) // the list starts folded
    await w.get('[data-test=internet-top] .link').trigger('click')
    await flushPromises()
    expect(api.destinations).toHaveBeenCalledWith('mac:aa', 24)
    expect(w.findAll('[data-test=internet-device]')).toHaveLength(2)
    expect(w.get('[data-test=internet-dests]').text()).toContain('www.netflix.com')

    await w.get('[data-test=internet-range-1h]').trigger('click')
    await flushPromises()
    expect(api.internet).toHaveBeenLastCalledWith('1h')
    expect(api.destinations).toHaveBeenLastCalledWith('mac:aa', 1)

    await w.get('[data-test=internet-dests] .link').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query.node).toBe('mac:aa')
    w.unmount()
  })

  it('says when places are unknown, and how to turn flows on', async () => {
    vi.mocked(api.internet).mockResolvedValue(usage({ attribution: undefined, countries: [] }))
    const { w } = await mountIt()
    expect(w.find('[data-test=no-geo]').exists()).toBe(true)
    w.unmount()
    vi.mocked(api.internet).mockResolvedValue(usage({ devices: [], listening: false }))
    const off = await mountIt()
    expect(off.w.find('[data-test=internet-off]').exists()).toBe(true)
    off.w.unmount()
  })
})
