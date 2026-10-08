import { describe, expect, it } from 'vitest'

import type { TopoEdge, TopoNode } from '@/lib/types'
import { networkVlans, portCarries, vlanMembers } from '@/lib/vlans'

const fw: TopoNode = {
  id: 'fw',
  kind: 'device',
  label: 'fw',
  online: true,
  device: {
    key: 'fw',
    name: 'fw',
    vlans: [{ id: 20, name: 'IOT', subnet: '192.168.20.0/24' }],
    interfaces: [{ name: 'igc1' }, { name: 'igc1.20', vlan: 20, parent: 'igc1' }],
  },
}
const sw: TopoNode = {
  id: 'sw',
  kind: 'device',
  label: 'sw',
  online: true,
  device: {
    key: 'sw',
    name: 'sw',
    interfaces: [
      { name: 'Port 1', vlans: { untagged: 1, tagged: [20, 30] } },
      { name: 'Port 2', vlans: { untagged: 20 } },
      { name: 'Port 3', vlans: { untagged: 1 } },
    ],
    fdb: [{ mac: 'aa:00:00:00:00:04', port: 'Port 4', vlan: 30 }],
  },
}
const node = (id: string, mac?: string): TopoNode => ({
  id,
  kind: 'client',
  label: id,
  online: true,
  mac,
})
const nodes = [
  fw,
  sw,
  node('plug', 'aa:00:00:00:00:02'),
  node('pc', 'aa:00:00:00:00:03'),
  node('cam', 'aa:00:00:00:00:04'),
  node('bulb'),
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
  { id: 'e2', source: 'sw', target: 'plug', source_port: 'Port 2', kind: 'fdb' },
  { id: 'e3', source: 'sw', target: 'pc', source_port: 'Port 3', kind: 'fdb' },
  { id: 'e4', source: 'sw', target: 'cam', source_port: 'Port 4', kind: 'fdb' },
  { id: 'e5', source: 'fw', target: 'bulb', source_port: 'igc1.20', kind: 'inferred' },
]

describe('VLANs', () => {
  it('lists every VLAN of the network with its name', () => {
    expect(networkVlans(nodes)).toEqual([
      { id: 20, name: 'IOT', subnet: '192.168.20.0/24', interface: 'igc1.20' },
      { id: 30 },
    ])
  })

  it('knows which ports carry a VLAN', () => {
    expect(portCarries({ name: 'x', vlans: { tagged: [20] } }, 20)).toBe(true)
    expect(portCarries({ name: 'x', vlan: 20 }, 20)).toBe(true)
    expect(portCarries({ name: 'x', vlans: { untagged: 1 } }, 20)).toBe(false)
    expect(portCarries(undefined, 20)).toBe(false)
  })

  it('finds the devices and links of a VLAN', () => {
    const iot = vlanMembers(20, nodes, edges)
    expect([...iot.nodes].sort()).toEqual(['bulb', 'fw', 'plug', 'sw'])
    expect([...iot.edges].sort()).toEqual(['e2', 'e5', 'up'])
    const cams = vlanMembers(30, nodes, edges)
    expect(cams.nodes.has('cam')).toBe(true) // seen in VLAN 30 in the MAC table
    expect(cams.nodes.has('pc')).toBe(false)
  })
})
