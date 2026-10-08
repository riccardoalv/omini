<script setup lang="ts">
import { Background } from '@vue-flow/background'
import { Controls } from '@vue-flow/controls'
import type { Edge, EdgeMouseEvent, Node, NodeDragEvent, NodeMouseEvent } from '@vue-flow/core'
import { VueFlow, useVueFlow } from '@vue-flow/core'
import {
  ArrowDownFromLine,
  ArrowRightFromLine,
  Bell,
  Download,
  Eye,
  EyeOff,
  LayoutGrid,
  RefreshCw,
  SquareDashed,
} from 'lucide-vue-next'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'

import ModalDialog from '@/components/ModalDialog.vue'
import TrafficChart from '@/components/TrafficChart.vue'
import AreaMenu from '@/components/map/AreaMenu.vue'
import AreaNode from '@/components/map/AreaNode.vue'
import LinkEdge from '@/components/map/LinkEdge.vue'
import NodeMenu from '@/components/map/NodeMenu.vue'
import ExportDialog, { type ExportOptions } from '@/components/map/ExportDialog.vue'
import NodePanel from '@/components/map/NodePanel.vue'
import TopologyNode, { type NodeData } from '@/components/map/TopologyNode.vue'
import { alertsState, attentionCount, worstByNode } from '@/lib/alerts'
import { api } from '@/lib/api'
import {
  AREA_PADDING,
  AREA_TITLE,
  areaIdOf,
  areaNodeId,
  collapseAreas,
  fitArea,
  MIN_AREA_SIZE,
  membersOf,
  rectFrom,
  regroup,
  withDescendants,
} from '@/lib/areas'
import { areaMembers, AUTO_AREA_COLORS, autoGroups } from '@/lib/autoAreas'
import { formatAgo, formatSpeed } from '@/lib/format'
import {
  clientCount,
  collapseClients,
  edgeLook,
  isWifiNetwork,
  withoutHidden,
  withoutOffline,
  withWifiNetworks,
} from '@/lib/graph'
import {
  alignOn,
  clearSaved,
  layout,
  layoutKey,
  type LayoutGroup,
  modelOrder,
  positionsFor,
} from '@/lib/layout'
import { displayName } from '@/lib/names'
import { mapDrawio } from '@/lib/drawio'
import { download, exportName, mapImage, mapJSON } from '@/lib/export'
import { applyTheme, prefs } from '@/lib/prefs'
import { deviceFlows, linkLabels, linkSeries, nodeFlow, type LinkSeries } from '@/lib/traffic'
import type { Integration, MapArea, Point, TopoEdge, TopoNode, TopologyResponse } from '@/lib/types'

const POLL_MS = 10_000
const SIZES: Record<string, { width: number; height: number }> = {
  device: { width: 220, height: 60 },
  unmanaged: { width: 200, height: 56 },
  segment: { width: 240, height: 56 },
  client: { width: 200, height: 38 },
  app: { width: 180, height: 34 },
  wan: { width: 180, height: 56 },
  ssid: { width: 190, height: 26 },
  group: { width: 150, height: 44 },
}

const { t, locale } = useI18n()
const router = useRouter()
const route = useRoute()
const {
  fitView,
  getViewport,
  onNodesInitialized,
  setViewport,
  updateNodeInternals,
  viewport,
  screenToFlowCoordinate,
} = useVueFlow('omini-map')

const data = shallowRef<TopologyResponse>()
const integrations = ref<Integration[]>([])
const loaded = ref(false)
const positions = ref<Record<string, Point>>({})
const selectedId = ref<string>()
const refreshing = ref(false)
const now = ref(Date.now())
let lastLayout = '' // signature of the last laid out graph
const layoutsApplied = ref(0) // counts layouts put on screen (an export waits for one)
let layoutAnchor: string | undefined // node the user expanded or collapsed
let timer: ReturnType<typeof setInterval> | undefined

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
// Map areas: declared before the view, which collapses them.
const areas = ref<MapArea[]>([])
/** A color being picked in the area menu, shown on the area until it is applied or the menu closes. */
const areaPreview = ref<{ id: number; color: string }>()
// The map's graph: each access point's Wi-Fi networks as mini nodes between it
// and its clients (the panel and lists use the plain graph).
const mapGraph = computed(() => withWifiNetworks(nodes.value, edges.value))
// While exporting an image, everything is shown expanded (no bubbles), in the
// orientation picked for the image (the user's own preference is untouched).
const expandAll = ref(false)
const exportDirection = ref<'RIGHT' | 'DOWN'>()
const direction = computed(() => exportDirection.value ?? prefs.layoutDirection)
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
/** Areas on the map: automatic ones the user removed are kept only so they are not made again. */
const liveAreas = computed(() =>
  areas.value.filter((a) => !a.dismissed && (!a.hidden || showHidden.value)),
)
/**
 * One automatic area per VLAN and subnet (when there are several), with the
 * devices only in it; devices in an area the user drew stay in that one.
 */
const autoAreaGroups = computed(() => {
  const drawn = liveAreas.value.filter((a) => !a.auto).flatMap((a) => a.members)
  return autoGroups(
    mapGraph.value.nodes,
    mapGraph.value.edges,
    new Set(withDescendants(drawn, mapGraph.value.edges)),
  )
})
const autoMembers = computed(() => new Map(autoAreaGroups.value.map((g) => [g.key, g.members])))
/** Areas collapsed into a bubble (whatever the orientation they were drawn in). */
const collapsedAreaList = computed(() =>
  liveAreas.value
    .filter((a) => prefs.collapsedAreas.includes(a.id))
    .map((a) => ({ ...a, members: areaMembers(a, clientView.value, autoMembers.value) })),
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
// "Empty" means no devices at all: hidden or collapsed devices do not count.
const empty = computed(() => loaded.value && allNodes.value.length === 0)

const nodeById = computed(() => new Map(allNodes.value.map((n) => [n.id, n])))

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
const attention = computed(() => attentionCount(alertsState.list))

const flowNodes = computed<Node[]>(() => {
  const out: Node[] = visibleAreas.value.map((a) => ({
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
      error: n.integration_id ? failedIntegrations.value.has(n.integration_id) : false,
      direction: direction.value,
      flow: nodeFlow(n, nodeById.value) ?? linkInfo.value.flows.get(n.id),
      alert: alertOf.value.get(n.id),
    },
    width: SIZES[n.kind]!.width,
    height: SIZES[n.kind]!.height,
    // A Wi-Fi network has no panel or menu, but it can be moved like any node.
    ...(n.kind === 'ssid' ? { selectable: false, focusable: false } : {}),
  }))
  out.push(...devices)
  for (const g of view.value.groups) {
    out.push({
      id: g.id,
      type: 'omini',
      position: positions.value[g.id] ?? { x: 0, y: 0 },
      data: { group: g, direction: direction.value },
      width: SIZES.group!.width,
      height: SIZES.group!.height,
    })
  }
  return out
})

/** The area each node on the map is drawn in. */
const areaOfNode = computed(() => {
  const out = new Map<string, number>()
  for (const a of visibleAreas.value)
    for (const id of areaNodes(a)) if (!out.has(id)) out.set(id, a.id)
  return out
})
/** A link entering an area: its pill stays out of the area's border (and title, top down). */
function insetOf(source: string, target: string) {
  const a = areaOfNode.value.get(target)
  if (a === undefined || areaOfNode.value.get(source) === a) return 0
  return direction.value === 'DOWN' ? AREA_PADDING + AREA_TITLE : AREA_PADDING
}

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
      data: { ...labels.get(e.id), inset: insetOf(e.source, e.target) },
      class: {
        slow: look.slow,
        offline: target ? !target.online : false,
      },
      style: {
        strokeWidth: look.width,
        strokeDasharray: look.dotted ? '2 4' : look.dashed ? '6 4' : undefined,
      },
    }
  })
})

const selectedNode = computed(() => nodes.value.find((n) => n.id === selectedId.value))
const selectedGroup = computed(() => view.value.groups.find((g) => g.id === selectedId.value))
const selectedIntegration = computed(() =>
  integrations.value.find((i) => i.id === selectedNode.value?.integration_id),
)

async function load() {
  try {
    const [topo, ints] = await Promise.all([api.topology(), api.integrations()])
    data.value = topo
    integrations.value = ints
    if (topo.alerts) {
      alertsState.list = topo.alerts
      alertsState.loaded = true
    }
    focusRequested()
    // Never overwrite an area while the user is moving, resizing or renaming it.
    if (!areaBusy.value && editingArea.value === undefined) areas.value = topo.areas ?? []
  } finally {
    loaded.value = true
    now.value = Date.now()
  }
}

/**
 * Areas are laid out as boxes: their devices stay together and no other device
 * lands among them. The top padding leaves room for the title.
 */
function layoutGroups(edges: TopoEdge[]): LayoutGroup[] {
  return liveAreas.value.map((a) => ({
    id: String(a.id),
    children: withDescendants(areaMembers(a, view.value, autoMembers.value), edges),
    padding: [AREA_PADDING + AREA_TITLE, AREA_PADDING, AREA_PADDING, AREA_PADDING],
  }))
}

/** Areas and their devices (drawn or automatic): the map is laid out again when they change. */
const autoAreaKey = computed(() =>
  liveAreas.value
    .map((a) => `${a.id}:${areaMembers(a, view.value, autoMembers.value).join(',')}`)
    .join(';'),
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
    ...v.nodes.map((n) => [n.id, { id: n.id, ...SIZES[n.kind]! }] as const),
    ...v.groups.map((g) => [g.id, { id: g.id, ...SIZES.group! }] as const),
  ])
  const names = new Map(v.nodes.map((n) => [n.id, displayName(n, t)]))
  const order = modelOrder([...sized.keys()], v.edges, (id) => names.get(id) ?? id)
  const rank = new Map(order.map((id, i) => [id, i]))
  const boxes = order.map((id) => sized.get(id)!)
  const edgesInOrder = [...v.edges].sort((a, b) => rank.get(a.target)! - rank.get(b.target)!)
  const fresh = await layout(boxes, edgesInOrder, saved, direction, layoutGroups(v.edges))
  // Expanding or collapsing keeps the clicked node where it is; the map is laid
  // out again around it (no overlaps). Other changes keep the first node still.
  const anchor =
    layoutAnchor && positions.value[layoutAnchor]
      ? layoutAnchor
      : ids.find((id) => positions.value[id])
  layoutAnchor = undefined
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

// Positions the user dragged in this session (also saved on the server),
// keyed per direction like the server layout.
const draggedPositions: Record<string, Point> = {}

async function onDragStop(e: NodeDragEvent) {
  if (areaDrag) {
    await finishAreaDrag()
    return
  }
  const moved: Record<string, Point> = {}
  const saved: Record<string, Point> = {}
  for (const n of e.nodes) {
    const p = { x: Math.round(n.position.x), y: Math.round(n.position.y) }
    moved[n.id] = p
    saved[layoutKey(prefs.layoutDirection, n.id)] = p
  }
  Object.assign(draggedPositions, saved)
  positions.value = { ...positions.value, ...moved }
  await Promise.all([api.saveLayout(saved), regroupDropped(Object.keys(moved))])
}

// --- map areas ---

const editingArea = ref<number>()
const areaBusy = ref(false) // dragging or resizing
/** Rectangles held still while something is being dragged or resized. */
const frozen = ref<Record<number, { x: number; y: number; width: number; height: number }>>({})

/** Boxes of the nodes (and group bubbles) laid out on the map. */
const nodeBoxes = computed(
  () =>
    new Map(
      [
        ...view.value.nodes.map((n) => ({ id: n.id, kind: n.kind })),
        ...view.value.groups.map((g) => ({ id: g.id, kind: 'group' })),
      ]
        .filter((b) => positions.value[b.id])
        .map((b) => [b.id, { id: b.id, ...positions.value[b.id]!, ...SIZES[b.kind]! }]),
    ),
)

/** A node in an area brings everything below it (apps, clients, VMs). */
const areaNodes = (a: MapArea) =>
  withDescendants(areaMembers(a, view.value, autoMembers.value), view.value.edges)

/**
 * Areas drawn around their nodes, in both orientations. An area with none of
 * its nodes on the map keeps its stored rectangle, which only makes sense in
 * the orientation it was drawn in.
 */
const visibleAreas = computed(() =>
  liveAreas.value
    .filter((a) => !prefs.collapsedAreas.includes(a.id))
    .filter((a) => {
      const shown = areaNodes(a).some((id) => nodeBoxes.value.has(id))
      // An automatic area is only drawn around its devices.
      return shown || (!a.auto && a.direction === direction.value)
    })
    .map((a) => ({
      ...a,
      ...(frozen.value[a.id] ?? fitArea({ ...a, members: areaNodes(a) }, nodeBoxes.value)),
      ...(areaPreview.value?.id === a.id ? { color: areaPreview.value.color } : {}),
    })),
)

function freezeAreas() {
  frozen.value = Object.fromEntries(
    visibleAreas.value.map((a) => [a.id, { x: a.x, y: a.y, width: a.width, height: a.height }]),
  )
}

function patchArea(id: number, patch: Partial<MapArea>) {
  areas.value = areas.value.map((a) => (a.id === id ? { ...a, ...patch } : a))
}

/** Saves an area with its current rectangle (used while no member is on the map). */
async function saveArea(id: number, patch: Partial<MapArea> = {}) {
  const shown = visibleAreas.value.find((a) => a.id === id)
  const rect = shown
    ? {
        x: Math.round(shown.x),
        y: Math.round(shown.y),
        width: Math.round(shown.width),
        height: Math.round(shown.height),
      }
    : {}
  patchArea(id, { ...rect, ...patch })
  await api.updateArea(id, { ...rect, ...patch })
}

// Moving an area moves its members.
let areaDrag: { id: number; start: Point; members: Record<string, Point> } | undefined

function onDragStart(e: NodeDragEvent) {
  const id = areaIdOf(e.node.id)
  const area = visibleAreas.value.find((a) => a.id === id)
  areaBusy.value = true
  if (!area) {
    freezeAreas() // a device is dragged: areas keep their shape until it is dropped
    return
  }
  const members: Record<string, Point> = {}
  for (const m of areaNodes(area)) if (positions.value[m]) members[m] = { ...positions.value[m]! }
  areaDrag = { id: area.id, start: { x: area.x, y: area.y }, members }
  frozen.value = { [area.id]: { x: area.x, y: area.y, width: area.width, height: area.height } }
}

function onDrag(e: NodeDragEvent) {
  if (!areaDrag) return
  const dx = e.node.position.x - areaDrag.start.x
  const dy = e.node.position.y - areaDrag.start.y
  const next = { ...positions.value }
  for (const [m, p] of Object.entries(areaDrag.members)) next[m] = { x: p.x + dx, y: p.y + dy }
  positions.value = next
  const f = frozen.value[areaDrag.id]!
  frozen.value = { [areaDrag.id]: { ...f, x: e.node.position.x, y: e.node.position.y } }
}

async function finishAreaDrag() {
  const drag = areaDrag!
  areaDrag = undefined
  const moved: Record<string, Point> = {}
  const saved: Record<string, Point> = {}
  for (const m of Object.keys(drag.members)) {
    const p = positions.value[m]!
    moved[m] = { x: Math.round(p.x), y: Math.round(p.y) }
    saved[layoutKey(prefs.layoutDirection, m)] = moved[m]!
  }
  Object.assign(draggedPositions, saved)
  positions.value = { ...positions.value, ...moved }
  try {
    await Promise.all([
      saveArea(drag.id),
      Object.keys(saved).length ? api.saveLayout(saved) : undefined,
    ])
  } finally {
    frozen.value = {}
    areaBusy.value = false
  }
}

/** Dropped devices join the area they were dropped in and leave the one they were dragged out of. */
async function regroupDropped(ids: string[]) {
  const dropped = ids.map((id) => nodeBoxes.value.get(id)).filter((b) => !!b)
  const rects = frozen.value
  const changes: Promise<void>[] = []
  for (const a of areas.value) {
    const rect = rects[a.id]
    if (!rect || a.auto) continue // an automatic area's members are worked out
    const members = regroup(a.members, rect, dropped)
    if (members.join() === a.members.join()) continue
    // The last member left: the area stays where it was.
    changes.push(saveArea(a.id, members.length ? { members } : { members, ...rect }))
  }
  frozen.value = {}
  areaBusy.value = false
  await Promise.all(changes)
}

function resizeArea(id: number, size: { width: number; height: number }) {
  areaBusy.value = true
  const a = visibleAreas.value.find((x) => x.id === id)
  if (!a) return
  frozen.value = { [id]: { x: a.x, y: a.y, ...size } }
}

/** Resizing changes which devices are inside; the area then fits around them. */
async function finishResize(id: number) {
  const rect = frozen.value[id]
  try {
    if (rect) {
      const members = membersOf(rect, [...nodeBoxes.value.values()])
      patchArea(id, { ...rect, members })
      frozen.value = {}
      await api.updateArea(id, { ...rect, members })
    }
  } finally {
    frozen.value = {}
    areaBusy.value = false
  }
}

async function renameArea(id: number, name: string) {
  editingArea.value = undefined
  patchArea(id, { name })
  await api.updateArea(id, { name })
}

async function colorArea(id: number, color: string) {
  patchArea(id, { color })
  await api.updateArea(id, { color })
}

// Automatic areas: one per VLAN and subnet, created as they appear (once:
// removing one only dismisses it). Colors go round a palette.
const creatingAuto = new Set<string>()
watch(autoAreaGroups, async (groups) => {
  if (!data.value) return
  const known = new Set(areas.value.map((a) => a.auto).filter(Boolean))
  let next = areas.value.filter((a) => a.auto).length
  for (const g of groups) {
    if (known.has(g.key) || creatingAuto.has(g.key)) continue
    creatingAuto.add(g.key)
    try {
      const created = await api.createArea({
        name: g.name,
        color: AUTO_AREA_COLORS[next++ % AUTO_AREA_COLORS.length]!,
        direction: prefs.layoutDirection,
        x: 0,
        y: 0,
        width: MIN_AREA_SIZE,
        height: MIN_AREA_SIZE,
        members: [],
        auto: g.key,
      })
      if (!areas.value.some((a) => a.id === created.id)) areas.value = [...areas.value, created]
    } catch {
      // Tried again on the next change.
    } finally {
      creatingAuto.delete(g.key)
    }
  }
})

/** Hides an area (or shows a hidden one again): its frame leaves the map, its devices stay. */
async function hideArea(id: number) {
  const hidden = !areas.value.find((a) => a.id === id)?.hidden
  patchArea(id, { hidden })
  await api.updateArea(id, { hidden })
}

async function deleteArea(id: number) {
  // An automatic area is only dismissed (kept, so it is not created again).
  areas.value = areas.value
    .map((a) => (a.id === id && a.auto ? { ...a, dismissed: true } : a))
    .filter((a) => a.id !== id || a.auto)
  prefs.collapsedAreas = prefs.collapsedAreas.filter((x) => x !== id)
  await api.deleteArea(id)
}

// Drawing a new area: click "New area", then drag a rectangle on the map.
const drawing = ref(false)
const draft = ref<{ start: Point; end: Point }>()
const draftStyle = computed(() => {
  if (!draft.value) return undefined
  const box = mapEl.value?.getBoundingClientRect()
  const r = rectFrom(draft.value.start, draft.value.end)
  return {
    left: `${r.x - (box?.left ?? 0)}px`,
    top: `${r.y - (box?.top ?? 0)}px`,
    width: `${r.width}px`,
    height: `${r.height}px`,
  }
})

function onDrawStart(e: PointerEvent) {
  const p = { x: e.clientX, y: e.clientY }
  draft.value = { start: p, end: p }
  ;(e.target as Element).setPointerCapture?.(e.pointerId)
}

function onDrawMove(e: PointerEvent) {
  if (draft.value) draft.value = { ...draft.value, end: { x: e.clientX, y: e.clientY } }
}

async function onDrawEnd() {
  const d = draft.value
  draft.value = undefined
  if (!d) return
  const r = rectFrom(screenToFlowCoordinate(d.start), screenToFlowCoordinate(d.end))
  const members = membersOf(r, [...nodeBoxes.value.values()])
  // A small rectangle is fine around a device (the area fits around it);
  // an empty one is probably a stray click: keep drawing.
  if (!members.length && (r.width < MIN_AREA_SIZE || r.height < MIN_AREA_SIZE)) return
  drawing.value = false
  const created = await api.createArea({
    name: t('map.areas.defaultName'),
    color: 'blue',
    direction: prefs.layoutDirection,
    x: Math.round(r.x),
    y: Math.round(r.y),
    width: Math.max(MIN_AREA_SIZE, Math.round(r.width)),
    height: Math.max(MIN_AREA_SIZE, Math.round(r.height)),
    members,
  })
  areas.value = [...areas.value, created]
  editingArea.value = created.id // name it right away
}

function onDrawKey(e: KeyboardEvent) {
  if (e.key === 'Escape' && drawing.value) {
    drawing.value = false
    draft.value = undefined
  }
}

// Right click on an area title.
const areaMenu = ref<{ x: number; y: number; id: number }>()
const areaMenuArea = computed(() => areas.value.find((a) => a.id === areaMenu.value?.id))

function collapseArea(id: number) {
  if (!prefs.collapsedAreas.includes(id)) prefs.collapsedAreas.push(id)
}
function expandArea(id: number) {
  prefs.collapsedAreas = prefs.collapsedAreas.filter((x) => x !== id)
  selectedId.value = undefined
}

function previewArea(color?: string) {
  areaPreview.value = color && areaMenu.value ? { id: areaMenu.value.id, color } : undefined
}
function closeAreaMenu() {
  areaMenu.value = undefined
  areaPreview.value = undefined
}

function areaMenuAction(action: 'rename' | 'delete' | 'collapse' | 'hide' | { color: string }) {
  const id = areaMenu.value?.id
  areaMenu.value = undefined
  areaPreview.value = undefined
  if (id === undefined) return
  if (action === 'collapse') collapseArea(id)
  else if (action === 'rename') editingArea.value = id
  else if (action === 'delete') deleteArea(id)
  else if (action === 'hide') hideArea(id)
  else colorArea(id, action.color)
}

function toggleDirection() {
  prefs.layoutDirection = prefs.layoutDirection === 'RIGHT' ? 'DOWN' : 'RIGHT'
}

// A link's traffic over time, shown when the link is clicked.
const linkChart = ref<LinkSeries>()
function onEdgeClick(e: EdgeMouseEvent) {
  const edge = view.value.edges.find((x) => x.id === e.edge.id)
  const series = edge && linkSeries(edge, nodeById.value)
  if (!series || !edge) return
  const name = (id: string) => {
    const n = nodeById.value.get(id)
    return n ? displayName(n, t) : id
  }
  linkChart.value = { ...series, name: `${name(edge.source)} → ${name(edge.target)}` }
}

/**
 * ?node=<id> (from the Insights screen) opens that node's panel and centers
 * it, expanding the group it is folded into.
 */
let focused: string | undefined
function focusRequested() {
  const id = typeof route.query.node === 'string' ? route.query.node : undefined
  if (!id || id === focused || !nodeById.value.has(id)) return
  focused = id
  const node = nodeById.value.get(id)!
  if (node.parent_id && !view.value.nodes.some((n) => n.id === id)) expand(node.parent_id)
  selectedId.value = id
  void router.replace({ query: { ...route.query, node: undefined } })
  setTimeout(() => {
    void fitView({ nodes: [id], maxZoom: 1.1, duration: 400, padding: 0.4 })
  }, 600)
}

function onNodeClick(e: NodeMouseEvent) {
  if (areaIdOf(e.node.id) !== undefined) return
  // A Wi-Fi network has no panel: a click folds or unfolds its clients.
  if (isWifiNetwork(e.node.id)) return toggleChildren(e.node.id)
  selectedId.value = e.node.id
}

async function refresh() {
  refreshing.value = true
  try {
    await api.refresh()
    await new Promise((r) => setTimeout(r, 1500))
    await load()
  } finally {
    refreshing.value = false
  }
}

// Export: the whole map as an image (PNG, SVG) or its data (JSON).
const exportOpen = ref(false)
const exporting = ref(false)
const exportError = ref('')
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

/** The theme the screen shows now ("system" resolved). */
function screenTheme(): 'dark' | 'light' {
  if (prefs.theme !== 'system') return prefs.theme
  return window.matchMedia?.('(prefers-color-scheme: light)').matches ? 'light' : 'dark'
}

/**
 * Exports the map. An image shows every group and area expanded, in the theme
 * and orientation picked: the map is laid out that way for the capture, then
 * put back as it was (theme, orientation, groups, zoom and position).
 */
async function exportMap(options: ExportOptions) {
  exportError.value = ''
  if (!data.value) return
  if (options.format === 'json') {
    download(mapJSON(data.value), exportName('json'))
    exportOpen.value = false
    return
  }
  const viewportEl = mapEl.value?.querySelector<HTMLElement>('.vue-flow__viewport')
  if (!viewportEl) return
  exporting.value = true
  const screen = getViewport()
  const turned = options.direction !== prefs.layoutDirection
  try {
    document.documentElement.dataset.theme = options.theme
    mapEl.value?.classList.add('exporting')
    await untilLaidOut(() => {
      expandAll.value = true
      exportDirection.value = options.direction
    })
    if (options.format === 'drawio')
      download(drawioFile(), exportName('drawio'), 'application/vnd.jgraph.mxfile')
    else {
      const background = getComputedStyle(mapEl.value!).backgroundColor
      download(await mapImage(options.format, viewportEl, background), exportName(options.format))
    }
    exportOpen.value = false
  } catch {
    exportError.value = t('map.exportFailed')
  } finally {
    applyTheme(prefs.theme)
    mapEl.value?.classList.remove('exporting')
    await untilLaidOut(() => {
      expandAll.value = false
      exportDirection.value = undefined
    })
    // Turning the map refits it: the screen goes back to where it was.
    if (turned) await new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r)))
    setViewport(screen)
    exporting.value = false
  }
}

/** The map as laid out now (expanded for the export) as a draw.io diagram. */
function drawioFile(): string {
  const v = view.value
  const labels = linkInfo.value.labels
  const nodes = [
    ...v.nodes.map((n) => ({
      id: n.id,
      label: displayName(n, t),
      sub: n.kind === 'ssid' ? undefined : n.ip,
      kind: n.kind,
      online: n.online,
      ...(positions.value[n.id] ?? { x: 0, y: 0 }),
      ...SIZES[n.kind]!,
    })),
    ...v.groups.map((g) => ({
      id: g.id,
      label: g.area?.name ?? t('map.groupLabel', { n: g.clients.length }),
      kind: 'group',
      ...(positions.value[g.id] ?? { x: 0, y: 0 }),
      ...SIZES.group!,
    })),
  ]
  const edges = v.edges.map((e) => {
    const l = labels.get(e.id)
    const label = l && !l.hidden ? [l.name, formatSpeed(l.speed)].filter(Boolean).join(' | ') : ''
    return {
      id: e.id,
      source: e.source,
      target: e.target,
      label,
      dashed: e.kind !== 'lldp' && e.kind !== 'fdb',
    }
  })
  const areaBoxes = visibleAreas.value.map((a) => ({
    id: String(a.id),
    name: a.name,
    color: a.color,
    x: a.x,
    y: a.y,
    width: a.width,
    height: a.height,
  }))
  return mapDrawio(nodes, edges, areaBoxes, 'Omini')
}

async function resetLayout() {
  await api.resetLayout()
  for (const k of Object.keys(draggedPositions)) delete draggedPositions[k]
  if (data.value) data.value = { ...data.value, layout: {} }
  lastLayout = ''
  await load()
}

async function scanNetwork() {
  await api.createIntegration({
    type: 'network',
    config: { subnets: 'auto', port_scan: true },
  })
  lastLayout = ''
  await load()
}

/**
 * Shows a node's clients. The side panel only follows when the expansion came
 * from it (its "Show all" button); a middle click or the menu leave it alone.
 */
function expand(parentId: string, fromPanel = false) {
  layoutAnchor = parentId
  prefs.collapsed = prefs.collapsed.filter((id) => id !== parentId)
  if (!prefs.expanded.includes(parentId)) prefs.expanded.push(parentId)
  if (fromPanel) selectedId.value = parentId
}

function collapse(parentId: string) {
  layoutAnchor = parentId
  prefs.expanded = prefs.expanded.filter((id) => id !== parentId)
  if (!prefs.collapsed.includes(parentId)) prefs.collapsed.push(parentId)
}

// Context menu (right click on a node).
const menu = ref<{ x: number; y: number; id: string }>()
const mapEl = ref<HTMLElement>()

function onContextMenu(e: NodeMouseEvent) {
  const ev = e.event as MouseEvent
  ev.preventDefault()
  const box = mapEl.value?.getBoundingClientRect()
  const x = ev.clientX - (box?.left ?? 0)
  const y = ev.clientY - (box?.top ?? 0)
  const area = areaIdOf(e.node.id)
  if (area !== undefined) {
    menu.value = undefined
    areaMenu.value = { x, y, id: area }
    return
  }
  areaMenu.value = undefined
  menu.value = { x, y, id: e.node.id }
}

/** The parent whose clients a menu action applies to (a group acts for its parent). */
const menuParent = computed(() => {
  const id = menu.value?.id
  if (!id) return undefined
  return view.value.groups.find((g) => g.id === id)?.parentId ?? id
})
const menuCanCollapse = computed(() => {
  const id = menuParent.value
  // Possible unless everything is already collapsed by the user (a partial
  // automatic group can still be extended to all clients).
  return (
    !!id &&
    !prefs.collapsed.includes(id) &&
    clientCount(id, mapGraph.value.nodes, mapGraph.value.edges) >= 2
  )
})
const menuCanExpand = computed(() => {
  const id = menuParent.value
  return !!id && view.value.groups.some((g) => g.parentId === id)
})

/** Expands the children of a node if they are grouped, groups them otherwise. */
function toggleChildren(id: string) {
  if (id.startsWith('area-bubble:')) return expandArea(Number(id.slice(12)))
  const parent = view.value.groups.find((g) => g.id === id)?.parentId ?? id
  if (view.value.groups.some((g) => g.parentId === parent)) expand(parent)
  // Counted on the map's graph: a Wi-Fi network's clients are its children there.
  else if (clientCount(parent, mapGraph.value.nodes, mapGraph.value.edges) >= 2) collapse(parent)
}

function menuAction(action: 'collapse' | 'expand' | 'details') {
  const id = menuParent.value
  const target = menu.value?.id
  menu.value = undefined
  if (!id) return
  if (action === 'collapse') collapse(id)
  else if (action === 'expand') expand(id)
  else selectedId.value = target
}

/** A deleted device leaves the map right away (it returns if a scan finds it again). */
function removeNode(id: string) {
  selectedId.value = undefined
  if (!data.value) return
  data.value = {
    ...data.value,
    topology: {
      nodes: data.value.topology.nodes.filter((n) => n.id !== id),
      edges: data.value.topology.edges.filter((e) => e.source !== id && e.target !== id),
    },
  }
}

function patchSelected(patch: Partial<TopoNode>) {
  if (!data.value || !selectedId.value) return
  const id = selectedId.value
  data.value = {
    ...data.value,
    topology: {
      ...data.value.topology,
      nodes: data.value.topology.nodes.map((n) => (n.id === id ? { ...n, ...patch } : n)),
    },
  }
}

onMounted(() => {
  load()
  timer = setInterval(load, POLL_MS)
  window.addEventListener('keydown', onDrawKey)
})
onBeforeUnmount(() => {
  clearInterval(timer)
  window.removeEventListener('keydown', onDrawKey)
})
</script>

<template>
  <div ref="mapEl" class="map">
    <div class="toolbar">
      <div class="chips">
        <span class="chip">{{ t('map.devices', summary.devices) }}</span>
        <span class="chip">{{ t('map.clients', summary.clients) }}</span>
        <RouterLink v-if="attention" to="/alerts" class="chip problem" data-test="alerts-chip">
          <Bell :size="12" />
          {{ t('map.alerts', { n: attention }, attention) }}
        </RouterLink>
        <RouterLink v-if="summary.problems" to="/integrations" class="chip problem">
          {{ t('map.problems', summary.problems) }}
        </RouterLink>
        <span v-if="data?.generated_at" class="updated muted">
          {{ t('map.updated', { ago: formatAgo(data.generated_at, locale, now) }) }}
        </span>
      </div>
      <div class="buttons">
        <button
          class="btn small"
          :class="{ active: prefs.hideOffline }"
          data-test="toggle-offline"
          :aria-pressed="prefs.hideOffline"
          :title="prefs.hideOffline ? t('map.showOffline') : t('map.hideOffline')"
          @click="prefs.hideOffline = !prefs.hideOffline"
        >
          <component :is="prefs.hideOffline ? Eye : EyeOff" :size="15" />
          <span class="label">
            {{ prefs.hideOffline ? t('map.showOffline') : t('map.hideOffline') }}
            <template v-if="offlineCount"> ({{ offlineCount }})</template>
          </span>
        </button>
        <button
          v-if="hiddenCount"
          class="btn small"
          :class="{ active: showHidden }"
          data-test="toggle-hidden"
          :aria-pressed="showHidden"
          @click="showHidden = !showHidden"
        >
          <component :is="showHidden ? EyeOff : Eye" :size="15" />
          <span class="label">
            {{ showHidden ? t('map.hideHidden') : t('map.showHidden') }} ({{ hiddenCount }})
          </span>
        </button>
        <button
          class="btn small"
          :title="prefs.layoutDirection === 'RIGHT' ? t('map.topDown') : t('map.leftToRight')"
          data-test="direction"
          @click="toggleDirection"
        >
          <component
            :is="prefs.layoutDirection === 'RIGHT' ? ArrowDownFromLine : ArrowRightFromLine"
            :size="15"
          />
          <span class="label">
            {{ prefs.layoutDirection === 'RIGHT' ? t('map.topDown') : t('map.leftToRight') }}
          </span>
        </button>
        <button
          class="btn small"
          :class="{ active: drawing }"
          data-test="new-area"
          :aria-pressed="drawing"
          :title="t('map.areas.drawHint')"
          @click="drawing = !drawing"
        >
          <SquareDashed :size="15" /><span class="label">{{ t('map.areas.new') }}</span>
        </button>
        <button
          class="btn small"
          data-test="export"
          :disabled="exporting"
          aria-haspopup="dialog"
          @click="exportOpen = true"
        >
          <Download :size="15" /><span class="label">{{ t('map.export') }}</span>
        </button>
        <button class="btn small" :title="t('map.resetLayout')" @click="resetLayout">
          <LayoutGrid :size="15" /><span class="label">{{ t('map.resetLayout') }}</span>
        </button>
        <button class="btn small" :disabled="refreshing" @click="refresh">
          <RefreshCw :size="15" :class="{ spin: refreshing }" />
          <span class="label">{{ t('map.refresh') }}</span>
        </button>
      </div>
    </div>

    <div v-if="empty" class="empty card">
      <h2>{{ t('map.emptyTitle') }}</h2>
      <p class="muted">{{ t('map.emptyHint') }}</p>
      <div class="empty-actions">
        <button class="btn primary" @click="scanNetwork">{{ t('map.scanNetwork') }}</button>
        <button class="btn" @click="router.push('/integrations?add=1')">
          {{ t('map.addIntegration') }}
        </button>
      </div>
    </div>

    <VueFlow
      v-else
      id="omini-map"
      :nodes="flowNodes"
      :edges="flowEdges"
      :min-zoom="0.15"
      :max-zoom="2"
      :nodes-connectable="false"
      :elements-selectable="false"
      :default-edge-options="{ selectable: false }"
      class="flow"
      @node-click="onNodeClick"
      @edge-click="onEdgeClick"
      @node-context-menu="onContextMenu"
      @node-drag-start="onDragStart"
      @node-drag="onDrag"
      @node-drag-stop="onDragStop"
      @pane-click="selectedId = undefined"
    >
      <template #node-omini="nodeProps">
        <TopologyNode
          :data="nodeProps.data"
          :selected="nodeProps.id === selectedId"
          @toggle="toggleChildren(nodeProps.id)"
        />
      </template>
      <template #edge-link="edgeProps">
        <LinkEdge v-bind="edgeProps" />
      </template>
      <template #node-area="nodeProps">
        <AreaNode
          :area="nodeProps.data.area"
          :editing="editingArea === nodeProps.data.area.id"
          :zoom="viewport.zoom"
          @start-rename="editingArea = nodeProps.data.area.id"
          @rename="(name) => renameArea(nodeProps.data.area.id, name)"
          @cancel-rename="editingArea = undefined"
          @resize="(size) => resizeArea(nodeProps.data.area.id, size)"
          @resize-end="finishResize(nodeProps.data.area.id)"
        />
      </template>
      <Background :gap="22" :size="1.2" pattern-color="var(--border)" />
      <Controls position="bottom-left" :show-interactive="false" />
    </VueFlow>

    <div
      v-if="drawing && !empty"
      class="draw-layer"
      data-test="draw-layer"
      @pointerdown.prevent="onDrawStart"
      @pointermove="onDrawMove"
      @pointerup="onDrawEnd"
    >
      <p class="draw-hint chip">{{ t('map.areas.drawHint') }}</p>
      <div v-if="draftStyle" class="draft" :style="draftStyle" />
    </div>

    <AreaMenu
      v-if="areaMenu && areaMenuArea"
      :x="areaMenu.x"
      :y="areaMenu.y"
      :color="areaMenuArea.color"
      :is-hidden="!!areaMenuArea.hidden"
      @rename="areaMenuAction('rename')"
      @collapse="areaMenuAction('collapse')"
      @hide="areaMenuAction('hide')"
      @color="(color: string) => areaMenuAction({ color })"
      @delete="areaMenuAction('delete')"
      @preview="previewArea"
      @close="closeAreaMenu"
    />

    <NodeMenu
      v-if="menu"
      :x="menu.x"
      :y="menu.y"
      :can-collapse="menuCanCollapse"
      :can-expand="menuCanExpand"
      :can-details="!isWifiNetwork(menu.id)"
      @collapse="menuAction('collapse')"
      @expand="menuAction('expand')"
      @details="menuAction('details')"
      @close="menu = undefined"
    />

    <ModalDialog
      v-if="linkChart"
      :title="t('traffic.link', { name: linkChart.name })"
      wide
      @close="linkChart = undefined"
    >
      <TrafficChart :node="linkChart.node" :iface="linkChart.iface" :swap="linkChart.swap" />
    </ModalDialog>

    <ExportDialog
      v-if="exportOpen"
      :theme="screenTheme()"
      :direction="prefs.layoutDirection"
      :busy="exporting"
      :error="exportError"
      @close="exportOpen = false"
      @export="exportMap"
    />

    <NodePanel
      v-if="selectedNode || selectedGroup"
      :node="selectedNode"
      :group="selectedGroup"
      :nodes="nodes"
      :edges="edges"
      :integration="selectedIntegration"
      :can-scan="integrations.some((i) => i.type === 'nmap' && i.enabled)"
      @close="selectedId = undefined"
      @select="(id) => (selectedId = id)"
      @changed="patchSelected"
      @expand="
        (id: string) =>
          id.startsWith('area-bubble:') ? expandArea(Number(id.slice(12))) : expand(id, true)
      "
      @deleted="removeNode"
      @scanned="load"
    />
  </div>
</template>

<style scoped>
.map {
  position: relative;
  height: 100%;
  background: var(--bg);
}
.flow {
  height: 100%;
}
.toolbar {
  position: absolute;
  top: 12px;
  left: 12px;
  right: 12px;
  z-index: 4;
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
  pointer-events: none;
  flex-wrap: wrap;
}
.toolbar > * {
  pointer-events: auto;
}
.chips,
.buttons {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
}
.chip {
  padding: 5px 12px;
  border: 1px solid var(--border);
  border-radius: 999px;
  background: var(--bg-elevated);
  font-size: 13px;
  font-weight: 500;
  text-decoration: none;
  color: var(--text);
}
.chip.problem {
  background: var(--danger-soft);
  border-color: transparent;
  color: var(--danger);
}
.updated {
  font-size: 12.5px;
  margin-left: 4px;
}
.spin {
  animation: spin 0.8s linear infinite;
}
.btn.active {
  background: var(--accent-soft);
  border-color: var(--accent);
  color: var(--accent);
}
@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}
.empty {
  position: absolute;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
  width: min(420px, calc(100% - 32px));
  padding: 28px;
  text-align: center;
}
.empty h2 {
  font-size: 18px;
}
.empty p {
  margin: 8px 0 18px;
}
.empty-actions {
  display: flex;
  justify-content: center;
  gap: 8px;
  flex-wrap: wrap;
}

.draw-layer {
  position: absolute;
  inset: 0;
  z-index: 3;
  cursor: crosshair;
}
.draw-hint {
  position: absolute;
  bottom: 16px;
  left: 50%;
  transform: translateX(-50%);
  margin: 0;
  pointer-events: none;
}
.draft {
  position: absolute;
  border: 1.5px dashed var(--accent);
  border-radius: var(--radius);
  background: var(--accent-soft);
}

/* Vue Flow theming */
.flow :deep(.vue-flow__edge-path) {
  stroke: var(--edge);
}
.flow :deep(.vue-flow__edge.slow .vue-flow__edge-path) {
  stroke: var(--warn);
}
.flow :deep(.vue-flow__edge.offline .vue-flow__edge-path) {
  opacity: 0.35;
}
.flow :deep(.vue-flow__edge-textbg) {
  fill: var(--bg-elevated);
}
.flow :deep(.vue-flow__edge-text) {
  fill: var(--text-muted);
  font-size: 11px;
  font-family: var(--mono);
}
.flow :deep(.vue-flow__node) {
  padding: 0;
  border: 0;
  background: transparent;
  box-shadow: none;
}
/* Only the title and the resize corner of an area take the mouse. */
.flow :deep(.vue-flow__node.area-node) {
  pointer-events: none;
}
.flow :deep(.vue-flow__controls) {
  box-shadow: var(--shadow);
  border-radius: var(--radius-sm);
  overflow: hidden;
}
.flow :deep(.vue-flow__controls-button) {
  background: var(--bg-elevated);
  border-color: var(--border);
  fill: var(--text);
}

@media (max-width: 640px) {
  .label {
    display: none;
  }
}
</style>
