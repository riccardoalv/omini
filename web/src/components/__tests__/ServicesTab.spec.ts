import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import type { ServicesView as View } from '@/lib/types'

import ServicesTab from '../map/ServicesTab.vue'
import { plugins } from './helpers'

vi.mock('@/lib/api', async (orig) => {
  const mod = await orig<typeof import('@/lib/api')>()
  return {
    ...mod,
    api: {
      ...mod.api,
      services: vi.fn<typeof mod.api.services>(),
      topology: vi.fn<typeof mod.api.topology>(),
      fixCandidates: vi.fn<typeof mod.api.fixCandidates>(),
      fixMany: vi.fn<typeof mod.api.fixMany>(),
    },
  }
})

const view: View = {
  dhcp: {
    checks: [
      {
        interface: 'eth0',
        offers: [
          {
            server_ip: '192.168.1.1',
            known: true,
            node_id: 'dev:fw',
            dns: ['192.168.1.1'],
            rtt_ms: 4,
          },
          { server_ip: '192.168.1.250', server_mac: '00:11:22:33:44:55', known: false },
        ],
      },
      { interface: 'eth1', offers: [] },
    ],
    pools: [{ node_id: 'dev:fw', network: '192.168.1.0/24', used: 190, total: 200 }],
    leases: [
      { node_id: 'dev:fw', client: 'mac:tv', ip: '192.168.1.20', mac: '00:11:22:00:00:20' },
      { node_id: 'dev:fw', ip: '192.168.1.21', mac: '00:11:22:00:00:21', hostname: 'phone' },
    ],
  },
  dns: {
    servers: [
      {
        server: '192.168.1.1',
        node_id: 'dev:fw',
        external: { name: 'example.com', ok: true, rtt_ms: 8, answers: ['93.184.215.14'] },
      },
      { server: '192.168.1.53', external: { name: 'example.com', ok: false, error: 'timeout' } },
    ],
  },
}

describe('ServicesTab', () => {
  beforeEach(() => {
    vi.mocked(api.services).mockResolvedValue(view)
    vi.mocked(api.fixCandidates).mockResolvedValue([])
    vi.mocked(api.topology).mockResolvedValue({
      topology: {
        nodes: [
          { id: 'dev:fw', kind: 'device', label: 'OPNsense', online: true },
          { id: 'mac:tv', kind: 'client', label: 'Living room TV', online: true },
        ],
        edges: [],
      },
      statuses: [],
      generated_at: '',
      layout: {},
    })
  })

  it('shows each DHCP check, the servers, pools and leases', async () => {
    const w = mount(ServicesTab, { global: { plugins: plugins() } })
    await flushPromises()
    const checks = w.findAll('[data-test=dhcp-checks] li')
    expect(checks[0]!.text()).toContain('An unknown server answered too: 192.168.1.250')
    expect(checks[1]!.text()).toContain('No DHCP server answered')
    expect(checks.every((c) => c.classes('bad'))).toBe(true)
    const servers = w.get('[data-test=dhcp-servers]').text()
    expect(servers).toContain('OPNsense')
    expect(servers).toContain('Unknown')
    expect(w.get('[data-test=dhcp-pools]').text()).toContain('190 / 200')
    expect(w.get('[data-test=dhcp-leases]').text()).toContain('Living room TV')
    // A device's name opens it on the map.
    await w.get('[data-test=dhcp-leases] .link').trigger('click')
    expect(w.emitted('select')).toEqual([['mac:tv']])
    await w.get('input[type=search]').setValue('phone')
    expect(w.findAll('[data-test=dhcp-leases] tbody tr')).toHaveLength(1)
    w.unmount()
  })

  it('shows each DNS server and whether it answers', async () => {
    const w = mount(ServicesTab, { global: { plugins: plugins() } })
    await flushPromises()
    await w.get('[data-test=tab-dns]').trigger('click')
    const rows = w.findAll('[data-test=dns-servers] tbody tr')
    expect(rows[0]!.text()).toContain('OK')
    expect(rows[0]!.text()).toContain('8 ms')
    expect(rows[1]!.text()).toContain('Not answering')
    expect(rows[1]!.text()).toContain('no answer')
    w.unmount()
  })

  it('fixes the infrastructure addresses many at once', async () => {
    vi.mocked(api.fixCandidates).mockResolvedValue([
      {
        node_id: 'mac:sw',
        label: 'sw-desk',
        type: 'switch',
        mac: 'aa:00:00:00:00:01',
        ip: '192.168.1.120',
        router: 'dev:fw',
        infra: true,
        proposed: '192.168.1.2',
      },
      {
        node_id: 'mac:nas',
        label: 'nas',
        type: 'nas',
        mac: 'aa:00:00:00:00:02',
        ip: '192.168.1.30',
        router: 'dev:fw',
        infra: true,
      },
      {
        node_id: 'mac:tv',
        label: 'tv',
        type: 'tv',
        mac: 'aa:00:00:00:00:03',
        ip: '192.168.1.40',
        router: 'dev:fw',
        infra: false,
      },
    ])
    vi.mocked(api.fixMany).mockResolvedValue([
      { node_id: 'mac:sw', change: {} as never },
      { node_id: 'mac:nas', error: 'the router refused' },
    ])
    const w = mount(ServicesTab, { global: { plugins: plugins() }, attachTo: document.body })
    await flushPromises()
    expect(w.get('[data-test=fix-ip]').text()).toContain(
      '2 infrastructure devices have no fixed IP',
    )
    await w.get('[data-test=fix-open]').trigger('click')
    const rows = () => document.querySelectorAll('[data-test=fix-list] li')
    expect(rows()).toHaveLength(2)
    expect(rows()[0]!.textContent).toContain('192.168.1.2')
    ;(document.querySelector('[data-test=fix-others]') as HTMLElement).click()
    await flushPromises()
    expect(rows()).toHaveLength(3)
    ;(document.querySelector('[data-test=fix-apply]') as HTMLElement).click()
    await flushPromises()
    expect(api.fixMany).toHaveBeenCalledWith([
      { node_id: 'mac:sw', ip: '192.168.1.2' },
      { node_id: 'mac:nas', ip: '192.168.1.30' },
    ])
    expect(document.body.textContent).toContain('the router refused')
    w.unmount()
  })
})
