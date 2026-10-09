import { describe, expect, it } from 'vitest'
import { computed, ref, shallowRef } from 'vue'

import type { Box } from '@/lib/areas'
import type { TopologyResponse } from '@/lib/types'

import { useFlowElements } from '../useFlowElements'
import { useMapGraph } from '../useMapGraph'
import { useNetworkFilter } from '../useNetworkFilter'

/** A firewall with two subnets, a device in each. */
const twoSubnets = (): TopologyResponse => ({
  topology: {
    nodes: [
      {
        id: 'dev:fw',
        kind: 'device',
        label: 'fw',
        online: true,
        device: {
          key: 'fw',
          name: 'fw',
          interfaces: [
            { name: 'igc1', description: 'LAN', ips: ['192.168.1.1/24'] },
            { name: 'igc2', description: 'LAB', ips: ['10.0.5.1/24'] },
          ],
        },
      },
      { id: 'pc', kind: 'client', label: 'pc', online: true, ip: '192.168.1.20' },
      { id: 'srv', kind: 'client', label: 'srv', online: true, ip: '10.0.5.9' },
    ],
    edges: [
      { id: 'e1', source: 'dev:fw', target: 'pc', kind: 'inferred' },
      { id: 'e2', source: 'dev:fw', target: 'srv', kind: 'inferred' },
    ],
  },
  statuses: [],
  generated_at: '2026-10-07T00:00:00Z',
  layout: {},
  areas: [],
})

function setup() {
  const g = useMapGraph(shallowRef<TopologyResponse>(twoSubnets()), ref([]))
  const positions = ref({
    'dev:fw': { x: 0, y: 0 },
    pc: { x: 400, y: 0 },
    srv: { x: 400, y: 100 },
  })
  const nodeBoxes = computed(
    () =>
      new Map<string, Box>(
        Object.entries(positions.value).map(([id, p]) => [
          id,
          { id, ...p, width: 200, height: 40 },
        ]),
      ),
  )
  const filter = useNetworkFilter(g.mapGraph, g.view, nodeBoxes)
  const flow = useFlowElements({
    view: g.view,
    nodes: g.nodes,
    nodeById: g.nodeById,
    positions,
    direction: g.direction,
    visibleAreas: ref([]),
    netFrames: filter.netFrames,
    inNet: filter.inNet,
    failedIntegrations: g.failedIntegrations,
    insetOf: () => 0,
  })
  return { filter, flow }
}

describe('useNetworkFilter', () => {
  it('highlights the picked subnet, framed, and dims the rest', () => {
    const { filter, flow } = setup()
    expect(filter.networkGroups.value.map((n) => n.name)).toEqual([
      'LAB · 10.0.5.0/24',
      'LAN · 192.168.1.0/24',
    ])
    expect(filter.inNet.value).toBeUndefined()
    expect(filter.netFrames.value).toEqual([])
    expect(flow.flowNodes.value.some((n) => n.class === 'net-dim')).toBe(false)

    filter.netFilter.value = filter.networkGroups.value[0]!.key
    expect([...filter.inNet.value!].sort()).toEqual(['dev:fw', 'srv'])
    expect(filter.netFrames.value).toHaveLength(1)
    expect(filter.netFrames.value[0]!.name).toBe('LAB · 10.0.5.0/24')
    const nodes = flow.flowNodes.value
    expect(nodes.find((n) => n.id === 'pc')!.class).toBe('net-dim')
    expect(nodes.find((n) => n.id === 'srv')!.class).toBeUndefined()
    // The frame is drawn behind the nodes and never takes the mouse.
    const frame = nodes.find((n) => n.type === 'netframe')!
    expect(frame.zIndex).toBe(-2)
    expect(frame.draggable).toBe(false)
    const dimmed = flow.flowEdges.value.find((e) => e.id === 'e1')!
    expect((dimmed.class as Record<string, boolean>)['net-dim']).toBe(true)
  })
})
