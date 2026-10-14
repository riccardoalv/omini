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
  seconds: 900,
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
  const w = mount(InternetView, { global: { plugins: [...plugins(), router] } })
  await flushPromises()
  return { w, router }
}

describe('InternetView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(api.topology).mockResolvedValue({
      topology: {
        nodes: [{ id: 'mac:aa', label: 'Phone', kind: 'client', online: true }],
        edges: [],
      },
    } as unknown as Awaited<ReturnType<typeof api.topology>>)
  })

  it('ranks who used the internet, and shows where a device went', async () => {
    vi.mocked(api.internet).mockResolvedValue(usage())
    vi.mocked(api.destinations).mockResolvedValue({
      hours: 1,
      destinations: [
        {
          name: 'netflix.com',
          rx: 2_900_000,
          tx: 1_000,
          ports: [{ proto: 6, port: 443, bytes: 1 }],
        },
      ],
    })
    const { w, router } = await mountIt()
    expect(api.internet).toHaveBeenCalledWith('15m')
    const rows = w.findAll('[data-test=internet-device]')
    expect(rows.map((r) => r.text().split('↓')[0]!.trim())).toEqual(['Phone', '192.168.1.77'])
    expect(w.get('[data-test=internet-total]').text()).toContain('average')

    await rows[0]!.get('button').trigger('click')
    await flushPromises()
    expect(api.destinations).toHaveBeenCalledWith('mac:aa', 1)
    expect(w.get('[data-test=internet-dests]').text()).toContain('netflix.com')

    await w.get('[data-test=internet-range-7d]').trigger('click')
    await flushPromises()
    expect(api.internet).toHaveBeenLastCalledWith('7d')
    expect(api.destinations).toHaveBeenLastCalledWith('mac:aa', 24)

    await w.get('[data-test=internet-dests] .link').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query.node).toBe('mac:aa')
  })

  it('says how to turn flows on when they are off', async () => {
    vi.mocked(api.internet).mockResolvedValue(usage({ devices: [], listening: false }))
    const { w } = await mountIt()
    expect(w.find('[data-test=internet-off]').exists()).toBe(true)
  })
})
