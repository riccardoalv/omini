import type { Edge, Node } from '@vue-flow/core'
import { computed, type Ref } from 'vue'

import type { NodeData } from '@/components/map/TopologyNode.vue'
import { worstByNode, alertsState } from '@/lib/alerts'
import { areaNodeId } from '@/lib/areas'
import { edgeLook, type GraphView } from '@/lib/graph'
import type { Direction } from '@/lib/layout'
import { NODE_SIZES } from '@/lib/sizes'
import { deviceFlows, linkLabels, nodeFlow } from '@/lib/traffic'
import type { MapArea, Point, TopoNode } from '@/lib/types'

type Frame = { id: string; x: number; y: number; width: number; height: number; name: string }

/** The nodes and links Vue Flow draws: devices, group bubbles, areas and the filter's frames. */
export function useFlowElements(options: {
  view: Ref<GraphView>
  /** The plain graph's nodes (no Wi-Fi networks): the links' labels are named from them. */
  nodes: Ref<TopoNode[]>
  nodeById: Ref<Map<string, TopoNode>>
  positions: Ref<Record<string, Point>>
  direction: Ref<Direction>
  visibleAreas: Ref<MapArea[]>
  netFrames: Ref<Frame[]>
  /** What the VLAN/subnet filter highlights; undefined: no filter. */
  inNet: Ref<Set<string> | undefined>
  failedIntegrations: Ref<Set<number>>
  /** A link entering an area: how far its pill keeps from the border (see useMapAreas). */
  insetOf: (source: string, target: string) => number
}) {
  const { view, nodes, nodeById, positions, direction, inNet } = options

  /**
   * Links show their port's name and maximum speed in the middle; the traffic
   * goes on the devices: internet traffic on routers and WANs, a link's traffic
   * on the device it reaches.
   */
  const linkInfo = computed(() => {
    const labels = linkLabels(view.value.edges, nodes.value)
    return {
      labels,
      flows: deviceFlows(labels, view.value.edges),
    }
  })
  /** The most severe open alert of each node: a mark on the node. */
  const alertOf = computed(() => worstByNode(alertsState.list))

  const flowNodes = computed<Node[]>(() => {
    const out: Node[] = options.visibleAreas.value.map((a) => ({
      id: areaNodeId(a.id),
      type: 'area',
      position: { x: a.x, y: a.y },
      data: { area: a },
      width: a.width,
      height: a.height,
      style: { width: `${a.width}px`, height: `${a.height}px` },
      zIndex: -1,
      dragHandle: '.area-title',
      class: 'area-node',
    }))
    const devices: Node<NodeData>[] = view.value.nodes.map((n) => ({
      id: n.id,
      type: 'omini',
      position: positions.value[n.id] ?? { x: 0, y: 0 },
      data: {
        node: n,
        error: n.integration_id ? options.failedIntegrations.value.has(n.integration_id) : false,
        direction: direction.value,
        flow: nodeFlow(n, nodeById.value) ?? linkInfo.value.flows.get(n.id),
        alert: alertOf.value.get(n.id),
      },
      width: NODE_SIZES[n.kind]!.width,
      height: NODE_SIZES[n.kind]!.height,
      class: inNet.value && !inNet.value.has(n.id) ? 'net-dim' : undefined,
      // A Wi-Fi network has no panel or menu, but it can be moved like any node.
      ...(n.kind === 'ssid' ? { selectable: false, focusable: false } : {}),
    }))
    for (const f of options.netFrames.value)
      out.push({
        id: `net-frame:${f.id}`,
        type: 'netframe',
        position: { x: f.x, y: f.y },
        data: { name: f.name },
        width: f.width,
        height: f.height,
        style: { width: `${f.width}px`, height: `${f.height}px` },
        zIndex: -2,
        draggable: false,
        selectable: false,
        focusable: false,
        class: 'net-frame-node',
      })
    out.push(...devices)
    for (const g of view.value.groups) {
      out.push({
        id: g.id,
        type: 'omini',
        position: positions.value[g.id] ?? { x: 0, y: 0 },
        data: { group: g, direction: direction.value },
        width: NODE_SIZES.group!.width,
        height: NODE_SIZES.group!.height,
      })
    }
    return out
  })

  /** The link each node hangs from in the layout: the first one reaching it (VPN tunnels aside). */
  const treeEdgeOf = computed(() => {
    const out = new Map<string, string>()
    for (const e of view.value.edges)
      if (e.kind !== 'vpn' && !out.has(e.target)) out.set(e.target, e.id)
    return out
  })

  const flowEdges = computed<Edge[]>(() => {
    const byId = new Map(nodes.value.map((n) => [n.id, n]))
    const labels = linkInfo.value.labels
    return view.value.edges.map((e) => {
      const source = byId.get(e.source)
      const target = byId.get(e.target)
      const look = edgeLook(e, source, target)
      return {
        id: e.id,
        source: e.source,
        target: e.target,
        type: 'link',
        data: {
          ...labels.get(e.id),
          inset: options.insetOf(e.source, e.target),
          // A second parent (a loop, a VPN): its pill sits at its own end, not on
          // top of the tree link's at the device.
          atSource: treeEdgeOf.value.get(e.target) !== e.id,
        },
        class: {
          slow: look.slow,
          vpn: look.vpn,
          offline: target ? !target.online : false,
          'net-dim': !!inNet.value && !(inNet.value.has(e.source) && inNet.value.has(e.target)),
        },
        style: {
          strokeWidth: look.width,
          strokeDasharray: look.dotted ? '2 4' : look.dashed ? '6 4' : undefined,
        },
      }
    })
  })

  return { flowNodes, flowEdges, linkLabels: computed(() => linkInfo.value.labels) }
}
