import { describe, expect, it } from 'vitest'
import { ref, shallowRef } from 'vue'

import type { TopologyResponse } from '@/lib/types'

import { useFlowElements } from '../useFlowElements'
import { useMapGraph } from '../useMapGraph'
import { area, star } from './helpers'

function setup(topo: TopologyResponse, insetOf = () => 0) {
  const g = useMapGraph(shallowRef(topo), ref([]))
  return useFlowElements({
    view: g.view,
    nodes: g.nodes,
    nodeById: g.nodeById,
    positions: ref({ 'dev:fw': { x: 10, y: 20 } }),
    direction: g.direction,
    visibleAreas: ref([area(3, ['mac:0'], { x: 5, y: 6, width: 70, height: 80 })]),
    netFrames: ref([]),
    inNet: ref(),
    failedIntegrations: g.failedIntegrations,
    insetOf,
  })
}

describe('useFlowElements', () => {
  it('draws areas behind the devices, at their place and size', () => {
    const { flowNodes } = setup(star('dev:fw', 1))
    const [first, ...rest] = flowNodes.value
    expect(first).toMatchObject({
      id: 'area:3',
      type: 'area',
      position: { x: 5, y: 6 },
      width: 70,
      height: 80,
      zIndex: -1,
    })
    const fw = rest.find((n) => n.id === 'dev:fw')!
    expect(fw.position).toEqual({ x: 10, y: 20 })
    expect(fw.width).toBe(220)
    // A node not laid out yet waits at the origin.
    expect(rest.find((n) => n.id === 'mac:0')!.position).toEqual({ x: 0, y: 0 })
  })

  it('marks failing integrations, Wi-Fi networks and second parents', () => {
    const topo = star('dev:fw', 1)
    topo.topology.nodes[0]!.integration_id = 2
    topo.statuses = [{ integration_id: 2, ok: false, collected_at: '', duration_ms: 0, devices: 0 }]
    topo.topology.nodes.push(
      { id: 'dev:ap', kind: 'device', label: 'ap', online: true },
      { id: 'phone', kind: 'client', label: 'phone', online: true, band: '5ghz' },
    )
    topo.topology.edges.push(
      { id: 'w', source: 'dev:ap', target: 'phone', kind: 'wifi', source_port: 'Home · 5 GHz' },
      // A VPN tunnel to the client too: not the link it hangs from.
      { id: 'v', source: 'dev:fw', target: 'mac:0', kind: 'vpn' },
    )
    const { flowNodes, flowEdges } = setup(topo, () => 24)
    const fw = flowNodes.value.find((n) => n.id === 'dev:fw')!
    expect(fw.data.error).toBe(true)
    const net = flowNodes.value.find((n) => n.data?.node?.kind === 'ssid')!
    expect(net.selectable).toBe(false)
    expect(net.focusable).toBe(false)
    const tree = flowEdges.value.find((e) => e.id === 'e:mac:0')!
    const vpn = flowEdges.value.find((e) => e.id === 'v')!
    expect(tree.data.atSource).toBe(false)
    expect(vpn.data.atSource).toBe(true)
    expect(tree.data.inset).toBe(24)
    expect((vpn.class as Record<string, boolean>).vpn).toBe(true)
  })
})
