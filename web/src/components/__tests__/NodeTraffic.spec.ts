import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import type { TopoNode } from '@/lib/types'

import NodeTraffic from '../map/NodeTraffic.vue'
import { plugins } from './helpers'

vi.mock('@/lib/api', async (orig) => {
  const mod = await orig<typeof import('@/lib/api')>()
  return {
    ...mod,
    api: {
      ...mod.api,
      destinations: vi.fn<typeof mod.api.destinations>(),
      flows: vi.fn<typeof mod.api.flows>(),
      internet: vi.fn<typeof mod.api.internet>(),
    },
  }
})

const node = (id: string, label: string, extra: Partial<TopoNode> = {}): TopoNode =>
  ({ id, label, kind: 'client', online: true, ...extra }) as TopoNode
const phone = node('mac:aa', 'Phone', { ip: '192.168.1.42' })
const nas = node('mac:bb', 'NAS', { ip: '192.168.1.10' })
const wan = node('wan:fw', 'Internet', { kind: 'wan' })
const mountIt = (n: TopoNode) =>
  mount(NodeTraffic, {
    props: { node: n, nodes: [phone, nas, wan] },
    global: { plugins: plugins(), stubs: { RouterLink: true } },
  })

describe('NodeTraffic', () => {
  beforeEach(() => vi.clearAllMocks())

  it('shows where a device goes by name and whom it talks to, and highlights them', async () => {
    vi.mocked(api.destinations).mockResolvedValue({
      hours: 24,
      destinations: [
        {
          name: 'youtube.com',
          rx: 5_000_000,
          tx: 20_000,
          ports: [{ proto: 6, port: 443, bytes: 1 }],
        },
      ],
    })
    vi.mocked(api.flows).mockResolvedValue({
      listening: true,
      exporters: [],
      conversations: [
        {
          a: '192.168.1.10',
          b: '192.168.1.42',
          a_node: 'mac:bb',
          b_node: 'mac:aa',
          bytes_ab: 900,
          bytes_ba: 100,
          packets: 3,
          ports: [{ proto: 6, port: 445, bytes: 1000 }],
        },
        // An internet peer is not "on your network".
        {
          a: '142.250.78.14',
          b: '192.168.1.42',
          b_node: 'mac:aa',
          bytes_ab: 9,
          bytes_ba: 1,
          packets: 1,
          ports: [],
        },
      ],
    })
    const w = mountIt(phone)
    await flushPromises()
    expect(api.destinations).toHaveBeenCalledWith('mac:aa', 24)
    expect(w.get('[data-test=node-destinations]').text()).toContain('youtube.com')
    const talks = w.get('[data-test=node-talks]')
    expect(talks.text()).toContain('NAS')
    expect(talks.text()).not.toContain('142.250.78.14')
    await w.get('[data-test=show-peers]').trigger('click')
    expect(w.emitted('peers')?.slice(-1)[0]).toEqual([['mac:aa', 'mac:bb']])
    await w.get('[data-test=show-peers]').trigger('click')
    expect(w.emitted('peers')?.slice(-1)[0]).toEqual([undefined])
  })

  it("shows who uses the internet now on the internet's node", async () => {
    vi.mocked(api.internet).mockResolvedValue({
      devices: [{ node: 'mac:aa', label: 'Phone', on_map: true, rx: 112_500_000, tx: 0 }],
      total: { node: '', rx: 112_500_000, tx: 0 },
      since: '',
      seconds: 900,
      series: [],
      step_s: 60,
      now: { rx_bps: 0, tx_bps: 0 },
      countries: [],
      orgs: [],
      home: '',
      listening: true,
      exporters: [],
    })
    const w = mountIt(wan)
    await flushPromises()
    expect(api.internet).toHaveBeenCalledWith('15m')
    expect(w.get('[data-test=internet-users]').text()).toContain('Phone')
    expect(w.get('[data-test=internet-users]').text()).toContain('1.0 Mbps')
    expect(api.destinations).not.toHaveBeenCalled()
  })
})
