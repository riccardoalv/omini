import { useVueFlow } from '@vue-flow/core'
import { computed, nextTick, ref, watch, type Ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { api } from '@/lib/api'
import { AREA_PADDING, AREA_TITLE, withDescendants } from '@/lib/areas'
import type { GraphView } from '@/lib/graph'
import {
  alignOn,
  clearSaved,
  layout,
  layoutKey,
  type Direction,
  type LayoutGroup,
  modelOrder,
  positionsFor,
} from '@/lib/layout'
import { displayName } from '@/lib/names'
import { prefs } from '@/lib/prefs'
import { NODE_SIZES } from '@/lib/sizes'
import type { MapArea, Point, TopoEdge, TopologyResponse } from '@/lib/types'

/**
 * Where every node of the map stands: laid out again when the visible graph,
 * the direction or the areas change (not on every poll), with the positions
 * the user dragged kept (here and on the server, per direction).
 */
export function useMapLayout(options: {
  data: Ref<TopologyResponse | undefined>
  view: Ref<GraphView>
  direction: Ref<Direction>
  liveAreas: Ref<MapArea[]>
  /** An area's members as they are on the map (see useMapGraph). */
  onMap: (members: string[]) => string[]
  /** The node the user expanded or collapsed, which stays still. */
  takeAnchor: () => string | undefined
}) {
  const { data, view, direction, liveAreas, onMap, takeAnchor } = options
  const { t } = useI18n()
  const { fitView, onNodesInitialized, updateNodeInternals } = useVueFlow('omini-map')

  const positions = ref<Record<string, Point>>({})
  let lastLayout = '' // signature of the last laid out graph
  const layoutsApplied = ref(0) // counts layouts put on screen (an export waits for one)
  // Positions the user dragged in this session (also saved on the server),
  // keyed per direction like the server layout.
  const draggedPositions: Record<string, Point> = {}

  /** Boxes of the nodes (and group bubbles) laid out on the map. */
  const nodeBoxes = computed(
    () =>
      new Map(
        [
          ...view.value.nodes.map((n) => ({ id: n.id, kind: n.kind })),
          ...view.value.groups.map((g) => ({ id: g.id, kind: 'group' })),
        ]
          .filter((b) => positions.value[b.id])
          .map((b) => [b.id, { id: b.id, ...positions.value[b.id]!, ...NODE_SIZES[b.kind]! }]),
      ),
  )

  /**
   * Areas are laid out as boxes: their devices stay together and no other device
   * lands among them. The top padding leaves room for the title.
   */
  function layoutGroups(edges: TopoEdge[]): LayoutGroup[] {
    return liveAreas.value.map((a) => ({
      id: String(a.id),
      members: onMap(a.members),
      children: withDescendants(onMap(a.members), edges),
      padding: [AREA_PADDING + AREA_TITLE, AREA_PADDING, AREA_PADDING, AREA_PADDING],
    }))
  }

  /** Areas and their devices (drawn or automatic): the map is laid out again when they change. */
  const autoAreaKey = computed(() =>
    liveAreas.value.map((a) => `${a.id}:${a.members.join(',')}`).join(';'),
  )

  // Re-layout only when the visible graph, the direction or the areas
  // change, not on every poll.
  watch([view, direction, autoAreaKey], async ([v, direction, autoKey]) => {
    const ids = [...v.nodes.map((n) => n.id), ...v.groups.map((g) => g.id)]
    const key =
      direction + '|' + ids.join(',') + '|' + v.edges.map((e) => e.id).join(',') + '|' + autoKey
    if (key === lastLayout) {
      layoutsApplied.value++
      return
    }
    const refit = lastLayout === '' || !lastLayout.startsWith(direction + '|')
    lastLayout = key
    const saved = positionsFor({ ...data.value?.layout, ...draggedPositions }, direction)
    const sized = new Map([
      ...v.nodes.map((n) => [n.id, { id: n.id, ...NODE_SIZES[n.kind]! }] as const),
      ...v.groups.map((g) => [g.id, { id: g.id, ...NODE_SIZES.group! }] as const),
    ])
    const names = new Map(v.nodes.map((n) => [n.id, displayName(n, t)]))
    const order = modelOrder([...sized.keys()], v.edges, (id) => names.get(id) ?? id)
    const rank = new Map(order.map((id, i) => [id, i]))
    const boxes = order.map((id) => sized.get(id)!)
    const edgesInOrder = [...v.edges].sort((a, b) => rank.get(a.target)! - rank.get(b.target)!)
    // A VPN tunnel joins two sites; it does not make one hang from the other.
    const treeEdges = edgesInOrder.filter((e) => e.kind !== 'vpn')
    const fresh = await layout(boxes, treeEdges, saved, direction, layoutGroups(v.edges))
    // Expanding or collapsing keeps the clicked node where it is; the map is laid
    // out again around it (no overlaps). Other changes keep the first node still.
    const expanded = takeAnchor()
    const anchor =
      expanded && positions.value[expanded] ? expanded : ids.find((id) => positions.value[id])
    // Shifting the layout may bring automatic nodes onto dragged ones again.
    positions.value = refit
      ? fresh
      : clearSaved(
          alignOn(positions.value, fresh, anchor, new Set(Object.keys(saved))),
          boxes,
          saved,
          direction,
        )
    if (refit) {
      // Handles moved (left/right vs top/bottom): Vue Flow must re-measure them.
      await nextTick()
      updateNodeInternals()
      fitSoon()
    }
    layoutsApplied.value++
  })

  // The map area changes width when the sidebar expands or collapses.
  watch(
    () => prefs.sidebarExpanded,
    () => setTimeout(fitSoon, 220), // after the sidebar transition
  )

  // Fits the whole map once Vue Flow has rendered the new positions.
  function fitSoon() {
    nextTick(() => requestAnimationFrame(() => fitView({ padding: 0.12, maxZoom: 1.1 })))
  }
  // Fit only the first time: later changes (expand, collapse, new devices) keep the viewport.
  let fitted = false
  onNodesInitialized(() => {
    if (fitted) return
    fitted = true
    fitSoon()
  })

  /** Keeps positions the user dragged (rounded), on the map and on the server. */
  function keep(moved: Record<string, Point>): Promise<void> {
    const rounded: Record<string, Point> = {}
    const saved: Record<string, Point> = {}
    for (const [id, p] of Object.entries(moved)) {
      rounded[id] = { x: Math.round(p.x), y: Math.round(p.y) }
      saved[layoutKey(prefs.layoutDirection, id)] = rounded[id]!
    }
    Object.assign(draggedPositions, saved)
    positions.value = { ...positions.value, ...rounded }
    return Object.keys(saved).length ? api.saveLayout(saved) : Promise.resolve()
  }

  /** Lays the map out from scratch on the next change (a new scan, a reset). */
  function relayout() {
    lastLayout = ''
  }

  /** Forgets the positions dragged in this session and lays the map out from scratch. */
  function forget() {
    for (const k of Object.keys(draggedPositions)) delete draggedPositions[k]
    relayout()
  }

  /** Waits for the map to be laid out and drawn after a change. */
  async function untilLaidOut(change: () => void) {
    const before = layoutsApplied.value
    change()
    for (let i = 0; i < 200 && layoutsApplied.value === before; i++) {
      await new Promise((r) => setTimeout(r, 25))
    }
    await nextTick()
    await new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r)))
  }

  return { positions, nodeBoxes, keep, relayout, forget, untilLaidOut }
}
