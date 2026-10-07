<script setup lang="ts">
import { Background } from '@vue-flow/background'
import { Controls } from '@vue-flow/controls'
import type { Edge, Node, NodeDragEvent, NodeMouseEvent } from '@vue-flow/core'
import { VueFlow, useVueFlow } from '@vue-flow/core'
import { ArrowDownFromLine, ArrowRightFromLine, LayoutGrid, RefreshCw } from 'lucide-vue-next'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'

import NodePanel from '@/components/map/NodePanel.vue'
import TopologyNode, { type NodeData } from '@/components/map/TopologyNode.vue'
import { api } from '@/lib/api'
import { formatAgo, formatSpeed } from '@/lib/format'
import { collapseClients, edgeLook } from '@/lib/graph'
import { layout, layoutKey, positionsFor } from '@/lib/layout'
import { prefs } from '@/lib/prefs'
import type { Integration, Point, TopoNode, TopologyResponse } from '@/lib/types'

const POLL_MS = 10_000
const SIZES: Record<string, { width: number; height: number }> = {
  device: { width: 220, height: 60 },
  unmanaged: { width: 200, height: 56 },
  segment: { width: 240, height: 56 },
  client: { width: 170, height: 38 },
  group: { width: 150, height: 44 },
}

const { t, locale } = useI18n()
const router = useRouter()
const { fitView, onNodesInitialized, updateNodeInternals } = useVueFlow('omini-map')

const data = shallowRef<TopologyResponse>()
const integrations = ref<Integration[]>([])
const loaded = ref(false)
const positions = ref<Record<string, Point>>({})
const selectedId = ref<string>()
const refreshing = ref(false)
const now = ref(Date.now())
let lastLayout = '' // signature of the last laid out graph
let timer: ReturnType<typeof setInterval> | undefined

const nodes = computed(() => data.value?.topology.nodes ?? [])
const edges = computed(() => data.value?.topology.edges ?? [])
const expanded = computed(() => new Set(prefs.expanded))
const view = computed(() =>
  collapseClients(nodes.value, edges.value, prefs.collapseThreshold, expanded.value),
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

const flowNodes = computed<Node<NodeData>[]>(() => {
  const out: Node<NodeData>[] = view.value.nodes.map((n) => ({
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
  positions.value = await layout(
    [
      ...v.nodes.map((n) => ({ id: n.id, ...SIZES[n.kind]! })),
      ...v.groups.map((g) => ({ id: g.id, ...SIZES.group! })),
    ],
    v.edges,
    saved,
    direction,
  )
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
onNodesInitialized(fitSoon)

// Positions the user dragged in this session (also saved on the server),
// keyed per direction like the server layout.
const draggedPositions: Record<string, Point> = {}

async function onDragStop(e: NodeDragEvent) {
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

function toggleDirection() {
  prefs.layoutDirection = prefs.layoutDirection === 'RIGHT' ? 'DOWN' : 'RIGHT'
}

function onNodeClick(e: NodeMouseEvent) {
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

async function loadDemo() {
  await api.createIntegration({ name: 'Demo network', type: 'demo', config: {} })
  await new Promise((r) => setTimeout(r, 800))
  lastLayout = ''
  await load()
}

function expand(parentId: string) {
  if (!prefs.expanded.includes(parentId)) prefs.expanded.push(parentId)
  selectedId.value = parentId
}

function collapse(parentId: string) {
  prefs.expanded = prefs.expanded.filter((id) => id !== parentId)
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
})
onBeforeUnmount(() => clearInterval(timer))
</script>

<template>
  <div class="map">
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
        <button class="btn" @click="loadDemo">{{ t('map.loadDemo') }}</button>
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
      @node-drag-stop="onDragStop"
      @pane-click="selectedId = undefined"
    >
      <template #node-omini="nodeProps">
        <TopologyNode :data="nodeProps.data" :selected="nodeProps.id === selectedId" />
      </template>
      <Background :gap="22" :size="1.2" pattern-color="var(--border)" />
      <Controls position="bottom-left" :show-interactive="false" />
    </VueFlow>

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
