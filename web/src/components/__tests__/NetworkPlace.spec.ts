import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { prefs } from '@/lib/prefs'
import type { TopoNode } from '@/lib/types'

import NetworkPlace from '../map/NetworkPlace.vue'
import { plugins } from './helpers'

vi.mock('@/lib/api', async (orig) => {
  const mod = await orig<typeof import('@/lib/api')>()
  return {
    ...mod,
    api: {
      ...mod.api,
      reserve: vi.fn<typeof mod.api.reserve>(),
      unreserve: vi.fn<typeof mod.api.unreserve>(),
      fixCandidates: vi.fn<typeof mod.api.fixCandidates>(),
      dnsName: vi.fn<typeof mod.api.dnsName>().mockResolvedValue({ missing: 'no_fixed_ip' }),
      vlans: vi.fn<typeof mod.api.vlans>().mockResolvedValue([]),
    },
  }
})

const fw = (reserved = false): TopoNode => ({
  id: 'dev:fw',
  kind: 'device',
  label: 'fw',
  online: true,
  changes: ['network.set', 'reservation.set', 'reservation.delete'],
  device: {
    key: 'fw',
    name: 'fw',
    networks: [{ vlan: 1, gateway: '192.168.1.1/24' }],
    dhcp_reservations: reserved ? [{ mac: 'AA:BB:CC:00:00:01', ip: '192.168.1.50' }] : [],
  },
})
const sw: TopoNode = {
  id: 'dev:sw',
  kind: 'device',
  label: 'sw',
  online: true,
  changes: ['port.vlans'],
  device: { key: 'sw', name: 'sw', interfaces: [{ name: 'Port 2', vlans: { untagged: 1 } }] },
}
const tv: TopoNode = {
  id: 'mac:tv',
  kind: 'client',
  label: 'tv',
  online: true,
  ip: '192.168.1.50',
  mac: 'aa:bb:cc:00:00:01',
  parent_id: 'dev:sw',
  port: 'Port 2',
}
const mountPlace = (nodes: TopoNode[]) =>
  mount(NetworkPlace, {
    props: { node: tv, nodes, nameOf: (id: string) => id, allowDestructive: false },
    global: { plugins: plugins() },
  })

describe('NetworkPlace', () => {
  beforeEach(() => {
    prefs.openSections = ['place']
    vi.mocked(api.reserve).mockResolvedValue({} as never)
    vi.mocked(api.unreserve).mockResolvedValue({} as never)
    vi.mocked(api.fixCandidates).mockResolvedValue([])
  })

  it('fixes the address the router serves', async () => {
    const w = mountPlace([fw(), sw, tv])
    await w.get('[data-test=fix-ip]').trigger('click')
    await flushPromises()
    expect(api.reserve).toHaveBeenCalledWith('mac:tv', '192.168.1.50')
    expect(w.emitted('applied')).toHaveLength(1)
  })

  it('proposes an address outside the DHCP range when the router needs one', async () => {
    vi.mocked(api.fixCandidates).mockResolvedValue([
      {
        node_id: 'mac:tv',
        label: 'tv',
        mac: 'aa:bb:cc:00:00:01',
        ip: '192.168.1.50',
        router: 'dev:fw',
        infra: false,
        proposed: '192.168.1.2',
      },
    ])
    const w = mountPlace([fw(), sw, tv])
    await flushPromises()
    expect(w.get('[data-test=fixed-ip] input').element).toHaveProperty('value', '192.168.1.2')
    expect(w.text()).toContain('inside the DHCP range')
    await w.get('[data-test=fix-ip]').trigger('click')
    await flushPromises()
    expect(api.reserve).toHaveBeenCalledWith('mac:tv', '192.168.1.2')
  })

  it('does not fix on a router that does not serve DHCP, and says where to', async () => {
    vi.mocked(api.fixCandidates).mockResolvedValue([
      {
        node_id: 'mac:tv',
        label: 'tv',
        mac: 'aa:bb:cc:00:00:01',
        ip: '192.168.1.50',
        router: '',
        infra: false,
        server: 'dev:pihole',
      },
    ])
    const off = fw()
    off.device!.networks = [{ vlan: 1, gateway: '192.168.1.1/24', dhcp: { enabled: false } }]
    const w = mountPlace([off, tv])
    await flushPromises()
    expect(w.find('[data-test=fix-ip]').exists()).toBe(false)
    expect(w.get('[data-test=fix-elsewhere]').text()).toContain('Gets its address from dev:pihole')
  })

  it('shows a fixed address and frees it', async () => {
    const w = mountPlace([fw(true), sw, tv])
    expect(w.get('[data-test=fixed-ip]').text()).toContain('192.168.1.50')
    await w.get('[data-test=free-ip]').trigger('click')
    await flushPromises()
    expect(api.unreserve).toHaveBeenCalledWith('mac:tv')
  })

  it("moves the device's switch port to another VLAN", async () => {
    const w = mountPlace([fw(), sw, tv])
    expect(w.get('[data-test=port-vlan]').text()).toContain('Port 2')
    await w.get('[data-test=move-vlan]').trigger('click')
    expect(w.find('[data-test=vlan-editor]').exists()).toBe(true)
  })

  it('moves a VM by its own virtual NIC', async () => {
    const vm: TopoNode = {
      ...tv,
      id: 'dev:vm',
      kind: 'device',
      parent_id: 'dev:pve',
      changes: ['port.vlans'],
      device: { key: 'vm', name: 'vm', interfaces: [{ name: 'net0', vlans: { untagged: 1 } }] },
    }
    const w = mount(NetworkPlace, {
      props: { node: vm, nodes: [vm], nameOf: (id: string) => id, allowDestructive: false },
      global: { plugins: plugins() },
    })
    expect(w.get('[data-test=port-vlan]').text()).toContain('net0')
  })

  it('is not there when nothing can be changed', () => {
    const w = mountPlace([tv])
    expect(w.find('[data-test=network-place]').exists()).toBe(false)
  })
})
