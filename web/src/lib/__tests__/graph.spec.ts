import { describe, expect, it } from 'vitest'

import { childrenOf, collapseClients, edgeLook, groupId, linkOnPort } from '../graph'
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

  it('finds children', () => {
    expect(childrenOf('sw', nodes, edges).map((n) => n.id)).toEqual(['ap', 'tv'])
  })

  it('finds what is connected to a port, from either end', () => {
    expect(linkOnPort('sw', '3', nodes, edges)?.id).toBe('ap')
    expect(linkOnPort('ap', 'eth0', nodes, edges)?.id).toBe('sw')
    expect(linkOnPort('sw', '8', nodes, edges)).toBeUndefined()
  })
})
