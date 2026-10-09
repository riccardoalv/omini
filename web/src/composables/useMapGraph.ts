import { computed, ref, type Ref } from 'vue'

import { collapseAreas } from '@/lib/areas'
import { collapseClients, withoutHidden, withoutOffline, withWifiNetworks } from '@/lib/graph'
import type { Direction } from '@/lib/layout'
import { prefs } from '@/lib/prefs'
import type { MapArea, TopologyResponse } from '@/lib/types'

/**
 * The graph the map draws, from the topology: hidden and offline devices left
 * out (unless asked for), Wi-Fi networks as mini nodes, clients folded into
 * bubbles and collapsed areas into one.
 */
export function useMapGraph(data: Ref<TopologyResponse | undefined>, areas: Ref<MapArea[]>) {
  const allNodes = computed(() => data.value?.topology.nodes ?? [])
  const allEdges = computed(() => data.value?.topology.edges ?? [])
  /** Shows the devices the user hid (to bring one back); not remembered. */
  const showHidden = ref(false)
  const unhidden = computed(() =>
    showHidden.value
      ? { nodes: allNodes.value, edges: allEdges.value, hidden: 0 }
      : withoutHidden(allNodes.value, allEdges.value),
  )
  // Hidden devices and hidden areas: "Show hidden" brings both back for a while.
  const hiddenCount = computed(
    () =>
      allNodes.value.filter((n) => n.hidden).length +
      areas.value.filter((a) => a.hidden && !a.dismissed).length,
  )
  const filtered = computed(() =>
    prefs.hideOffline ? withoutOffline(unhidden.value.nodes, unhidden.value.edges) : unhidden.value,
  )
  const nodes = computed(() => filtered.value.nodes)
  const edges = computed(() => filtered.value.edges)
  const offlineCount = computed(() => allNodes.value.filter((n) => !n.online).length)
  const expanded = computed(() => new Set(prefs.expanded))
  const forced = computed(() => new Set(prefs.collapsed))
  // The map's graph: each access point's Wi-Fi networks as mini nodes between it
  // and its clients (the panel and lists use the plain graph).
  const mapGraph = computed(() => withWifiNetworks(nodes.value, edges.value))
  // While exporting an image, everything is shown expanded (no bubbles), in the
  // orientation picked for the image (the user's own preference is untouched).
  const expandAll = ref(false)
  const exportDirection = ref<Direction>()
  const direction = computed(() => exportDirection.value ?? prefs.layoutDirection)
  /** Areas on the map: the ones the user drew (VLANs and subnets are a filter, not areas). */
  const liveAreas = computed(() =>
    areas.value.filter((a) => !a.auto && !a.dismissed && (!a.hidden || showHidden.value)),
  )
  /** Areas collapsed into a bubble (whatever the orientation they were drawn in). */
  const collapsedAreaList = computed(() =>
    liveAreas.value.filter((a) => prefs.collapsedAreas.includes(a.id)),
  )
  const clientView = computed(() =>
    collapseClients(
      mapGraph.value.nodes,
      mapGraph.value.edges,
      prefs.collapseThreshold,
      expanded.value,
      forced.value,
    ),
  )
  const view = computed(() =>
    expandAll.value
      ? collapseClients(mapGraph.value.nodes, mapGraph.value.edges, Infinity, new Set())
      : collapseAreas(clientView.value, collapsedAreaList.value),
  )
  const failedIntegrations = computed(
    () => new Set((data.value?.statuses ?? []).filter((s) => !s.ok).map((s) => s.integration_id)),
  )
  const summary = computed(() => {
    const all = nodes.value
    return {
      devices: all.filter((n) => n.kind === 'device' || n.kind === 'unmanaged').length,
      clients: all.filter((n) => n.kind === 'client' && n.online).length,
      problems: failedIntegrations.value.size,
    }
  })
  const nodeById = computed(() => new Map(allNodes.value.map((n) => [n.id, n])))

  /**
   * An area's members as they are on the map: a member folded into a bubble
   * (its parent collapsed) is that bubble — the area follows it instead of
   * staying empty where it was.
   */
  function onMap(members: string[]): string[] {
    const shown = new Set(view.value.nodes.map((n) => n.id))
    const out = new Set<string>()
    for (const m of members) {
      if (shown.has(m)) out.add(m)
      else {
        const g = view.value.groups.find((x) => x.clients.some((c) => c.id === m))
        if (g) out.add(g.id)
      }
    }
    return [...out]
  }

  return {
    allNodes,
    showHidden,
    hiddenCount,
    nodes,
    edges,
    offlineCount,
    mapGraph,
    expandAll,
    exportDirection,
    direction,
    liveAreas,
    view,
    failedIntegrations,
    summary,
    nodeById,
    onMap,
  }
}
