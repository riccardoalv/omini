<script setup lang="ts">
import { Background } from '@vue-flow/background'
import { Controls } from '@vue-flow/controls'
import type { Edge, Node, NodeDragEvent, NodeMouseEvent } from '@vue-flow/core'
import { VueFlow, useVueFlow } from '@vue-flow/core'
import {
  ArrowDownFromLine,
  ArrowRightFromLine,
  Eye,
  EyeOff,
  LayoutGrid,
  RefreshCw,
  SquareDashed,
} from 'lucide-vue-next'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'

import AreaMenu from '@/components/map/AreaMenu.vue'
import AreaNode from '@/components/map/AreaNode.vue'
import NodeMenu from '@/components/map/NodeMenu.vue'
import NodePanel from '@/components/map/NodePanel.vue'
import TopologyNode, { type NodeData } from '@/components/map/TopologyNode.vue'
import { api } from '@/lib/api'
import { areaIdOf, areaNodeId, MIN_AREA_SIZE, membersOf, rectFrom } from '@/lib/areas'
import { formatAgo, formatSpeed } from '@/lib/format'
import { clientCount, collapseClients, edgeLook, withoutOffline } from '@/lib/graph'
import { alignOn, layout, layoutKey, positionsFor } from '@/lib/layout'
import { prefs } from '@/lib/prefs'
import type {
  AreaColor,
  Integration,
  MapArea,
  Point,
  TopoNode,
  TopologyResponse,
} from '@/lib/types'

const POLL_MS = 10_000
const SIZES: Record<string, { width: number; height: number }> = {
  device: { width: 220, height: 60 },
  unmanaged: { width: 200, height: 56 },
  segment: { width: 240, height: 56 },
  client: { width: 200, height: 38 },
  app: { width: 180, height: 34 },
  group: { width: 150, height: 44 },
}

const { t, locale } = useI18n()
const router = useRouter()
const { fitView, onNodesInitialized, updateNodeInternals, viewport, screenToFlowCoordinate } =
  useVueFlow('omini-map')

const data = shallowRef<TopologyResponse>()
const integrations = ref<Integration[]>([])
const loaded = ref(false)
const positions = ref<Record<string, Point>>({})
const selectedId = ref<string>()
const refreshing = ref(false)
const now = ref(Date.now())
let lastLayout = '' // signature of the last laid out graph
let layoutAnchor: string | undefined // node the user expanded or collapsed
let timer: ReturnType<typeof setInterval> | undefined

const allNodes = computed(() => data.value?.topology.nodes ?? [])
const allEdges = computed(() => data.value?.topology.edges ?? [])
const filtered = computed(() =>
  prefs.hideOffline
    ? withoutOffline(allNodes.value, allEdges.value)
    : { nodes: allNodes.value, edges: allEdges.value, hidden: 0 },
)
const nodes = computed(() => filtered.value.nodes)
const edges = computed(() => filtered.value.edges)
const offlineCount = computed(() => allNodes.value.filter((n) => !n.online).length)
const expanded = computed(() => new Set(prefs.expanded))
const forced = computed(() => new Set(prefs.collapsed))
const view = computed(() =>
  collapseClients(nodes.value, edges.value, prefs.collapseThreshold, expanded.value, forced.value),
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
const empty = computed(() => loaded.value && nodes.value.length === 0)

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
      direction: prefs.layoutDirection,
    },
    width: SIZES[n.kind]!.width,
    height: SIZES[n.kind]!.height,
  }))
  out.push(...devices)
  for (const g of view.value.groups) {
    out.push({
      id: g.id,
      type: 'omini',
      position: positions.value[g.id] ?? { x: 0, y: 0 },
      data: { group: g, direction: prefs.layoutDirection },
      width: SIZES.group!.width,
      height: SIZES.group!.height,
    })
  }
  return out
})

const flowEdges = computed<Edge[]>(() => {
  const byId = new Map(nodes.value.map((n) => [n.id, n]))
  return view.value.edges.map((e) => {
    const source = byId.get(e.source)
    const target = byId.get(e.target)
    const look = edgeLook(e, source, target)
    const speed = target?.kind !== 'client' ? formatSpeed(e.speed_mbps) : ''
    return {
      id: e.id,
      source: e.source,
      target: e.target,
      type: 'smoothstep',
      label: speed || undefined,
      class: { slow: look.slow, offline: target ? !target.online : false },
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
    // Never overwrite an area while the user is moving, resizing or renaming it.
    if (!areaBusy.value && editingArea.value === undefined) areas.value = topo.areas ?? []
  } finally {
    loaded.value = true
    now.value = Date.now()
  }
}

// Re-layout only when the visible graph or the direction changes, not on every poll.
watch([view, () => prefs.layoutDirection], async ([v, direction]) => {
  const ids = [...v.nodes.map((n) => n.id), ...v.groups.map((g) => g.id)]
  const key = direction + '|' + ids.join(',') + '|' + v.edges.map((e) => e.id).join(',')
  if (key === lastLayout) return
  const refit = lastLayout === '' || !lastLayout.startsWith(direction + '|')
  lastLayout = key
  const saved = positionsFor({ ...data.value?.layout, ...draggedPositions }, direction)
  const fresh = await layout(
    [
      ...v.nodes.map((n) => ({ id: n.id, ...SIZES[n.kind]! })),
      ...v.groups.map((g) => ({ id: g.id, ...SIZES.group! })),
    ],
    v.edges,
    saved,
    direction,
  )
  // Expanding or collapsing keeps the clicked node where it is; the map is laid
  // out again around it (no overlaps). Other changes keep the first node still.
  const anchor =
    layoutAnchor && positions.value[layoutAnchor]
      ? layoutAnchor
      : ids.find((id) => positions.value[id])
  layoutAnchor = undefined
  positions.value = refit
    ? fresh
    : alignOn(positions.value, fresh, anchor, new Set(Object.keys(saved)))
  if (refit) {
    // Handles moved (left/right vs top/bottom): Vue Flow must re-measure them.
    await nextTick()
    updateNodeInternals()
    fitSoon()
  }
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
    await finishAreaDrag(e)
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
  await api.saveLayout(saved)
}

// --- map areas ---

const areas = ref<MapArea[]>([])
const visibleAreas = computed(() =>
  areas.value.filter((a) => a.direction === prefs.layoutDirection),
)
const editingArea = ref<number>()
const areaBusy = ref(false) // dragging or resizing

/** Boxes of everything laid out on the map, to find the members of an area. */
function boxes() {
  return [
    ...view.value.nodes.map((n) => ({ id: n.id, kind: n.kind })),
    ...view.value.groups.map((g) => ({ id: g.id, kind: 'group' })),
  ]
    .filter((b) => positions.value[b.id])
    .map((b) => ({ id: b.id, ...positions.value[b.id]!, ...SIZES[b.kind]! }))
}

// Moving an area moves the nodes inside it.
let areaDrag: { id: number; start: Point; members: Record<string, Point> } | undefined

function onDragStart(e: NodeDragEvent) {
  const id = areaIdOf(e.node.id)
  const area = areas.value.find((a) => a.id === id)
  if (!area) return
  areaBusy.value = true
  const members: Record<string, Point> = {}
  for (const m of membersOf(area, boxes())) members[m] = { ...positions.value[m]! }
  areaDrag = { id: area.id, start: { x: area.x, y: area.y }, members }
}

function onDrag(e: NodeDragEvent) {
  if (!areaDrag) return
  const dx = e.node.position.x - areaDrag.start.x
  const dy = e.node.position.y - areaDrag.start.y
  const next = { ...positions.value }
  for (const [m, p] of Object.entries(areaDrag.members)) next[m] = { x: p.x + dx, y: p.y + dy }
  positions.value = next
  // The node list is rebuilt from the areas: move the area there too, or it snaps back.
  patchArea(areaDrag.id, { x: e.node.position.x, y: e.node.position.y })
}

async function finishAreaDrag(e: NodeDragEvent) {
  const drag = areaDrag!
  areaDrag = undefined
  const x = Math.round(e.node.position.x)
  const y = Math.round(e.node.position.y)
  patchArea(drag.id, { x, y })
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
      api.updateArea(drag.id, { x, y }),
      Object.keys(saved).length ? api.saveLayout(saved) : undefined,
    ])
  } finally {
    areaBusy.value = false
  }
}

function patchArea(id: number, patch: Partial<MapArea>) {
  areas.value = areas.value.map((a) => (a.id === id ? { ...a, ...patch } : a))
}

function resizeArea(id: number, size: { width: number; height: number }) {
  areaBusy.value = true
  patchArea(id, size)
}

async function finishResize(id: number) {
  const a = areas.value.find((x) => x.id === id)
  try {
    if (a) await api.updateArea(id, { width: a.width, height: a.height })
  } finally {
    areaBusy.value = false
  }
}

async function renameArea(id: number, name: string) {
  editingArea.value = undefined
  patchArea(id, { name })
  await api.updateArea(id, { name })
}

async function colorArea(id: number, color: AreaColor) {
  patchArea(id, { color })
  await api.updateArea(id, { color })
}

async function deleteArea(id: number) {
  areas.value = areas.value.filter((a) => a.id !== id)
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
  drawing.value = false
  if (!d) return
  const a = screenToFlowCoordinate(d.start)
  const b = screenToFlowCoordinate(d.end)
  const r = rectFrom(a, b)
  if (r.width < MIN_AREA_SIZE || r.height < MIN_AREA_SIZE) return
  const created = await api.createArea({
    name: t('map.areas.defaultName'),
    color: 'blue',
    direction: prefs.layoutDirection,
    x: Math.round(r.x),
    y: Math.round(r.y),
    width: Math.round(r.width),
    height: Math.round(r.height),
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

function areaMenuAction(action: 'rename' | 'delete' | AreaColor) {
  const id = areaMenu.value?.id
  areaMenu.value = undefined
  if (id === undefined) return
  if (action === 'rename') editingArea.value = id
  else if (action === 'delete') deleteArea(id)
  else colorArea(id, action)
}

function toggleDirection() {
  prefs.layoutDirection = prefs.layoutDirection === 'RIGHT' ? 'DOWN' : 'RIGHT'
}

function onNodeClick(e: NodeMouseEvent) {
  if (areaIdOf(e.node.id) !== undefined) return
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

async function resetLayout() {
  await api.resetLayout()
  for (const k of Object.keys(draggedPositions)) delete draggedPositions[k]
  if (data.value) data.value = { ...data.value, layout: {} }
  lastLayout = ''
  await load()
}

async function scanNetwork() {
  await api.createIntegration({
    name: 'Network scan',
    type: 'network',
    config: { subnets: 'auto', port_scan: true },
  })
  lastLayout = ''
  await load()
}

function expand(parentId: string) {
  layoutAnchor = parentId
  prefs.collapsed = prefs.collapsed.filter((id) => id !== parentId)
  if (!prefs.expanded.includes(parentId)) prefs.expanded.push(parentId)
  selectedId.value = parentId
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
  return !!id && !prefs.collapsed.includes(id) && clientCount(id, nodes.value, edges.value) >= 2
})
const menuCanExpand = computed(() => {
  const id = menuParent.value
  return !!id && view.value.groups.some((g) => g.parentId === id)
})

/** Expands the children of a node if they are grouped, groups them otherwise. */
function toggleChildren(id: string) {
  const parent = view.value.groups.find((g) => g.id === id)?.parentId ?? id
  if (view.value.groups.some((g) => g.parentId === parent)) expand(parent)
  else if (clientCount(parent, nodes.value, edges.value) >= 2) collapse(parent)
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
      @rename="areaMenuAction('rename')"
      @color="areaMenuAction"
      @delete="areaMenuAction('delete')"
      @close="areaMenu = undefined"
    />

    <NodeMenu
      v-if="menu"
      :x="menu.x"
      :y="menu.y"
      :can-collapse="menuCanCollapse"
      :can-expand="menuCanExpand"
      @collapse="menuAction('collapse')"
      @expand="menuAction('expand')"
      @details="menuAction('details')"
      @close="menu = undefined"
    />

    <NodePanel
      v-if="selectedNode || selectedGroup"
      :node="selectedNode"
      :group="selectedGroup"
      :nodes="nodes"
      :edges="edges"
      :integration="selectedIntegration"
      :expanded-parent="!!selectedNode && prefs.expanded.includes(selectedNode.id)"
      @close="selectedId = undefined"
      @select="(id) => (selectedId = id)"
      @changed="patchSelected"
      @expand="expand"
      @collapse="collapse"
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
