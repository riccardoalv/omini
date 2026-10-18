import { describe, expect, it } from 'vitest'

import type { TopoNode } from '@/lib/types'
import { networkVlans, portCarries } from '@/lib/vlans'

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
describe('VLANs', () => {
  it('lists every VLAN of the network with its name', () => {
    expect(networkVlans(nodes)).toEqual([
      { id: 20, name: 'IOT', subnet: '192.168.20.0/24', interface: 'igc1.20' },
      { id: 30 },
    ])
  })

  it("leaves out the VLAN the operator's PPPoE rides on", () => {
    const fw: TopoNode = {
      id: 'fw2',
      kind: 'device',
      label: 'fw2',
      online: true,
      device: {
        key: 'fw2',
        name: 'fw2',
        interfaces: [
          { name: 'vlan0.2000', vlan: 2000, parent: 're0' },
          { name: 'pppoe1', parent: 'vlan0.2000', wan: true },
        ],
        vlans: [{ id: 2000, interface: 'vlan0.2000' }],
      },
    }
    expect(networkVlans([...nodes, fw]).map((v) => v.id)).toEqual([20, 30])
  })

  it('knows which ports carry a VLAN', () => {
    expect(portCarries({ name: 'x', vlans: { tagged: [20] } }, 20)).toBe(true)
    expect(portCarries({ name: 'x', vlan: 20 }, 20)).toBe(true)
    expect(portCarries({ name: 'x', vlans: { untagged: 1 } }, 20)).toBe(false)
    expect(portCarries(undefined, 20)).toBe(false)
  })
})
