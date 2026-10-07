import { describe, expect, it } from 'vitest'

import type { TopoEdge, TopoNode } from '../types'
import { deviceFlows, formatRate, linkLabels, nodeFlow, portName } from '../traffic'

const fw: TopoNode = {
  id: 'dev:fw',
  kind: 'device',
  label: 'OPNsense',
  online: true,
  device: {
    key: 'fw',
    name: 'fw',
    interfaces: [
      { name: 'pppoe1', wan: true },
      { name: 're0', speed_mbps: 2500 },
      { name: 'bridge0', speed_mbps: 10000 },
      { name: 'igb3', speed_mbps: 1000 },
    ],
  },
  traffic: {
    pppoe1: { rx_bps: 120e6, tx_bps: 8e6 },
    re0: { rx_bps: 125e6, tx_bps: 9e6 },
    bridge0: { rx_bps: 10e6, tx_bps: 118e6 },
  },
}
const wan: TopoNode = {
  id: 'wan:dev:fw:pppoe1',
  kind: 'wan',
  label: 'WAN',
  online: true,
  wan: { interface: 'pppoe1', port: 're0', speed_mbps: 2500 },
}
const client = (id: string): TopoNode => ({ id, kind: 'client', label: id, online: true })
const edge = (source: string, target: string, extra: Partial<TopoEdge> = {}): TopoEdge => ({
  id: `${source}>${target}`,
  source,
  target,
  kind: 'inferred',
  ...extra,
})

describe('traffic', () => {
  it('formats rates', () => {
    expect(formatRate(950)).toBe('950 bps')
    expect(formatRate(45_000)).toBe('45 kbps')
    expect(formatRate(45_210_000)).toBe('45.2 Mbps')
    expect(formatRate(450_000_000)).toBe('450 Mbps')
    expect(formatRate(1_200_000_000)).toBe('1.2 Gbps')
  })

  it("shows a firewall's internet traffic and a WAN's own", () => {
    const byId = new Map([fw, wan].map((n) => [n.id, n]))
    expect(nodeFlow(fw, byId)).toEqual({ down: 120e6, up: 8e6 })
    expect(nodeFlow(wan, byId)).toEqual({ down: 120e6, up: 8e6 })
    expect(nodeFlow(client('x'), byId)).toBeUndefined()
  })

  it('gives one pill to a shared port and its own pill to a direct link', () => {
    const nodes = [fw, wan, client('a'), client('b'), client('modem')]
    const edges = [
      edge(wan.id, fw.id, { target_port: 're0', speed_mbps: 2500 }),
      edge(fw.id, 'a', { source_port: 'bridge0' }),
      edge(fw.id, 'b', { source_port: 'bridge0' }),
      edge(fw.id, 'modem', { source_port: 'igb3', speed_mbps: 1000 }),
    ]
    const labels = linkLabels(edges, nodes)

    // WAN → firewall: traffic seen at the firewall's re0, towards the firewall.
    expect(labels.get(`${wan.id}>${fw.id}`)).toEqual({
      speed: 2500,
      flow: { down: 125e6, up: 9e6 },
      at: 'target',
    })
    // The LAN bridge: one pill at the firewall with the port's 10G and traffic.
    expect(labels.get(`${fw.id}>a`)).toEqual({
      speed: 10000,
      flow: { down: 118e6, up: 10e6 },
      at: 'source',
      hidden: false,
    })
    expect(labels.get(`${fw.id}>b`)!.hidden).toBe(true)
    // A port with a single device keeps the link's own speed, no traffic known.
    expect(labels.get(`${fw.id}>modem`)).toEqual({ speed: 1000, flow: undefined, at: 'target' })
  })

  it('moves a pill away from a node that shows a traffic badge', () => {
    const edges = [edge(wan.id, fw.id, { target_port: 're0', speed_mbps: 2500 })]
    const labels = linkLabels(edges, [fw, wan], new Set([fw.id]))
    expect(labels.get(`${wan.id}>${fw.id}`)!.at).toBe('source')
  })

  it("puts a link's traffic on the device it reaches, not on shared ports", () => {
    const nodes = [fw, wan, client('a'), client('b'), client('nas')]
    const edges = [
      edge(fw.id, 'a', { source_port: 'bridge0' }),
      edge(fw.id, 'b', { source_port: 'bridge0' }),
      edge(fw.id, 'nas', { source_port: 're0' }),
    ]
    const flows = deviceFlows(linkLabels(edges, nodes), edges)
    expect(flows.get('nas')).toEqual({ down: 9e6, up: 125e6 })
    expect(flows.has('a')).toBe(false) // the bridge is shared: no per-device traffic
  })
})

describe('port names on links', () => {
  const named: TopoNode = {
    ...fw,
    port_labels: { igb3: 'Uplink to the rack' },
    device: {
      ...fw.device!,
      interfaces: [
        { name: 'bridge0', description: 'LAN', speed_mbps: 10000 },
        { name: 'igb3', description: 'OPT1', speed_mbps: 1000 },
        { name: 're0', description: 're0', speed_mbps: 2500 },
      ],
    },
  }

  it("prefers the user's description, then the device's, never the bare interface name", () => {
    expect(portName(named, 'igb3')).toBe('Uplink to the rack')
    expect(portName(named, 'bridge0')).toBe('LAN')
    expect(portName(named, 're0')).toBeUndefined()
    expect(portName(named, undefined)).toBeUndefined()
  })

  it('names the shared port and a direct link', () => {
    const edges = [
      edge(named.id, 'a', { source_port: 'bridge0' }),
      edge(named.id, 'b', { source_port: 'bridge0' }),
      edge(named.id, 'sw', { source_port: 'igb3', speed_mbps: 1000 }),
    ]
    const labels = linkLabels(edges, [named, client('a'), client('b'), client('sw')])
    expect(labels.get(`${named.id}>a`)).toMatchObject({ name: 'LAN', speed: 10000 })
    expect(labels.get(`${named.id}>sw`)).toMatchObject({
      name: 'Uplink to the rack',
      speed: 1000,
    })
  })
})
