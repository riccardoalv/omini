<script setup lang="ts">
import { Background } from '@vue-flow/background'
import { Controls } from '@vue-flow/controls'
import type { EdgeMouseEvent, NodeMouseEvent } from '@vue-flow/core'
import { VueFlow, useVueFlow } from '@vue-flow/core'
import { computed, defineAsyncComponent, onBeforeUnmount, onMounted, ref, shallowRef } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'

import ModalDialog from '@/components/ModalDialog.vue'
import TrafficChart from '@/components/TrafficChart.vue'
import AreaMenu from '@/components/map/AreaMenu.vue'
import AreaNode from '@/components/map/AreaNode.vue'
import DevicesDrawer from '@/components/map/DevicesDrawer.vue'
import DrawLayer from '@/components/map/DrawLayer.vue'
import LinkEdge from '@/components/map/LinkEdge.vue'
import MapToolbar from '@/components/map/MapToolbar.vue'
import NetFrame from '@/components/map/NetFrame.vue'
import NodeMenu from '@/components/map/NodeMenu.vue'
import NodePanel from '@/components/map/NodePanel.vue'
import TopologyNode from '@/components/map/TopologyNode.vue'
import { useFlowElements } from '@/composables/useFlowElements'
import { useFolding } from '@/composables/useFolding'
import { useMapAreas } from '@/composables/useMapAreas'
import { screenTheme, useMapExport } from '@/composables/useMapExport'
import { useMapFocus } from '@/composables/useMapFocus'
import { useMapGraph } from '@/composables/useMapGraph'
import { useMapLayout } from '@/composables/useMapLayout'
import { useNetworkFilter } from '@/composables/useNetworkFilter'
import { alertsState, attentionCount } from '@/lib/alerts'
import { api } from '@/lib/api'
import { areaIdOf } from '@/lib/areas'
import { isWifiNetwork } from '@/lib/graph'
import { displayName } from '@/lib/names'
import { prefs } from '@/lib/prefs'
import { linkSeries, type LinkSeries } from '@/lib/traffic'
import type { Integration, MapArea, TopoNode, TopologyResponse } from '@/lib/types'

// The export dialog (and what builds the files) is loaded when it is opened.
const ExportDialog = defineAsyncComponent(() => import('@/components/map/ExportDialog.vue'))

const POLL_MS = 10_000

const { t } = useI18n()
const router = useRouter()
const { viewport, screenToFlowCoordinate } = useVueFlow('omini-map')

const data = shallowRef<TopologyResponse>()
const integrations = ref<Integration[]>([])
const loaded = ref(false)
const selectedId = ref<string>()
const refreshing = ref(false)
const now = ref(Date.now())
let timer: ReturnType<typeof setInterval> | undefined
const mapEl = ref<HTMLElement>()
// Map areas: declared before the view, which collapses them.
const areas = ref<MapArea[]>([])

const {
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
} = useMapGraph(data, areas)
const {
  takeAnchor,
  expand,
  collapseArea,
  expandArea,
  toggleChildren,
  menu,
  menuCanCollapse,
  menuCanExpand,
  menuAction,
} = useFolding(view, mapGraph, selectedId)
const { positions, nodeBoxes, keep, relayout, forget, untilLaidOut } = useMapLayout({
  data,
  view,
  direction,
  liveAreas,
  onMap,
  takeAnchor,
})
const {
  editingArea,
  areaBusy,
  visibleAreas,
  insetOf,
  onDragStart,
  onDrag,
  onDragStop,
  resizeArea,
  finishResize,
  renameArea,
  drawing,
  drawArea,
  areaMenu,
  areaMenuArea,
  previewArea,
  closeAreaMenu,
  areaMenuAction,
} = useMapAreas({
  areas,
  liveAreas,
  view,
  direction,
  positions,
  nodeBoxes,
  onMap,
  keep,
  collapseArea,
  toMap: screenToFlowCoordinate,
})
const { networkGroups, netFilter, inNet, netFrames } = useNetworkFilter(mapGraph, view, nodeBoxes)
const { flowNodes, flowEdges, linkLabels } = useFlowElements({
  view,
  nodes,
  nodeById,
  positions,
  direction,
  visibleAreas,
  netFrames,
  inNet,
  failedIntegrations,
  insetOf,
})
const { devicesOpen, showDevice, focusRequested } = useMapFocus({
  nodeById,
  view,
  selectedId,
  expand,
})
const { exportOpen, exporting, exportError, exportMap } = useMapExport({
  data,
  view,
  positions,
  visibleAreas,
  linkLabels,
  mapEl,
  expandAll,
  exportDirection,
  untilLaidOut,
})

// "Empty" means no devices at all: hidden or collapsed devices do not count.
const empty = computed(() => loaded.value && allNodes.value.length === 0)
const attention = computed(() => attentionCount(alertsState.list))

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

function onDrawKey(e: KeyboardEvent) {
  if (e.key === 'Escape' && drawing.value) drawing.value = false
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

function onNodeClick(e: NodeMouseEvent) {
  if (areaIdOf(e.node.id) !== undefined) return
  // A Wi-Fi network has no panel: a click folds or unfolds its clients.
  if (isWifiNetwork(e.node.id)) return toggleChildren(e.node.id)
  selectedId.value = e.node.id
}

// Context menu (right click): an area's on its title, else the node's.
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
  forget()
  if (data.value) data.value = { ...data.value, layout: {} }
  await load()
}

async function scanNetwork() {
  await api.createIntegration({
    type: 'network',
    config: { subnets: 'auto', port_scan: true },
  })
  relayout()
  await load()
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
    <DevicesDrawer
      v-if="devicesOpen"
      @close="devicesOpen = false"
      @select="showDevice"
      @changed="load"
    />
    <MapToolbar
      v-model:devices-open="devicesOpen"
      v-model:net-filter="netFilter"
      v-model:show-hidden="showHidden"
      v-model:drawing="drawing"
      :summary="summary"
      :attention="attention"
      :generated-at="data?.generated_at"
      :now="now"
      :offline-count="offlineCount"
      :network-groups="networkGroups"
      :hidden-count="hiddenCount"
      :exporting="exporting"
      :refreshing="refreshing"
      @export="exportOpen = true"
      @reset-layout="resetLayout"
      @refresh="refresh"
    />

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
      <template #node-netframe="nodeProps">
        <NetFrame :name="nodeProps.data.name" />
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

    <DrawLayer v-if="drawing && !empty" @draw="drawArea" />

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

/* Vue Flow theming */
.flow :deep(.vue-flow__edge-path) {
  stroke: var(--edge);
}
.flow :deep(.vue-flow__node.net-dim),
.flow :deep(.vue-flow__edge.net-dim) {
  opacity: 0.15;
}
.flow :deep(.vue-flow__node.net-frame-node) {
  pointer-events: none;
}
.flow :deep(.vue-flow__edge.vpn .vue-flow__edge-path) {
  stroke: #a371f7;
  stroke-dasharray: 8 5;
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
</style>
