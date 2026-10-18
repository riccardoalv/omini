import { describe, expect, it } from 'vitest'

import {
  groupNetworks,
  inNetwork,
  networkColor,
  networkAreaId,
  networkAreas,
  networkHighlight,
  networkSubnets,
  NETWORK_PALETTE,
  subnetOf,
  suggestGateway,
  suggestRange,
} from '@/lib/networks'
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

  it("uses a router's networks (VLAN and gateway) for membership by address", () => {
    const rt: TopoNode = {
      id: 'rt',
      kind: 'device',
      label: 'rt',
      online: true,
      device: {
        key: 'rt',
        name: 'rt',
        networks: [
          { vlan: 1, name: 'LAN', gateway: '192.168.1.1/24' },
          { vlan: 30, interface: 'opt3', gateway: '192.168.30.1/24' },
        ],
      },
    }
    expect(networkSubnets([rt]).map((s) => [s.cidr, s.vlan, s.label])).toEqual([
      ['192.168.1.0/24', 1, 'LAN'],
      ['192.168.30.0/24', 30, 'opt3'],
    ])
    const groups = groupNetworks(
      [rt, client('tv', '192.168.30.20'), client('pc', '192.168.1.20')],
      [],
      new Set(),
      'inclusive',
    )
    expect(groups.find((g) => g.key === 'subnet:192.168.30.0/24')?.members.has('tv')).toBe(true)
  })

  it("puts a Wi-Fi client in its SSID's VLAN", () => {
    const ap: TopoNode = {
      id: 'ap',
      kind: 'device',
      label: 'ap',
      online: true,
      device: {
        key: 'ap',
        name: 'ap',
        wifi_networks: [{ ssid: 'Home' }, { ssid: 'Guest', vlan: 20 }],
      },
    }
    const guest: TopoNode = { ...client('guest'), ssid: 'Guest' }
    const home: TopoNode = { ...client('home'), ssid: 'Home' }
    const all = [...nodes, ap, guest, home]
    const wifi: TopoEdge[] = [
      ...edges,
      { id: 'w0', source: 'sw', target: 'ap', source_port: 'Port 1', kind: 'fdb' },
      { id: 'w1', source: 'ap', target: 'guest', kind: 'wifi' },
      { id: 'w2', source: 'ap', target: 'home', kind: 'wifi' },
    ]
    for (const mode of ['inclusive', 'exclusive'] as const) {
      const groups = groupNetworks(all, wifi, new Set(), mode)
      const in20 = groups.find((g) => g.key === 'vlan:20')!.members
      expect(in20.has('guest')).toBe(true)
      expect(in20.has('home')).toBe(false)
    }
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

describe('networkAreas', () => {
  it('draws every network as areas, one per branch, devices in several networks outside them all', () => {
    const areas = networkAreas(nodes, edges, [], 'RIGHT')
    // The LAN is under the switch (pc) and right under the firewall (phone): two areas.
    expect(areas.map((a) => [a.name, a.members, a.auto, a.id < 0])).toEqual([
      ['LAN · 192.168.1.0/24', ['pc'], true, true],
      ['LAN · 192.168.1.0/24', ['phone'], true, true],
      ['VLAN 20 · IOT', ['cam'], true, true],
      ['VPN · 10.10.0.0/24', ['laptop'], true, true],
    ])
    // The ids are stable: the network and the branch it enters at.
    expect(networkAreaId('vlan:20@sw')).toBe(areas[2]!.id)
  })

  it('leaves out a device with something of another network below it', () => {
    const hub: TopoNode = { id: 'hub', kind: 'unmanaged', label: 'hub', online: true }
    const areas = networkAreas(
      [...nodes, hub, client('tv', '192.168.1.80')],
      [
        ...edges,
        { id: 'h', source: 'sw', target: 'hub', source_port: 'Port 3', kind: 'fdb' },
        { id: 'h1', source: 'hub', target: 'tv', kind: 'inferred' },
      ],
      [],
      'RIGHT',
    )
    // Only the TV below it: the hub is in the LAN with it.
    expect(areas.find((a) => a.members.includes('tv'))!.members).toContain('hub')
    const mixed = networkAreas(
      [...nodes, hub, client('tv', '192.168.1.80'), client('cam2', '192.168.20.11')],
      [
        ...edges,
        { id: 'h', source: 'sw', target: 'hub', source_port: 'Port 3', kind: 'fdb' },
        { id: 'h1', source: 'hub', target: 'tv', kind: 'inferred' },
        { id: 'h2', source: 'hub', target: 'cam2', kind: 'inferred' },
      ],
      [],
      'RIGHT',
    )
    expect(mixed.flatMap((a) => a.members)).not.toContain('hub')
    expect(mixed.find((a) => a.members.includes('cam2'))!.name).toBe('VLAN 20 · IOT')
  })

  it('leaves out clients it is told to (a bubble shared by networks)', () => {
    const areas = networkAreas(nodes, edges, [], 'RIGHT', new Map(), new Set(['cam']))
    expect(areas.flatMap((a) => a.members)).not.toContain('cam')
  })

  it('places a client by its own address, not by every VLAN of the trunk it hangs from', () => {
    const areas = networkAreas(
      [...nodes, client('tv', '192.168.1.70')],
      [...edges, { id: 't', source: 'sw', target: 'tv', source_port: 'Port 1', kind: 'fdb' }],
      [],
      'RIGHT',
    )
    expect(areas.find((a) => a.members.includes('tv'))!.name).toMatch(/^LAN/)
  })

  it('leaves devices in an area the user drew to that area', () => {
    const mine = {
      id: 1,
      name: 'Desk',
      color: 'blue',
      direction: 'RIGHT' as const,
      x: 0,
      y: 0,
      width: 0,
      height: 0,
      members: ['pc'],
    }
    const lan = networkAreas(nodes, edges, [mine], 'RIGHT').filter((a) => a.name.startsWith('LAN'))
    expect(lan.flatMap((a) => a.members)).toEqual(['phone'])
  })

  it('colors a VLAN with its catalog color, else from the palette', () => {
    expect(networkColor({ vlan: 20 }, 0, new Map([[20, '#123456']]))).toBe('#123456')
    expect(networkColor({}, 1)).toBe(NETWORK_PALETTE[1])
    expect(networkColor({}, NETWORK_PALETTE.length)).toBe(NETWORK_PALETTE[0])
    const colored = networkAreas(nodes, edges, [], 'RIGHT', new Map([[20, '#123456']]))
    expect(colored.find((a) => a.name.startsWith('VLAN 20'))!.color).toBe('#123456')
  })
})

describe('network suggestions', () => {
  it('suggests 192.168.<id>.1/24, else a free 10.x network', () => {
    expect(suggestGateway(30, [])).toBe('192.168.30.1/24')
    expect(suggestGateway(30, ['192.168.30.0/24'])).toBe('10.0.30.1/24')
    expect(suggestGateway(300, [])).toBe('10.1.44.1/24')
  })
  it('suggests .100 to .250 of a /24, the upper half of a smaller one', () => {
    expect(suggestRange('192.168.30.1/24')).toEqual({
      from: '192.168.30.100',
      to: '192.168.30.250',
    })
    expect(suggestRange('10.0.0.1/28')).toEqual({ from: '10.0.0.8', to: '10.0.0.14' })
    expect(suggestRange('8.8.8.1/24')).toBeUndefined()
  })
  it('knows the addresses of a network', () => {
    expect(inNetwork('192.168.30.5', '192.168.30.1/24')).toBe(true)
    expect(inNetwork('192.168.30.255', '192.168.30.1/24')).toBe(false)
    expect(inNetwork('192.168.31.5', '192.168.30.1/24')).toBe(false)
  })
})
