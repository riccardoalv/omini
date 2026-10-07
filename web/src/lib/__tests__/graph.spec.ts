import { describe, expect, it } from 'vitest'

import {
  childrenOf,
  clientCount,
  collapseClients,
  edgeLook,
  groupId,
  isWifiNetwork,
  linkOnPort,
  withoutHidden,
  withoutOffline,
  withWifiNetworks,
} from '../graph'
import type { TopoEdge, TopoNode } from '../types'

const device = (id: string, role = 'switch'): TopoNode => ({
  id,
  kind: 'device',
  label: id,
  role,
  online: true,
})
const client = (id: string, extra: Partial<TopoNode> = {}): TopoNode => ({
  id,
  kind: 'client',
  label: id,
  role: 'client',
  online: true,
  ...extra,
})
const edge = (source: string, target: string, extra: Partial<TopoEdge> = {}): TopoEdge => ({
  id: `e:${source}|${target}`,
  source,
  target,
  kind: 'fdb',
  ...extra,
})

function network(clientCount: number) {
  const nodes = [device('ap', 'ap')]
  const edges: TopoEdge[] = []
  for (let i = 0; i < clientCount; i++) {
    nodes.push(client(`c${i}`))
    edges.push(edge('ap', `c${i}`, { kind: 'wifi' }))
  }
  return { nodes, edges }
}

describe('collapseClients', () => {
  it('collapsing a node also hides the children of its children', () => {
    // Proxmox → 2 VMs → 10 apps each (the VMs' apps are grouped automatically).
    const nodes = [device('pve'), client('vm1'), client('vm2')]
    const edges = [edge('pve', 'vm1'), edge('pve', 'vm2')]
    for (const vm of ['vm1', 'vm2']) {
      for (let i = 0; i < 10; i++) {
        nodes.push({ ...client(`${vm}-app${i}`), kind: 'app' })
        edges.push(edge(vm, `${vm}-app${i}`))
      }
    }
    const open = collapseClients(nodes, edges, 8, new Set())
    expect(open.groups.map((g) => g.parentId)).toEqual(['vm1', 'vm2'])

    const view = collapseClients(nodes, edges, 8, new Set(), new Set(['pve']))
    expect(view.nodes.map((n) => n.id)).toEqual(['pve'])
    expect(view.groups.map((g) => g.parentId)).toEqual(['pve']) // only the Proxmox bubble
    expect(view.edges.map((e) => e.target)).toEqual([groupId('pve')])
  })

  it('keeps every client visible up to the threshold', () => {
    const { nodes, edges } = network(8)
    const view = collapseClients(nodes, edges, 8, new Set())
    expect(view.groups).toHaveLength(0)
    expect(view.nodes).toHaveLength(9)
  })

  it('collapses clients above the threshold into one group', () => {
    const { nodes, edges } = network(10)
    nodes[3] = { ...nodes[3]!, online: false }
    const view = collapseClients(nodes, edges, 8, new Set())

    expect(view.groups).toHaveLength(1)
    const group = view.groups[0]!
    expect(group.id).toBe(groupId('ap'))
    expect(group.clients).toHaveLength(10)
    expect(group.online).toBe(9)
    expect(view.nodes.map((n) => n.id)).toEqual(['ap'])
    expect(view.edges).toEqual([
      { id: `e:ap|${groupId('ap')}`, source: 'ap', target: groupId('ap'), kind: 'inferred' },
    ])
  })

  it('does not collapse a parent the user expanded', () => {
    const { nodes, edges } = network(10)
    const view = collapseClients(nodes, edges, 8, new Set(['ap']))
    expect(view.groups).toHaveLength(0)
    expect(view.nodes).toHaveLength(11)
  })

  it('keeps pinned clients visible', () => {
    const { nodes, edges } = network(10)
    nodes[1] = { ...nodes[1]!, pinned: true }
    const view = collapseClients(nodes, edges, 8, new Set())
    expect(view.nodes.map((n) => n.id)).toEqual(['ap', 'c0'])
    expect(view.groups[0]!.clients).toHaveLength(9)
    expect(view.edges.some((e) => e.target === 'c0')).toBe(true)
  })

  it('collapses every client of a node the user collapsed, except pinned ones', () => {
    const { nodes, edges } = network(3)
    nodes[1] = { ...nodes[1]!, type: 'server', product: 'jellyfin' }
    nodes[2] = { ...nodes[2]!, pinned: true }
    const view = collapseClients(nodes, edges, 8, new Set(), new Set(['ap']))
    expect(view.nodes.map((n) => n.id)).toEqual(['ap', 'c1'])
    expect(view.groups[0]!.clients.map((c) => c.id)).toEqual(['c0', 'c2'])
  })

  it('keeps infrastructure and homelab software visible', () => {
    const { nodes, edges } = network(10)
    nodes[1] = { ...nodes[1]!, type: 'nas', product: 'truenas' }
    nodes[2] = { ...nodes[2]!, type: 'switch' }
    nodes[3] = { ...nodes[3]!, type: 'virtual_machine' }
    nodes[4] = { ...nodes[4]!, type: 'phone' }
    const view = collapseClients(nodes, edges, 8, new Set())
    expect(view.nodes.map((n) => n.id)).toEqual(['ap', 'c0', 'c1', 'c2'])
    expect(view.groups[0]!.clients).toHaveLength(7)
  })

  it('never collapses devices, only clients', () => {
    const nodes = [device('sw'), ...Array.from({ length: 10 }, (_, i) => device(`d${i}`))]
    const edges = nodes.slice(1).map((n) => edge('sw', n.id))
    expect(collapseClients(nodes, edges, 8, new Set()).groups).toHaveLength(0)
  })
})

describe('edgeLook', () => {
  it('scales width with speed', () => {
    expect(edgeLook(edge('a', 'b', { speed_mbps: 100 })).width).toBe(1.5)
    expect(edgeLook(edge('a', 'b', { speed_mbps: 1000 })).width).toBe(2)
    expect(edgeLook(edge('a', 'b', { speed_mbps: 2500 })).width).toBe(3)
    expect(edgeLook(edge('a', 'b', { speed_mbps: 10000 })).width).toBe(4)
  })

  it('flags slow links between infrastructure only', () => {
    const slow = edge('sw', 'ap', { speed_mbps: 100 })
    expect(edgeLook(slow, device('sw'), device('ap', 'ap')).slow).toBe(true)
    expect(edgeLook(slow, device('sw'), client('ap')).slow).toBe(false)
    expect(edgeLook(edge('sw', 'ap', { speed_mbps: 1000 }), device('sw'), device('ap')).slow).toBe(
      false,
    )
  })

  it('dashes inferred links and dots Wi-Fi', () => {
    expect(edgeLook(edge('a', 'b', { kind: 'inferred' })).dashed).toBe(true)
    expect(edgeLook(edge('a', 'b', { kind: 'wifi' })).dotted).toBe(true)
    expect(edgeLook(edge('a', 'b', { kind: 'lldp' })).dashed).toBe(false)
  })
})

describe('graph helpers', () => {
  const nodes = [device('sw'), device('ap', 'ap'), client('tv')]
  const edges = [
    edge('sw', 'ap', { source_port: '3', target_port: 'eth0', kind: 'lldp' }),
    edge('sw', 'tv', { source_port: '6' }),
  ]

  it('counts children that can be grouped (clients and apps)', () => {
    const app: TopoNode = { id: 'jf', kind: 'app', label: 'Jellyfin', online: true }
    expect(clientCount('sw', [...nodes, app], [...edges, edge('sw', 'jf')])).toBe(2)
    expect(clientCount('sw', nodes, edges)).toBe(1)
  })

  it('finds children', () => {
    expect(childrenOf('sw', nodes, edges).map((n) => n.id)).toEqual(['ap', 'tv'])
  })

  it('finds what is connected to a port, from either end', () => {
    expect(linkOnPort('sw', '3', nodes, edges)?.id).toBe('ap')
    expect(linkOnPort('ap', 'eth0', nodes, edges)?.id).toBe('sw')
    expect(linkOnPort('sw', '8', nodes, edges)).toBeUndefined()
  })
})

describe('withoutOffline', () => {
  it('removes offline nodes and their links', () => {
    const nodes = [device('sw'), client('tv'), client('phone', { online: false })]
    const edges = [edge('sw', 'tv'), edge('sw', 'phone')]
    const view = withoutOffline(nodes, edges)
    expect(view.nodes.map((n) => n.id)).toEqual(['sw', 'tv'])
    expect(view.edges.map((e) => e.target)).toEqual(['tv'])
    expect(view.hidden).toBe(1)
  })
})

describe('withoutHidden', () => {
  it('removes hidden devices and everything below them', () => {
    const nodes = [device('gw'), client('pve', { hidden: true }), client('vm'), client('tv')]
    const edges = [edge('gw', 'pve'), edge('pve', 'vm'), edge('gw', 'tv')]
    const view = withoutHidden(nodes, edges)
    expect(view.nodes.map((n) => n.id)).toEqual(['gw', 'tv'])
    expect(view.edges.map((e) => e.target)).toEqual(['tv'])
    expect(view.hidden).toBe(1)
  })
})

describe('withWifiNetworks', () => {
  it('puts each Wi-Fi network between the access point and its clients', () => {
    const ap: TopoNode = { id: 'dev:ap', kind: 'device', label: 'Bedroom', online: true }
    const phone: TopoNode = {
      id: 'mac:1',
      kind: 'client',
      label: 'phone',
      online: true,
      band: '5ghz',
    }
    const bulb: TopoNode = {
      id: 'mac:2',
      kind: 'client',
      label: 'bulb',
      online: true,
      band: '2.4ghz',
    }
    const tv: TopoNode = { id: 'mac:3', kind: 'client', label: 'tv', online: true, band: '5ghz' }
    const pc: TopoNode = { id: 'mac:4', kind: 'client', label: 'pc', online: true }
    const wifi = (target: string, net: string): TopoEdge => ({
      id: `e:${target}`,
      source: ap.id,
      target,
      kind: 'wifi',
      source_port: net,
    })
    const { nodes, edges } = withWifiNetworks(
      [ap, phone, bulb, tv, pc],
      [
        wifi(phone.id, 'Home · 5 GHz'),
        wifi(bulb.id, 'IOT · 2.4 GHz'),
        wifi(tv.id, 'Home · 5 GHz'),
        { id: 'e:pc', source: ap.id, target: pc.id, kind: 'fdb', source_port: 'LAN' },
      ],
    )
    const nets = nodes.filter((n) => n.kind === 'ssid')
    expect(nets.map((n) => [n.label, n.band])).toEqual([
      ['Home · 5 GHz', '5ghz'],
      ['IOT · 2.4 GHz', '2.4ghz'],
    ])
    expect(nets.every((n) => isWifiNetwork(n.id))).toBe(true)
    const parentOf = (id: string) => edges.find((e) => e.target === id)!.source
    expect(parentOf(phone.id)).toBe('wifi:dev:ap:Home · 5 GHz')
    expect(parentOf(tv.id)).toBe('wifi:dev:ap:Home · 5 GHz')
    expect(parentOf(bulb.id)).toBe('wifi:dev:ap:IOT · 2.4 GHz')
    expect(parentOf('wifi:dev:ap:Home · 5 GHz')).toBe(ap.id)
    expect(parentOf(pc.id)).toBe(ap.id) // wired: unchanged
  })
})
