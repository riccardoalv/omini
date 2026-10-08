import { describe, expect, it } from 'vitest'

import type { TopoEdge, TopoNode } from '../types'
import { deviceFlows, formatRate, linkLabels, linkSeries, nodeFlow, portName } from '../traffic'

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
      name: 're0',
      flow: { down: 125e6, up: 9e6 },
      at: 'middle',
    })
    // The LAN bridge: one pill at the firewall with the port's 10G and traffic.
    expect(labels.get(`${fw.id}>a`)).toEqual({
      speed: 10000,
      name: 'bridge0',
      flow: { down: 118e6, up: 10e6 },
      at: 'shared',
      hidden: false,
    })
    expect(labels.get(`${fw.id}>b`)!.hidden).toBe(true)
    // A port with a single device keeps the link's own speed, no traffic known.
    expect(labels.get(`${fw.id}>modem`)).toEqual({
      speed: 1000,
      name: 'igb3',
      flow: undefined,
      at: 'middle',
    })
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
    port_labels: { igb3: 'Uplink to the rack', mlxen0: 'Porta LAN' },
    device: {
      ...fw.device!,
      interfaces: [
        {
          name: 'bridge0',
          type: 'bridge',
          description: 'LAN',
          speed_mbps: 10000,
          up: true,
          members: ['mlxen0', 'vtnet0'],
        },
        {
          name: 'mlxen0',
          type: 'ethernet',
          description: 'LAN_PHYSICAL',
          speed_mbps: 10000,
          up: true,
        },
        { name: 'vtnet0', type: 'other', up: true },
        { name: 'igb3', type: 'ethernet', description: 'OPT1', speed_mbps: 1000, up: true },
        { name: 're0', type: 'ethernet', description: 'WAN_PHYSICAL', speed_mbps: 2500, up: true },
      ],
    },
  }

  it("uses the name the user gave, else the interface's own — never its description", () => {
    expect(portName(named, 'igb3')).toBe('Uplink to the rack')
    expect(portName(named, 're0')).toBe('re0')
    expect(portName(named, undefined)).toBeUndefined()
  })

  it('names a bridge after the physical port behind it', () => {
    expect(portName(named, 'bridge0')).toBe('Porta LAN')
    const unnamed = { ...named, port_labels: {} }
    expect(portName(unnamed, 'bridge0')).toBe('mlxen0')
    // Without a member list: the only physical port up at the bridge's speed.
    const old: TopoNode = {
      ...unnamed,
      device: {
        ...unnamed.device!,
        interfaces: unnamed.device!.interfaces!.map((i) => ({ ...i, members: undefined })),
      },
    }
    expect(portName(old, 'bridge0')).toBe('mlxen0')
  })

  it('names the shared port and a direct link', () => {
    const edges = [
      edge(named.id, 'a', { source_port: 'bridge0' }),
      edge(named.id, 'b', { source_port: 'bridge0' }),
      edge(named.id, 'sw', { source_port: 'igb3', speed_mbps: 1000 }),
    ]
    const labels = linkLabels(edges, [named, client('a'), client('b'), client('sw')])
    expect(labels.get(`${named.id}>a`)).toMatchObject({ name: 'Porta LAN', speed: 10000 })
    expect(labels.get(`${named.id}>sw`)).toMatchObject({
      name: 'Uplink to the rack',
      speed: 1000,
    })
  })
})

describe('Wi-Fi clients', () => {
  const phone: TopoNode = { ...client('phone'), flow: { rx_bps: 2e6, tx_bps: 1e5 } }

  it('show the traffic their access point measures, nothing while idle', () => {
    expect(nodeFlow(phone, new Map())).toEqual({ down: 2e6, up: 1e5 })
    expect(nodeFlow({ ...phone, flow: { rx_bps: 0, tx_bps: 0 } }, new Map())).toBeUndefined()
  })
})

describe('links without a pill', () => {
  it("an app's link: the app shows its port itself", () => {
    const app: TopoNode = { id: 'app:vm:8096', kind: 'app', label: 'Jellyfin', online: true }
    const edges = [edge(fw.id, app.id, { source_port: '8096' })]
    expect(linkLabels(edges, [fw, app]).has(`${fw.id}>${app.id}`)).toBe(false)
  })

  it('a Wi-Fi link: its network is a node of its own', () => {
    const edges = [edge(fw.id, 'phone', { source_port: 'IOT · 2.4 GHz', kind: 'wifi' })]
    expect(linkLabels(edges, [fw, client('phone')]).has(`${fw.id}>phone`)).toBe(false)
  })
})

describe('traffic on the links', () => {
  const fw: TopoNode = {
    id: 'fw',
    kind: 'device',
    label: 'fw',
    online: true,
    traffic: { igc1: { rx_bps: 2e6, tx_bps: 9e6 } },
  }
  const ap: TopoNode = { id: 'ap', kind: 'device', label: 'AP', online: true }
  const phone: TopoNode = {
    id: 'phone',
    kind: 'client',
    label: 'Phone',
    online: true,
    flow: { rx_bps: 3e6, tx_bps: 1e5 },
  }
  const tv: TopoNode = { id: 'tv', kind: 'client', label: 'TV', online: true }
  const nodes = [fw, ap, phone, tv]
  const edges: TopoEdge[] = [
    { id: 'e1', source: 'fw', target: 'ap', source_port: 'igc1', kind: 'lldp' },
    { id: 'e2', source: 'ap', target: 'phone', kind: 'wifi' },
    { id: 'e3', source: 'ap', target: 'tv', kind: 'fdb' },
  ]

  it('finds the history of a link', () => {
    const byId = new Map(nodes.map((n) => [n.id, n]))
    expect(linkSeries(edges[0]!, byId)).toEqual({
      node: 'fw',
      iface: 'igc1',
      swap: true,
      name: 'fw → AP',
    })
    expect(linkSeries(edges[1]!, byId)).toMatchObject({ node: 'phone', iface: '', swap: false })
    expect(linkSeries(edges[2]!, byId)).toBeUndefined()
  })
})
