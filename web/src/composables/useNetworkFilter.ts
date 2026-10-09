import { computed, ref, type Ref } from 'vue'

import { type Box, clusterFrames } from '@/lib/areas'
import { autoGroups, networkHighlight } from '@/lib/autoAreas'
import type { GraphView } from '@/lib/graph'
import type { TopoEdge, TopoNode } from '@/lib/types'

/**
 * The VLANs and subnets of the network (when there are several), each with
 * every device in it: the toolbar's filter highlights one and dims the rest
 * (as Omada, Auvik or Catalyst do) instead of drawing them as areas.
 */
export function useNetworkFilter(
  mapGraph: Ref<{ nodes: TopoNode[]; edges: TopoEdge[] }>,
  view: Ref<GraphView>,
  nodeBoxes: Ref<Map<string, Box>>,
) {
  const networkGroups = computed(() =>
    autoGroups(mapGraph.value.nodes, mapGraph.value.edges, new Set(), 'inclusive'),
  )
  // The devices only in one VLAN or subnet: what hangs below them is in it too.
  const onlyIn = computed(() => autoGroups(mapGraph.value.nodes, mapGraph.value.edges))
  const netFilter = ref<string>()
  /**
   * Nodes of the picked VLAN or subnet; undefined: no filter. A device only in it
   * brings what hangs below it; one also in other networks (the firewall, with an
   * address in each) lights up alone — else picking the modem's network lit the
   * whole LAN under the firewall.
   */
  const inNet = computed(() =>
    networkHighlight(netFilter.value, networkGroups.value, onlyIn.value, view.value.edges),
  )

  /** Temporary frames around what the VLAN/subnet filter highlights (one per linked group). */
  const netFrames = computed(() => {
    if (!inNet.value) return []
    const name = networkGroups.value.find((g) => g.key === netFilter.value)?.name ?? ''
    return clusterFrames(inNet.value, view.value.edges, nodeBoxes.value).map((f) => ({
      ...f,
      name,
    }))
  })

  return { networkGroups, netFilter, inNet, netFrames }
}
