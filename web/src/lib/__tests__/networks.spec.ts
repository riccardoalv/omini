import { describe, expect, it } from 'vitest'

import { groupNetworks, networkHighlight, networkSubnets, subnetOf } from '@/lib/networks'
import type { TopoEdge, TopoNode } from '@/lib/types'

const fw: TopoNode = {
  id: 'fw',
  kind: 'device',
  label: 'fw',
  online: true,
  device: {
    key: 'fw',
    name: 'fw',
    interfaces: [
      { name: 'igc0', wan: true, ips: ['203.0.113.7/24'] },
      { name: 'igc1', description: 'LAN', ips: ['192.168.1.1/24'] },
      { name: 'igc1.20', description: 'IOT', vlan: 20, parent: 'igc1', ips: ['192.168.20.1/24'] },
      { name: 'wg0', description: 'VPN', ips: ['10.10.0.1/24'] },
    ],
  },
}
const sw: TopoNode = {
  id: 'sw',
  kind: 'device',
  label: 'sw',
  online: true,
  ip: '192.168.1.2',
  device: {
    key: 'sw',
    name: 'sw',
    interfaces: [
      { name: 'Port 1', vlans: { untagged: 1, tagged: [20] } },
      { name: 'Port 2', vlans: { untagged: 20 } },
      { name: 'Port 3', vlans: { untagged: 1 } },
    ],
  },
}
const client = (id: string, ip?: string): TopoNode => ({
  id,
  kind: 'client',
  label: id,
  online: true,
  ip,
})
const nodes = [
  fw,
  sw,
  client('cam', '192.168.20.10'),
  client('pc', '192.168.1.50'),
  client('phone', '192.168.1.60'),
  client('laptop', '10.10.0.2'),
  client('nobody'),
]
const edges: TopoEdge[] = [
  {
    id: 'up',
    source: 'fw',
    target: 'sw',
    source_port: 'igc1',
    target_port: 'Port 1',
    kind: 'lldp',
  },
  { id: 'e1', source: 'sw', target: 'cam', source_port: 'Port 2', kind: 'fdb' },
  { id: 'e2', source: 'sw', target: 'pc', source_port: 'Port 3', kind: 'fdb' },
  { id: 'e3', source: 'fw', target: 'phone', source_port: 'igc1', kind: 'inferred' },
  { id: 'e4', source: 'fw', target: 'laptop', source_port: 'wg0', kind: 'inferred' },
]

describe('network groups', () => {
  it('reads private subnets from addresses with a prefix', () => {
    expect(subnetOf('192.168.1.77/24')).toMatchObject({ cidr: '192.168.1.0/24', bits: 24 })
    expect(subnetOf('10.1.2.3/16')?.cidr).toBe('10.1.0.0/16')
    expect(subnetOf('172.20.5.1/22')?.cidr).toBe('172.20.4.0/22')
    expect(subnetOf('8.8.8.8/24')).toBeUndefined() // public
    expect(subnetOf('192.168.1.1/32')).toBeUndefined() // a host
    expect(subnetOf('192.168.1.1')).toBeUndefined()
  })

  it('lists the networks of the interfaces, without the WAN', () => {
    expect(networkSubnets(nodes).map((s) => [s.cidr, s.vlan, s.label])).toEqual([
      ['10.10.0.0/24', undefined, 'VPN'],
      ['192.168.1.0/24', undefined, 'LAN'],
      ['192.168.20.0/24', 20, 'IOT'],
    ])
  })

  it('gives each VLAN and subnet the devices only in it', () => {
    const groups = groupNetworks(nodes, edges)
    expect(groups.map((g) => [g.key, g.name, [...g.members].sort()])).toEqual([
      ['vlan:20', 'VLAN 20 · IOT', ['cam']],
      ['subnet:10.10.0.0/24', 'VPN · 10.10.0.0/24', ['laptop']],
      // The switch carries VLAN 20 too (a trunk) and the firewall is in all of them.
      ['subnet:192.168.1.0/24', 'LAN · 192.168.1.0/24', ['pc', 'phone']],
    ])
  })

  it('brings in a node without addresses when all its children are in the area', () => {
    const net: TopoNode = { id: 'net', kind: 'device', label: 'Network', online: true }
    const hub: TopoNode = { id: 'hub', kind: 'unmanaged', label: 'hub', online: true }
    const groups = groupNetworks(
      [...nodes, net, hub, client('tv', '192.168.20.11')],
      [
        ...edges,
        { id: 'n1', source: 'fw', target: 'net', kind: 'inferred' },
        { id: 'n2', source: 'net', target: 'hub', kind: 'inferred' },
        { id: 'n3', source: 'hub', target: 'tv', kind: 'inferred' },
        { id: 'n4', source: 'net', target: 'laptop', kind: 'inferred' },
      ],
    )
    const of = (key: string) => [...groups.find((g) => g.key === key)!.members].sort()
    expect(of('vlan:20')).toEqual(['cam', 'hub', 'tv'])
    // Its children are in two areas: it stays between them.
    expect(groups.some((g) => g.members.has('net'))).toBe(false)
  })

  it('leaves out nodes already in an area the user drew', () => {
    const lan = groupNetworks(nodes, edges, new Set(['pc'])).find((g) =>
      g.key.includes('192.168.1.'),
    )
    expect([...lan!.members]).toEqual(['phone'])
  })

  it('makes none for a network with a single subnet', () => {
    const flat: TopoNode = {
      ...fw,
      device: { ...fw.device!, interfaces: [fw.device!.interfaces![1]!] },
    }
    expect(groupNetworks([flat, client('pc', '192.168.1.50')], [])).toEqual([])
  })
})

describe('networkHighlight', () => {
  it('lights a router in several networks alone, not what hangs below it', () => {
    // wan → fw (LAN + modem network) → sw (LAN) → pc; wan → modem (modem network).
    const fw: TopoNode = {
      id: 'fw',
      kind: 'device',
      label: 'fw',
      online: true,
      device: {
        key: 'fw',
        name: 'fw',
        interfaces: [
          { name: 'wanphys', description: 'WAN_PHYSICAL', ips: ['192.168.100.2/24'] },
          { name: 'lan', description: 'LAN', ips: ['192.168.1.1/24'] },
        ],
      },
    }
    const nodes = [
      fw,
      client('sw', '192.168.1.96'),
      client('pc', '192.168.1.50'),
      client('modem', '192.168.100.1'),
    ]
    const edges: TopoEdge[] = [
      { id: '1', source: 'fw', target: 'sw', kind: 'lldp' },
      { id: '2', source: 'sw', target: 'pc', kind: 'fdb' },
    ]
    const inclusive = groupNetworks(nodes, edges, new Set(), 'inclusive')
    const exclusive = groupNetworks(nodes, edges)
    const wan = networkHighlight('subnet:192.168.100.0/24', inclusive, exclusive, edges)!
    expect([...wan].sort()).toEqual(['fw', 'modem'])
    const lan = networkHighlight('subnet:192.168.1.0/24', inclusive, exclusive, edges)!
    expect([...lan].sort()).toEqual(['fw', 'pc', 'sw'])
    expect(networkHighlight(undefined, inclusive, exclusive, edges)).toBeUndefined()
  })
})
