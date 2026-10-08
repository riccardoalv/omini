<script setup lang="ts">
import {
  ArrowDown,
  ArrowUp,
  Check,
  Eye,
  EyeOff,
  ExternalLink,
  Pencil,
  Pin,
  PinOff,
  Radar,
  Trash2,
  X,
} from 'lucide-vue-next'
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import DeviceIcon from '@/components/DeviceIcon.vue'
import NodeIcon from '@/components/NodeIcon.vue'
import TrafficChart from '@/components/TrafficChart.vue'
import DeviceExtras from '@/components/map/DeviceExtras.vue'
import PortPanel from '@/components/map/PortPanel.vue'
import ResourceBars from '@/components/map/ResourceBars.vue'
import { alertsState, alertText, sortAlerts } from '@/lib/alerts'
import { api, ApiError } from '@/lib/api'
import { ui } from '@/lib/ui'
import { formatAgo, formatBytes, formatSpeed, formatUptime } from '@/lib/format'
import type { ClientGroup } from '@/lib/graph'
import { childrenOf, linkOnPort } from '@/lib/graph'
import { deviceTypes, logos, productSlugs, slugName } from '@/lib/icons'
import { displayName } from '@/lib/names'
import { parseReasons } from '@/lib/reasons'
import { formatRate, nodeFlow } from '@/lib/traffic'
import { serviceName } from '@/lib/services'
import type {
  Conversation,
  Integration,
  PresenceEvent,
  TopoEdge,
  TopoNode,
  WebService,
} from '@/lib/types'

const props = defineProps<{
  node?: TopoNode
  group?: ClientGroup
  nodes: TopoNode[]
  edges: TopoEdge[]
  integration?: Integration
  /** The nmap integration is added: devices can be scanned on demand. */
  canScan?: boolean
}>()
const emit = defineEmits<{
  close: []
  changed: [patch: Partial<TopoNode>]
  select: [id: string]
  expand: [parentId: string]
  deleted: [id: string]
  scanned: []
}>()
const { t, locale } = useI18n()

const editing = ref(false)
const alias = ref('')
const error = ref('')
const web = ref<WebService[]>([])
const detectingWeb = ref(false)
const webMenu = ref(false)

// Look for a web interface (admin page, NAS, hypervisor...) when a node with an IP is shown.
async function detectWeb(id: string | undefined, ip: string | undefined) {
  web.value = []
  webMenu.value = false
  // An app node knows its own address.
  const own = props.node?.kind === 'app' ? props.node.web : undefined
  if (own?.length) {
    web.value = own.map((w) => ({ url: w.url, port: w.port, title: w.title }))
    return
  }
  if (!id || !ip) return
  detectingWeb.value = true
  try {
    const found = await api.webServices(id)
    if (props.node?.id === id) web.value = found
  } catch {
    // detection is best effort
  } finally {
    if (props.node?.id === id) detectingWeb.value = false
  }
}

// The node's recent comings and goings.
const activity = ref<PresenceEvent[]>([])
async function loadActivity(id: string | undefined) {
  activity.value = []
  if (
    !id ||
    props.node?.kind === 'ssid' ||
    props.node?.kind === 'app' ||
    props.node?.kind === 'wan'
  )
    return
  try {
    const got = await api.presence({ node: id, limit: 8 })
    if (props.node?.id === id) activity.value = got
  } catch {
    // best effort
  }
}

// Who the node talks to (flow exports), when Omini receives them.
const talks = ref<Conversation[]>([])
async function loadTalks(id: string | undefined) {
  talks.value = []
  if (!id || props.node?.kind === 'ssid' || props.node?.kind === 'app') return
  try {
    const res = await api.flows({ node: id, minutes: 60, limit: 5 })
    if (props.node?.id === id) talks.value = res.conversations
  } catch {
    // flows are optional
  }
}
const peerOf = (c: Conversation) => {
  const mine = c.a_node === n.value?.id
  const id = mine ? c.b_node : c.a_node
  const ip = mine ? c.b : c.a
  const peer = id ? byId.value.get(id) : undefined
  return { id, label: peer ? displayName(peer, t) : ip }
}

watch(
  () => props.node?.id,
  (id) => {
    editing.value = false
    error.value = ''
    detectWeb(id, props.node?.ip)
    loadActivity(id)
    loadTalks(id)
  },
  { immediate: true },
)

/** Open alerts about this node. */
const nodeAlerts = computed(() =>
  sortAlerts(alertsState.list.filter((a) => a.node_id === props.node?.id && !a.resolved_at)),
)
/** Whose history the panel charts: a Wi-Fi client's own traffic, or a WAN's uplink. */
const chart = computed(() => {
  const node = n.value
  if (!node) return undefined
  if (node.flow) return { node: node.id, iface: '' }
  if (node.kind === 'wan' && node.wan) {
    const owner = node.id.slice(4, node.id.length - node.wan.interface.length - 1)
    return { node: owner, iface: node.wan.interface }
  }
  return undefined
})
const activityTime = computed(
  () => new Intl.DateTimeFormat(locale.value, { dateStyle: 'short', timeStyle: 'short' }),
)

const n = computed(() => props.node)
const byId = computed(() => new Map(props.nodes.map((x) => [x.id, x])))
const parent = computed(() => (n.value?.parent_id ? byId.value.get(n.value.parent_id) : undefined))
// A router or firewall running as a VM: the host it runs on (no link on the map).
const runsOn = computed(() => (n.value?.runs_on ? byId.value.get(n.value.runs_on) : undefined))
const children = computed(() =>
  n.value
    ? childrenOf(n.value.id, props.nodes, props.edges).filter((c) => c.kind === 'client')
    : [],
)
/** Ports with a jack: what the front view draws (VLANs and tunnels are listed only). */
const physicalPorts = computed(() =>
  ports.value.filter((i) => !i.type || i.type === 'ethernet' || i.type === 'lag'),
)
const portLinks = computed(() => {
  const id = n.value?.id
  const out: Record<string, TopoNode | undefined> = {}
  if (!id) return out
  for (const p of physicalPorts.value)
    out[p.name] = linkOnPort(id, p.name, props.nodes, props.edges)
  return out
})
const ports = computed(() =>
  (n.value?.device?.interfaces ?? []).filter((i) => i.type !== 'loopback'),
)

// Naming a port (from its card in the front view).
async function savePort(port: string, label: string) {
  const node = n.value
  if (!node) return
  try {
    await api.setPortLabel(node.id, port, label)
    const labels = { ...node.port_labels }
    if (label) labels[port] = label
    else delete labels[port]
    emit('changed', { port_labels: labels })
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : t('common.error')
  }
}
// Width: dragged from the left edge (or arrow keys on the handle), kept per browser.
const WIDTH_KEY = 'omini.panelWidth'
const MIN_WIDTH = 360
const maxWidth = () => Math.max(MIN_WIDTH, Math.min(960, window.innerWidth * 0.8))
const clampWidth = (w: number) => Math.round(Math.min(maxWidth(), Math.max(MIN_WIDTH, w)))
function savedWidth(): number {
  try {
    const w = Number(localStorage.getItem(WIDTH_KEY))
    if (w > 0) return clampWidth(w)
  } catch {
    // storage may be blocked
  }
  return clampWidth(Math.min(460, window.innerWidth * 0.34))
}
const width = ref(savedWidth())
// The popups of alerts move aside while the panel is open.
watch(width, (w) => (ui.panelWidth = w), { immediate: true })
onBeforeUnmount(() => (ui.panelWidth = 0))
const resizing = ref(false)
function keepWidth() {
  try {
    localStorage.setItem(WIDTH_KEY, String(width.value))
  } catch {
    // storage may be blocked
  }
}
let startX = 0
let startWidth = 0
function onResize(e: PointerEvent) {
  // The panel sits on the right: dragging left widens it.
  width.value = clampWidth(startWidth + startX - e.clientX)
}
function stopResize() {
  resizing.value = false
  window.removeEventListener('pointermove', onResize)
  window.removeEventListener('pointerup', stopResize)
  keepWidth()
}
function startResize(e: PointerEvent) {
  e.preventDefault()
  startX = e.clientX
  startWidth = width.value
  resizing.value = true
  window.addEventListener('pointermove', onResize)
  window.addEventListener('pointerup', stopResize)
}
function resizeBy(delta: number) {
  width.value = clampWidth(width.value + delta)
  keepWidth()
}
onBeforeUnmount(stopResize)

/** Key numbers at the top of the panel, only those known. */
const summary = computed(() => {
  const node = n.value
  if (!node) return []
  const d = node.device
  const phys = physicalPorts.value
  const out: {
    key: string
    label: string
    value: string
    tone?: 'ok' | 'warn' | 'danger' | 'muted'
    hint?: string
  }[] = []
  if (d?.uptime_s !== undefined)
    out.push({ key: 'uptime', label: t('panel.uptime'), value: formatUptime(d.uptime_s) })
  if (d?.os_version) out.push({ key: 'version', label: t('panel.version'), value: d.os_version })
  const fw = d?.firmware
  if (fw) {
    const checked = fw.checked_at ? formatAgo(fw.checked_at, locale.value) : undefined
    if (fw.update_available)
      out.push({
        key: 'updates',
        label: t('panel.updates'),
        value: [
          fw.updates ? t('panel.updatesN', fw.updates) : t('panel.updateAvailable'),
          fw.latest && fw.latest !== fw.current ? `→ ${fw.latest}` : '',
        ]
          .filter(Boolean)
          .join(' '),
        tone: 'warn',
        hint: [
          fw.needs_reboot ? t('panel.needsReboot') : '',
          checked ? t('panel.checked', { when: checked }) : '',
        ]
          .filter(Boolean)
          .join(' · '),
      })
    else if (fw.update_available === false)
      out.push({
        key: 'updates',
        label: t('panel.updates'),
        value: t('panel.upToDate'),
        tone: 'ok',
        hint: checked ? t('panel.checked', { when: checked }) : undefined,
      })
    else
      out.push({
        key: 'updates',
        label: t('panel.updates'),
        value: t('panel.notChecked'),
        tone: 'muted',
        hint: t('panel.notCheckedHint'),
      })
  }
  const temps = d?.temperatures ?? []
  if (temps.length) {
    const cpu = temps.filter((x) => x.kind === 'cpu')
    const hottest = Math.max(...(cpu.length ? cpu : temps).map((x) => x.celsius))
    out.push({
      key: 'temperature',
      label: t(cpu.length ? 'panel.cpuTemperature' : 'panel.temperature'),
      value: `${Math.round(hottest)} °C`,
      tone: hottest >= 85 ? 'danger' : hottest >= 70 ? 'warn' : undefined,
      hint: temps.map((x) => `${x.sensor}: ${Math.round(x.celsius)} °C`).join(' · '),
    })
  }
  if (d?.load_avg?.length)
    out.push({
      key: 'load',
      label: t('panel.load'),
      value: d.load_avg.map((v) => v.toFixed(2)).join(' · '),
      hint: t('panel.loadHint'),
    })
  const model = node.model ?? d?.model
  if (model) out.push({ key: 'model', label: t('panel.model'), value: model })
  if (phys.length)
    out.push({
      key: 'ports',
      label: t('panel.portsUp'),
      value: t('panel.portsUpValue', { up: phys.filter((p) => p.up).length, n: phys.length }),
    })
  if (children.value.length)
    out.push({ key: 'clients', label: t('panel.clients'), value: String(children.value.length) })
  if (d?.dhcp_leases?.length)
    out.push({ key: 'leases', label: t('panel.leases'), value: String(d.dhcp_leases.length) })
  return out
})
const bandNames: Record<string, string> = { '2.4ghz': '2.4 GHz', '5ghz': '5 GHz', '6ghz': '6 GHz' }
/** "5 GHz · 867 Mbps link" */
const bandText = computed(() => {
  const node = n.value
  if (!node?.band) return ''
  const rate = node.link_mbps ? t('panel.linkRate', { rate: Math.round(node.link_mbps) }) : ''
  return [bandNames[node.band] ?? node.band, rate].filter(Boolean).join(' · ')
})
/** Traffic right now: internet (routers, firewalls, WAN nodes) or a Wi-Fi client's. */
const flow = computed(() => {
  const node = n.value
  if (!node) return undefined
  // A Wi-Fi client's traffic shows even while idle (the map's badge does not).
  if (node.flow) return { down: node.flow.rx_bps, up: node.flow.tx_bps }
  return nodeFlow(node, byId.value)
})
// Long client lists show the first few; the rest on demand.
const CLIENTS_SHOWN = 8
const allClients = ref(false)
watch(
  () => props.node?.id,
  () => (allClients.value = false),
)
const shownChildren = computed(() =>
  allClients.value ? children.value : children.value.slice(0, CLIENTS_SHOWN),
)

const gateways = computed(() => n.value?.device?.gateways ?? n.value?.wan?.gateways ?? [])
const roleLabel = computed(() => (n.value?.type ? t(`types.${n.value.type}`) : ''))
const reasons = computed(() => parseReasons(n.value?.reasons))
const logoName = slugName
// Options for the user's corrections: products first, then systems and brands.
const iconOptions = computed(() =>
  Object.entries(logos)
    .map(([slug, logo]) => ({ slug, title: logo.title, product: productSlugs.has(slug) }))
    .sort((a, b) => Number(b.product) - Number(a.product) || a.title.localeCompare(b.title)),
)
const classifying = ref(false)
const typeChoice = ref('')
const iconChoiceValue = ref('')

function startClassify() {
  typeChoice.value = ''
  iconChoiceValue.value = n.value?.icon ?? ''
  classifying.value = true
}

async function saveClassification() {
  await save({ device_type: typeChoice.value, icon: iconChoiceValue.value })
  classifying.value = false
}
const title = computed(() => {
  if (!n.value) return ''
  if (n.value.kind === 'segment' && n.value.label === 'Unmanaged segment') return t('map.segment')
  return displayName(n.value, t)
})

// "Scan": nmap on this device now; the map is updated with what it finds.
const scanning = ref(false)
const scanResult = ref('')
watch(
  () => props.node?.id,
  () => (scanResult.value = ''),
)
const scannable = computed(
  () =>
    !!props.canScan && !!n.value?.ip && ['device', 'client', 'unmanaged'].includes(n.value.kind),
)
async function scan() {
  if (!n.value) return
  scanning.value = true
  scanResult.value = ''
  error.value = ''
  try {
    const host = await api.scanNode(n.value.id)
    const ports = host.open_ports?.length ?? 0
    scanResult.value = [t('panel.scanPorts', ports), host.os ? slugName(host.os) : '']
      .filter(Boolean)
      .join(' · ')
    emit('scanned')
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : t('common.error')
  } finally {
    scanning.value = false
  }
}

// Deleting takes a second click (no blocking dialog).
const confirmDelete = ref(false)
watch(
  () => props.node?.id,
  () => (confirmDelete.value = false),
)
async function remove() {
  if (!n.value) return
  if (!confirmDelete.value) {
    confirmDelete.value = true
    return
  }
  error.value = ''
  try {
    await api.deleteInventory([n.value.id])
    emit('deleted', n.value.id)
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : t('common.error')
  } finally {
    confirmDelete.value = false
  }
}

function startEdit() {
  alias.value = n.value?.label ?? ''
  editing.value = true
}

async function save(patch: {
  alias?: string
  pinned?: boolean
  hidden?: boolean
  device_type?: string
  icon?: string
}) {
  if (!n.value) return
  error.value = ''
  try {
    await api.updateInventory(n.value.id, patch)
    const local: Partial<TopoNode> = {}
    if (patch.alias !== undefined && patch.alias !== '') local.label = patch.alias
    if (patch.pinned !== undefined) local.pinned = patch.pinned
    if (patch.hidden !== undefined) local.hidden = patch.hidden
    if (patch.device_type) local.type = patch.device_type
    if (patch.icon !== undefined) local.icon = patch.icon || undefined
    emit('changed', local)
    editing.value = false
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : t('common.error')
  }
}
</script>

<template>
  <aside
    class="panel card"
    :class="{ resizing }"
    :style="{ '--panel-width': `${width}px` }"
    aria-live="polite"
  >
    <div
      class="resize"
      role="separator"
      aria-orientation="vertical"
      tabindex="0"
      data-test="resize"
      :aria-label="t('panel.resize')"
      :title="t('panel.resize')"
      :aria-valuenow="width"
      @pointerdown="startResize"
      @keydown.left.prevent="resizeBy(40)"
      @keydown.right.prevent="resizeBy(-40)"
    />
    <div class="scroll">
      <!-- Group of collapsed clients -->
      <template v-if="group">
        <header>
          <span class="icon"><NodeIcon name="users" /></span>
          <div class="title">
            <h2>
              {{
                group.area
                  ? group.area.name
                  : t(
                      group.clients.every((c) => c.kind === 'app')
                        ? 'map.appsLabel'
                        : 'map.groupLabel',
                      {
                        n: group.clients.length,
                      },
                    )
              }}
            </h2>
            <span class="muted">{{ t('map.groupOnline', { n: group.online }) }}</span>
          </div>
          <button class="btn ghost icon" :aria-label="t('common.close')" @click="emit('close')">
            <X :size="18" />
          </button>
        </header>
        <button
          class="btn primary wide"
          @click="emit('expand', group.area ? group.id : group.parentId)"
        >
          {{ t('panel.expandGroup') }}
        </button>
        <ul class="client-list">
          <li v-for="c in group.clients" :key="c.id" @click="emit('select', c.id)">
            <span class="dot" :class="{ online: c.online }" />
            <DeviceIcon :device="c" :size="15" />
            <span class="grow">{{ displayName(c, t) }}</span>
            <span class="muted mono">{{ c.ip }}</span>
          </li>
        </ul>
      </template>

      <template v-else-if="n">
        <header class="head">
          <span class="icon"><DeviceIcon :device="n" :size="24" /></span>
          <div class="title">
            <form v-if="editing" class="rename" @submit.prevent="save({ alias })">
              <input v-model="alias" class="input" autofocus :aria-label="t('panel.rename')" />
              <button class="btn icon" type="submit" :aria-label="t('common.save')">
                <Check :size="16" />
              </button>
            </form>
            <div v-else class="name-row">
              <h2>{{ title }}</h2>
              <button
                class="btn ghost icon name-action"
                type="button"
                data-test="rename"
                :title="t('panel.rename')"
                :aria-label="t('panel.rename')"
                @click="startEdit"
              >
                <Pencil :size="14" />
              </button>
              <!-- Web interface: an icon next to the name (one interface) or a short list. -->
              <a
                v-if="web.length === 1"
                :href="web[0]!.url"
                target="_blank"
                rel="noopener noreferrer"
                class="btn ghost icon web-link"
                data-test="open-web"
                :title="`${t('panel.openWeb')} · ${web[0]!.url}`"
                :aria-label="t('panel.openWeb')"
              >
                <ExternalLink :size="16" />
              </a>
              <div v-else-if="web.length > 1" class="web-menu-wrap">
                <button
                  type="button"
                  class="btn ghost icon web-link"
                  data-test="open-web-menu"
                  :title="t('panel.openWeb')"
                  :aria-label="t('panel.openWeb')"
                  :aria-expanded="webMenu"
                  @click="webMenu = !webMenu"
                >
                  <ExternalLink :size="16" />
                </button>
                <div v-if="webMenu" class="web-menu card" role="menu">
                  <a
                    v-for="svc in web"
                    :key="svc.url"
                    :href="svc.url"
                    target="_blank"
                    rel="noopener noreferrer"
                    role="menuitem"
                    data-test="open-web"
                    @click="webMenu = false"
                  >
                    <span class="grow">{{ svc.title || svc.url }}</span>
                    <span class="web-port">{{ t('panel.webPort', { port: svc.port }) }}</span>
                  </a>
                </div>
              </div>
            </div>
            <div class="status">
              <span class="pill" :class="n.online ? 'on' : 'off'">
                <span class="dot" :class="{ online: n.online }" />
                {{ n.online ? t('common.online') : t('common.offline') }}
              </span>
              <span v-if="roleLabel">{{ roleLabel }}</span>
              <span v-if="n.ip" class="mono">{{ n.ip }}</span>
            </div>
          </div>
          <button class="btn ghost icon" :aria-label="t('common.close')" @click="emit('close')">
            <X :size="18" />
          </button>
        </header>

        <div class="actions">
          <button
            v-if="scannable"
            class="btn small"
            data-test="scan"
            :disabled="scanning"
            :title="t('panel.scanHint')"
            @click="scan"
          >
            <Radar :size="14" :class="{ spin: scanning }" />
            {{ scanning ? t('panel.scanning') : t('panel.scan') }}
          </button>
          <button
            class="btn small"
            :title="t('panel.pinHint')"
            @click="save({ pinned: !n.pinned })"
          >
            <component :is="n.pinned ? PinOff : Pin" :size="14" />
            {{ n.pinned ? t('panel.unpin') : t('panel.pin') }}
          </button>
        </div>
        <p v-if="error" class="alert error">{{ error }}</p>
        <p v-if="scanResult" class="alert ok" role="status" data-test="scan-result">
          {{ t('panel.scanned', { result: scanResult }) }}
        </p>

        <p v-if="n.kind === 'segment'" class="hint">{{ t('map.segmentHint') }}</p>
        <p v-if="n.kind === 'unmanaged'" class="hint">{{ t('map.unmanagedHint') }}</p>

        <div v-if="summary.length || flow" class="tiles" data-test="summary">
          <div v-if="flow" class="tile traffic" data-test="flow">
            <span class="tile-label">{{
              n.flow ? t('panel.trafficNow') : t('panel.internetNow')
            }}</span>
            <span class="tile-value">
              <span class="down"><ArrowDown :size="13" />{{ formatRate(flow.down) }}</span>
              <span class="up"><ArrowUp :size="13" />{{ formatRate(flow.up) }}</span>
            </span>
          </div>
          <div
            v-for="s in summary"
            :key="s.key"
            class="tile"
            :class="s.tone"
            :title="s.hint"
            :data-test="`tile-${s.key}`"
          >
            <span class="tile-label">{{ s.label }}</span>
            <span class="tile-value">{{ s.value }}</span>
            <span v-if="s.hint && s.key === 'updates' && s.tone !== 'muted'" class="tile-hint">{{
              s.hint
            }}</span>
          </div>
        </div>

        <ResourceBars
          :cpu="n.device?.cpu_pct"
          :memory="n.device?.mem_pct"
          :cpu-count="n.device?.cpu_count"
          :mem-used="n.device?.mem_used_bytes"
          :mem-total="n.device?.mem_total_bytes"
          :swap="n.device?.swap_pct"
          :storage="n.device?.storage"
        />

        <section v-if="nodeAlerts.length" class="block" data-test="node-alerts">
          <h3>{{ t('insights.nodeAlerts') }}</h3>
          <ul class="node-alerts">
            <li
              v-for="a in nodeAlerts"
              :key="a.id"
              :class="[a.severity, { dismissed: a.dismissed }]"
            >
              <strong>{{ alertText(a, t, locale).title }}</strong>
              <span>{{ alertText(a, t, locale).detail }}</span>
            </li>
          </ul>
        </section>

        <section v-if="chart" class="block" data-test="node-traffic">
          <h3>{{ t('traffic.title') }}</h3>
          <TrafficChart :node="chart.node" :iface="chart.iface" />
        </section>

        <section v-if="n.kind === 'device'" class="block" data-test="ports-section">
          <h3>{{ t('panel.ports') }}</h3>
          <p v-if="!physicalPorts.length" class="muted">{{ t('panel.noPorts') }}</p>
          <PortPanel
            v-if="physicalPorts.length"
            :ports="physicalPorts"
            :links="portLinks"
            :labels="n.port_labels"
            :node-id="n.id"
            @select="(id) => emit('select', id)"
            @label="savePort"
          />
        </section>

        <DeviceExtras v-if="n.device" :device="n.device" />

        <section v-if="n.wan || gateways.length" class="block">
          <h3>{{ t('panel.internet') }}</h3>
          <dl v-if="n.wan" class="facts" data-test="wan">
            <dt>{{ t('panel.interface') }}</dt>
            <dd class="mono">
              {{ n.wan.interface }}<template v-if="n.wan.port"> → {{ n.wan.port }}</template>
            </dd>
            <template v-if="n.wan.speed_mbps">
              <dt>{{ t('panel.speed') }}</dt>
              <dd>{{ formatSpeed(n.wan.speed_mbps) }}</dd>
            </template>
            <template v-if="n.wan.ips?.length">
              <dt>IP</dt>
              <dd class="mono">{{ n.wan.ips.join(', ') }}</dd>
            </template>
          </dl>
          <ul v-if="gateways.length" class="gateways" data-test="gateways">
            <li v-for="g in gateways" :key="g.name" class="gateway" :class="g.status">
              <span class="dot" :class="g.status" />
              <span class="grow">
                <strong>{{ g.name }}</strong>
                <span class="muted"> · {{ t(`panel.gatewayStatus.${g.status}`) }}</span>
              </span>
              <span class="muted mono">
                <template v-if="g.rtt_ms !== undefined">{{ g.rtt_ms }} ms</template>
                <template v-if="g.loss_pct"> · {{ g.loss_pct }}% {{ t('panel.loss') }}</template>
              </span>
            </li>
          </ul>
        </section>

        <section v-if="children.length" class="block">
          <h3>{{ t('panel.clients') }} ({{ children.length }})</h3>
          <ul class="client-list">
            <li v-for="c in shownChildren" :key="c.id" @click="emit('select', c.id)">
              <span class="dot" :class="{ online: c.online }" />
              <DeviceIcon :device="c" :size="15" />
              <span class="grow">{{ displayName(c, t) }}</span>
              <span class="muted mono">{{ c.ip }}</span>
            </li>
          </ul>
          <button
            v-if="children.length > CLIENTS_SHOWN"
            class="link more"
            type="button"
            data-test="more-clients"
            @click="allClients = !allClients"
          >
            {{ allClients ? t('panel.showLess') : t('panel.showAll', { n: children.length }) }}
          </button>
        </section>

        <section v-if="talks.length" class="block" data-test="node-talks">
          <h3>{{ t('flows.talksTo') }}</h3>
          <ul class="talks">
            <li v-for="c in talks" :key="c.a + c.b">
              <button
                class="link grow"
                type="button"
                :disabled="!peerOf(c).id"
                @click="peerOf(c).id && emit('select', peerOf(c).id!)"
              >
                {{ peerOf(c).label }}
              </button>
              <span class="muted small">{{
                c.ports
                  .slice(0, 2)
                  .map((p) => serviceName(p.proto, p.port))
                  .join(', ')
              }}</span>
              <span class="mono small">{{ formatBytes(c.bytes_ab + c.bytes_ba) }}</span>
            </li>
          </ul>
          <RouterLink class="link small" :to="{ path: '/flows', query: { node: n.id } }">{{
            t('flows.allFlows')
          }}</RouterLink>
        </section>

        <section v-if="activity.length" class="block" data-test="node-activity">
          <h3>{{ t('insights.activity') }}</h3>
          <ul class="activity">
            <li v-for="e in activity" :key="e.id" :class="e.first ? 'first' : e.kind">
              <span class="dot" />
              <span class="grow">{{
                e.first
                  ? t('insights.firstSeen')
                  : e.kind === 'join'
                    ? t('insights.joined')
                    : t('insights.left')
              }}</span>
              <span class="muted mono">{{ activityTime.format(new Date(e.at)) }}</span>
            </li>
          </ul>
        </section>

        <section class="block">
          <h3>{{ t('panel.details') }}</h3>
          <dl class="facts">
            <template v-if="n.ip"
              ><dt>{{ t('panel.ip') }}</dt>
              <dd class="mono">{{ n.ip }}</dd></template
            >
            <template v-if="n.mac">
              <dt>{{ t('panel.mac') }}</dt>
              <dd class="mono">
                {{ n.mac }}
                <span v-if="n.random_mac" class="badge">{{ t('panel.randomMac') }}</span>
              </dd>
            </template>
            <template v-if="n.hostname && n.hostname !== n.label">
              <dt>{{ t('panel.hostname') }}</dt>
              <dd>{{ n.hostname }}</dd>
            </template>
            <template v-if="n.vendor"
              ><dt>{{ t('panel.vendor') }}</dt>
              <dd>{{ n.vendor }}</dd></template
            >
            <template v-if="n.device?.serial"
              ><dt>{{ t('panel.serial') }}</dt>
              <dd class="mono">{{ n.device.serial }}</dd></template
            >
            <template v-if="parent">
              <dt>{{ t('panel.connectedTo') }}</dt>
              <dd>
                <a href="#" @click.prevent="emit('select', parent.id)">{{ parent.label }}</a>
                <template v-if="n.port"> · {{ t('panel.port', { port: n.port }) }}</template>
              </dd>
            </template>
            <template v-if="runsOn">
              <dt>{{ t('panel.runsOn') }}</dt>
              <dd data-test="runs-on">
                <a href="#" @click.prevent="emit('select', runsOn.id)">{{ runsOn.label }}</a>
              </dd>
            </template>
            <template v-if="n.ssid">
              <dt>{{ t('panel.ssid') }}</dt>
              <dd>{{ n.ssid }}</dd>
            </template>
            <template v-if="n.band">
              <dt>{{ t('panel.band') }}</dt>
              <dd data-test="band">{{ bandText }}</dd>
            </template>
            <template v-if="n.signal_dbm !== undefined">
              <dt>{{ t('panel.signal') }}</dt>
              <dd>
                {{ n.signal_dbm }} dBm
                <span v-if="n.signal_dbm < -75" class="badge warn">{{
                  t('panel.weakSignal')
                }}</span>
              </dd>
            </template>
            <template v-if="n.last_seen && !n.online">
              <dt>{{ t('panel.lastSeen') }}</dt>
              <dd>{{ formatAgo(n.last_seen, locale) }}</dd>
            </template>
            <template v-if="integration">
              <dt>{{ t('panel.integration') }}</dt>
              <dd>
                <RouterLink to="/integrations">{{ integration.name }}</RouterLink>
                <span v-if="integration.status?.error" class="err">{{
                  integration.status.error
                }}</span>
              </dd>
            </template>
          </dl>
        </section>

        <section v-if="n.kind !== 'segment' && n.kind !== 'app'" class="block classification">
          <h3>{{ t('panel.classification') }}</h3>
          <form v-if="classifying" class="classify-form" @submit.prevent="saveClassification">
            <label class="field">
              <span>{{ t('panel.changeType') }}</span>
              <select v-model="typeChoice" class="select" data-test="type-select">
                <option value="">{{ t('panel.automatic') }}</option>
                <option v-for="ty in deviceTypes" :key="ty" :value="ty">
                  {{ t(`types.${ty}`) }}
                </option>
              </select>
            </label>
            <label class="field">
              <span>{{ t('panel.changeIcon') }}</span>
              <select v-model="iconChoiceValue" class="select" data-test="icon-select">
                <option value="">{{ t('panel.automatic') }}</option>
                <option v-for="o in iconOptions" :key="o.slug" :value="o.slug">
                  {{ o.title }}
                </option>
              </select>
            </label>
            <div class="row-actions">
              <button class="btn small" type="button" @click="classifying = false">
                {{ t('common.cancel') }}
              </button>
              <button class="btn small primary" type="submit">{{ t('common.save') }}</button>
            </div>
          </form>
          <template v-else>
            <dl class="facts">
              <dt>{{ t('panel.role') }}</dt>
              <dd>
                {{ roleLabel }}
                <button
                  class="link"
                  type="button"
                  data-test="edit-classification"
                  @click="startClassify"
                >
                  {{ t('common.edit') }}
                </button>
              </dd>
              <template v-if="n.product"
                ><dt>{{ t('panel.product') }}</dt>
                <dd>{{ logoName(n.product) }}</dd></template
              >
              <template v-if="n.os"
                ><dt>{{ t('panel.os') }}</dt>
                <dd>{{ logoName(n.os) }}</dd></template
              >
              <template v-if="n.brand"
                ><dt>{{ t('panel.brand') }}</dt>
                <dd>{{ logoName(n.brand) }}</dd></template
              >
            </dl>
            <div v-if="reasons.length" class="reasons">
              <span class="muted">{{ t('panel.detectedBy') }}:</span>
              <span v-for="(r, i) in reasons" :key="i" class="badge">
                {{ t(`panel.${r.key}`) }}<template v-if="r.value">: {{ r.value }}</template>
              </span>
            </div>
          </template>
        </section>

        <footer v-if="n.kind !== 'app'" class="manage">
          <button
            class="btn small"
            data-test="hide"
            :title="t('panel.hideHint')"
            @click="save({ hidden: !n.hidden })"
          >
            <component :is="n.hidden ? Eye : EyeOff" :size="14" />
            {{ n.hidden ? t('panel.unhide') : t('panel.hide') }}
          </button>
          <button
            class="btn small danger"
            data-test="delete"
            :title="t('panel.deleteHint')"
            @click="remove"
          >
            <Trash2 :size="14" />
            {{ confirmDelete ? t('panel.confirmDelete') : t('panel.delete') }}
          </button>
        </footer>
      </template>
    </div>
  </aside>
</template>

<style scoped>
.talks {
  display: grid;
  gap: 4px;
  margin: 0 0 6px;
  padding: 0;
  list-style: none;
  font-size: 13px;
}
.talks li {
  display: flex;
  align-items: center;
  gap: 8px;
}
.talks .grow {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-align: left;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.talks .small {
  font-size: 12px;
  white-space: nowrap;
}
.node-alerts {
  display: grid;
  gap: 6px;
  margin: 0;
  padding: 0;
  list-style: none;
}
.node-alerts li {
  display: grid;
  gap: 2px;
  padding: 8px 10px;
  border-left: 3px solid var(--sev);
  border-radius: var(--radius-sm);
  background: color-mix(in srgb, var(--sev) 10%, transparent);
  font-size: 13px;
}
.node-alerts li span {
  color: var(--text-muted);
  font-size: 12.5px;
}
.node-alerts .critical {
  --sev: var(--danger);
}
.node-alerts .warning {
  --sev: var(--warn);
}
.node-alerts .info {
  --sev: var(--accent);
}
.node-alerts .dismissed {
  opacity: 0.6;
}
.activity {
  display: grid;
  gap: 4px;
  margin: 0;
  padding: 0;
  list-style: none;
  font-size: 13px;
}
.activity li {
  display: flex;
  align-items: center;
  gap: 8px;
}
.activity .dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--text-muted);
}
.activity .join .dot {
  background: var(--ok);
}
.activity .first .dot {
  background: var(--accent);
}
.spin {
  animation: spin 1.2s linear infinite;
}
@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}
.name-row {
  display: flex;
  align-items: center;
  gap: 4px;
  min-width: 0;
}
.web-link {
  flex: none;
  color: var(--accent);
}
.web-menu-wrap {
  position: relative;
}
.web-menu {
  position: absolute;
  z-index: 40;
  top: calc(100% + 4px);
  left: 0;
  min-width: 220px;
  padding: 4px;
  box-shadow: var(--shadow);
}
.web-menu a {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 7px 10px;
  border-radius: var(--radius-sm);
  color: var(--text);
  text-decoration: none;
  font-size: 13px;
}
.web-menu a:hover {
  background: var(--surface-hover);
}
.dot.up {
  background: var(--ok);
}
.dot.degraded {
  background: var(--warn);
}
.dot.down {
  background: var(--danger);
}
.warn {
  color: var(--warn);
  font-size: 12px;
}
.panel {
  position: absolute;
  top: 12px;
  right: 12px;
  bottom: 12px;
  width: var(--panel-width, 440px);
  max-width: calc(100% - 24px);
  padding: 0;
  overflow: hidden;
  box-shadow: var(--shadow);
  z-index: 5;
  display: flex;
}
.panel.resizing {
  user-select: none;
}
.scroll {
  flex: 1;
  min-width: 0;
  padding: 18px 18px 14px;
  overflow: hidden auto;
}
/* Drag the left edge to widen or narrow the panel. */
.resize {
  position: absolute;
  top: 0;
  bottom: 0;
  left: 0;
  width: 8px;
  cursor: ew-resize;
  z-index: 2;
  touch-action: none;
}
.resize::after {
  content: '';
  position: absolute;
  top: 50%;
  left: 2px;
  width: 4px;
  height: 40px;
  margin-top: -20px;
  border-radius: 2px;
  background: var(--border);
  opacity: 0;
  transition: opacity 0.15s;
}
.panel:hover .resize::after,
.resize:focus-visible::after,
.resizing .resize::after {
  opacity: 1;
}
.resize:hover::after,
.resizing .resize::after {
  background: var(--accent);
}
header {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  margin-bottom: 14px;
}
.status {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 4px 10px;
  margin-top: 4px;
  color: var(--text-muted);
  font-size: 12.5px;
}
.pill {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 1px 8px;
  border-radius: 999px;
  background: var(--surface-hover);
  color: var(--text);
}
.name-action {
  width: 26px;
  height: 26px;
  flex: none;
  opacity: 0;
}
.name-row:hover .name-action,
.name-action:focus-visible {
  opacity: 0.8;
}
.tiles {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(118px, 1fr));
  gap: 8px;
  margin-bottom: 14px;
}
.tile {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
  padding: 9px 11px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface-hover);
}
.tile-label {
  color: var(--text-muted);
  font-size: 11.5px;
}
.tile-value {
  font-size: 14px;
  font-weight: 600;
  overflow-wrap: anywhere;
}
.tile.traffic {
  grid-column: span 2;
}
.tile.ok .tile-value {
  color: var(--ok);
}
.tile.warn {
  border-color: color-mix(in srgb, var(--warn) 60%, var(--border));
}
.tile.warn .tile-value {
  color: var(--warn);
}
.tile.danger {
  border-color: color-mix(in srgb, var(--danger) 60%, var(--border));
}
.tile.danger .tile-value {
  color: var(--danger);
}
.tile.muted .tile-value {
  color: var(--text-muted);
  font-weight: 500;
}
.tile-hint {
  color: var(--text-muted);
  font-size: 11px;
}
.tile.traffic .tile-value {
  display: flex;
  gap: 14px;
}
.tile.traffic .down,
.tile.traffic .up {
  display: inline-flex;
  align-items: center;
  gap: 3px;
}
.tile.traffic .down {
  color: var(--ok);
}
.tile.traffic .up {
  color: var(--accent);
}
.block {
  padding-top: 12px;
  margin: 14px 0 0;
  border-top: 1px solid var(--border);
}
.nowrap {
  white-space: nowrap;
}
.gateways {
  list-style: none;
  margin: 8px 0 0;
  padding: 0;
  display: grid;
  gap: 6px;
}
.gateway {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 7px 10px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  font-size: 13px;
}
.gateway.down {
  border-color: var(--danger);
}
.gateway.degraded {
  border-color: var(--warn);
}
.manage {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 16px;
  padding-top: 12px;
  border-top: 1px solid var(--border);
}
.icon {
  display: grid;
  place-items: center;
  width: 40px;
  height: 40px;
  flex: none;
  border-radius: 10px;
  background: var(--accent-soft);
  color: var(--accent);
}
.title {
  flex: 1;
  min-width: 0;
}
h2 {
  font-size: 17px;
  overflow-wrap: anywhere;
}
.rename {
  display: flex;
  gap: 6px;
}
.actions {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-bottom: 12px;
}
.web {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-bottom: 14px;
}
.web .btn {
  text-decoration: none;
}
.web-port {
  opacity: 0.7;
  font-size: 12px;
}
.reasons {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  align-items: center;
  font-size: 12.5px;
  margin-bottom: 6px;
}
.classify-form .field {
  margin-bottom: 10px;
}
.row-actions {
  display: flex;
  justify-content: flex-end;
  gap: 6px;
}
.link {
  margin-left: 8px;
  padding: 0;
  border: 0;
  background: none;
  color: var(--accent);
  font: inherit;
  font-size: 12.5px;
  cursor: pointer;
}
.link.more {
  margin: 6px 0 0 8px;
}
.hint {
  margin: 0 0 12px;
  padding: 10px 12px;
  border-radius: var(--radius-sm);
  background: var(--surface-hover);
  color: var(--text-muted);
  font-size: 13px;
}
.facts {
  display: grid;
  grid-template-columns: minmax(90px, auto) 1fr;
  gap: 7px 14px;
  margin: 0;
  font-size: 13px;
}
dt {
  color: var(--text-muted);
}
dd {
  margin: 0;
  overflow-wrap: anywhere;
}
.err {
  display: block;
  color: var(--danger);
  font-size: 12.5px;
}
h3 {
  margin: 0 0 10px;
  font-size: 13px;
  color: var(--text-muted);
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.03em;
}
/* Long links and descriptions wrap instead of widening the panel. */
.ports td.grow {
  width: 100%;
  max-width: 0; /* take the remaining width, never more */
  overflow-wrap: anywhere;
}
.ports td {
  padding: 6px 6px;
  font-size: 13px;
}
.grow {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.client-list {
  list-style: none;
  margin: 0;
  padding: 0;
}
.client-list li {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 7px 8px;
  border-radius: var(--radius-sm);
  cursor: pointer;
  font-size: 13px;
}
.client-list li:hover {
  background: var(--surface-hover);
}
.btn.wide {
  width: 100%;
  justify-content: center;
  margin-bottom: 10px;
}
.alert {
  margin: 0 0 12px;
}

@media (max-width: 640px) {
  .panel {
    top: auto;
    left: 12px;
    right: 12px;
    width: auto;
    height: 65%;
  }
  .resize {
    display: none;
  }
}
</style>
