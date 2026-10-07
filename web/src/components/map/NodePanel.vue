<script setup lang="ts">
import { Check, Eye, EyeOff, ExternalLink, Pencil, Pin, PinOff, Trash2, X } from 'lucide-vue-next'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import DeviceIcon from '@/components/DeviceIcon.vue'
import NodeIcon from '@/components/NodeIcon.vue'
import PortPanel from '@/components/map/PortPanel.vue'
import ResourceBars from '@/components/map/ResourceBars.vue'
import { api, ApiError } from '@/lib/api'
import { formatAgo, formatSpeed, formatUptime } from '@/lib/format'
import type { ClientGroup } from '@/lib/graph'
import { childrenOf, linkOnPort } from '@/lib/graph'
import { deviceTypes, logos, productSlugs, slugName } from '@/lib/icons'
import { displayName } from '@/lib/names'
import { parseReasons } from '@/lib/reasons'
import type { Integration, TopoEdge, TopoNode, WebService } from '@/lib/types'

const props = defineProps<{
  node?: TopoNode
  group?: ClientGroup
  nodes: TopoNode[]
  edges: TopoEdge[]
  integration?: Integration
  expandedParent: boolean
}>()
const emit = defineEmits<{
  close: []
  changed: [patch: Partial<TopoNode>]
  select: [id: string]
  expand: [parentId: string]
  collapse: [parentId: string]
  deleted: [id: string]
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

watch(
  () => props.node?.id,
  (id) => {
    editing.value = false
    error.value = ''
    detectWeb(id, props.node?.ip)
  },
  { immediate: true },
)

const n = computed(() => props.node)
const byId = computed(() => new Map(props.nodes.map((x) => [x.id, x])))
const parent = computed(() => (n.value?.parent_id ? byId.value.get(n.value.parent_id) : undefined))
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
/** Ports, with the user's description in place of the device's. */
const ports = computed(() =>
  (n.value?.device?.interfaces ?? [])
    .filter((i) => i.type !== 'loopback')
    .map((i) => {
      const own = n.value?.port_labels?.[i.name]
      return own ? { ...i, description: own } : i
    }),
)

// Editing a port's description inline.
const editingPort = ref<string>()
const portDraft = ref('')
function editPort(name: string, current?: string) {
  editingPort.value = name
  portDraft.value = current ?? ''
}
async function savePort() {
  const node = n.value
  const port = editingPort.value
  if (!node || !port) return
  editingPort.value = undefined
  const label = portDraft.value.trim()
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
  <aside class="panel card" aria-live="polite">
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
      <header>
        <span class="icon"><DeviceIcon :device="n" :size="22" /></span>
        <div class="title">
          <form v-if="editing" class="rename" @submit.prevent="save({ alias })">
            <input v-model="alias" class="input" autofocus :aria-label="t('panel.rename')" />
            <button class="btn icon" type="submit" :aria-label="t('common.save')">
              <Check :size="16" />
            </button>
          </form>
          <div v-else class="name-row">
            <h2>{{ title }}</h2>
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
          <span class="muted">
            <span class="dot" :class="{ online: n.online }" />
            {{ n.online ? t('common.online') : t('common.offline') }}
            <template v-if="roleLabel"> · {{ roleLabel }}</template>
          </span>
        </div>
        <button class="btn ghost icon" :aria-label="t('common.close')" @click="emit('close')">
          <X :size="18" />
        </button>
      </header>

      <div class="actions">
        <button class="btn small" @click="startEdit">
          <Pencil :size="14" />{{ t('panel.rename') }}
        </button>
        <button class="btn small" :title="t('panel.pinHint')" @click="save({ pinned: !n.pinned })">
          <component :is="n.pinned ? PinOff : Pin" :size="14" />
          {{ n.pinned ? t('panel.unpin') : t('panel.pin') }}
        </button>
        <button v-if="expandedParent" class="btn small" @click="emit('collapse', n.id)">
          {{ t('panel.collapseGroup') }}
        </button>
        <template v-if="n.kind !== 'app'">
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
        </template>
      </div>
      <p v-if="error" class="alert error">{{ error }}</p>

      <ResourceBars :cpu="n.device?.cpu_pct" :memory="n.device?.mem_pct" />

      <p v-if="n.kind === 'segment'" class="hint">{{ t('map.segmentHint') }}</p>
      <p v-if="n.kind === 'unmanaged'" class="hint">{{ t('map.unmanagedHint') }}</p>

      <section v-if="n.kind !== 'segment' && n.kind !== 'app'" class="classification">
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
              <option v-for="o in iconOptions" :key="o.slug" :value="o.slug">{{ o.title }}</option>
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
          <dl class="facts compact">
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
        <template v-if="n.model"
          ><dt>{{ t('panel.model') }}</dt>
          <dd>{{ n.model }}</dd></template
        >
        <template v-if="n.device?.uptime_s !== undefined">
          <dt>{{ t('panel.uptime') }}</dt>
          <dd>{{ formatUptime(n.device.uptime_s) }}</dd>
        </template>
        <template v-if="parent">
          <dt>{{ t('panel.connectedTo') }}</dt>
          <dd>
            <a href="#" @click.prevent="emit('select', parent.id)">{{ parent.label }}</a>
            <template v-if="n.port"> · {{ t('panel.port', { port: n.port }) }}</template>
          </dd>
        </template>
        <template v-if="n.ssid">
          <dt>{{ t('panel.ssid') }}</dt>
          <dd>{{ n.ssid }}</dd>
        </template>
        <template v-if="n.signal_dbm !== undefined">
          <dt>{{ t('panel.signal') }}</dt>
          <dd>
            {{ n.signal_dbm }} dBm
            <span v-if="n.signal_dbm < -75" class="badge warn">{{ t('panel.weakSignal') }}</span>
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
            <span v-if="integration.status?.error" class="err">{{ integration.status.error }}</span>
          </dd>
        </template>
      </dl>

      <section v-if="n.kind === 'device'">
        <h3>{{ t('panel.ports') }}</h3>
        <p v-if="!ports.length" class="muted">{{ t('panel.noPorts') }}</p>
        <PortPanel
          v-if="physicalPorts.length"
          :ports="physicalPorts"
          :links="portLinks"
          @select="(id) => emit('select', id)"
        />
        <table v-if="ports.length" class="table ports">
          <tbody>
            <tr v-for="p in ports" :key="p.name">
              <td><span class="dot" :class="{ online: p.up }" /></td>
              <td class="mono">{{ p.name }}</td>
              <td :title="p.media">
                {{ p.up ? formatSpeed(p.speed_mbps) : t('panel.portDown') }}
                <span v-if="p.up && p.duplex === 'half'" class="warn">{{
                  t('panel.halfDuplex')
                }}</span>
              </td>
              <td class="grow">
                <form v-if="editingPort === p.name" class="port-edit" @submit.prevent="savePort">
                  <input
                    v-model="portDraft"
                    class="input"
                    maxlength="80"
                    autofocus
                    data-test="port-label-input"
                    :placeholder="t('panel.portLabelHint')"
                    :aria-label="t('panel.portLabel')"
                    @keydown.esc.prevent="editingPort = undefined"
                  />
                  <button class="btn icon" type="submit" :aria-label="t('common.save')">
                    <Check :size="14" />
                  </button>
                </form>
                <template
                  v-for="other in [linkOnPort(n.id, p.name, nodes, edges)]"
                  v-else
                  :key="other?.id"
                >
                  <a v-if="other" href="#" @click.prevent="emit('select', other.id)">{{
                    other.label
                  }}</a>
                  <span v-else class="muted">{{ p.description }}</span>
                  <span v-if="other && p.description" class="muted"> · {{ p.description }}</span>
                  <button
                    class="btn ghost icon edit-port"
                    type="button"
                    data-test="edit-port"
                    :aria-label="t('panel.portLabel')"
                    :title="t('panel.portLabel')"
                    @click="editPort(p.name, n.port_labels?.[p.name] ?? p.description)"
                  >
                    <Pencil :size="12" />
                  </button>
                </template>
              </td>
            </tr>
          </tbody>
        </table>
      </section>

      <section v-if="n.wan" data-test="wan">
        <h3>{{ t('panel.uplink') }}</h3>
        <dl>
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
      </section>

      <section v-if="gateways.length" data-test="gateways">
        <h3>{{ t('panel.gateways') }}</h3>
        <table class="table ports">
          <tbody>
            <tr v-for="g in gateways" :key="g.name">
              <td><span class="dot" :class="g.status" /></td>
              <td>{{ g.name }}</td>
              <td>{{ t(`panel.gatewayStatus.${g.status}`) }}</td>
              <td class="grow muted mono">
                <template v-if="g.rtt_ms !== undefined">{{ g.rtt_ms }} ms</template>
                <template v-if="g.loss_pct"> · {{ g.loss_pct }}% {{ t('panel.loss') }}</template>
              </td>
            </tr>
          </tbody>
        </table>
      </section>

      <section v-if="children.length">
        <h3>{{ t('panel.clients') }} ({{ children.length }})</h3>
        <ul class="client-list">
          <li v-for="c in children" :key="c.id" @click="emit('select', c.id)">
            <span class="dot" :class="{ online: c.online }" />
            <DeviceIcon :device="c" :size="15" />
            <span class="grow">{{ displayName(c, t) }}</span>
            <span class="muted mono">{{ c.ip }}</span>
          </li>
        </ul>
      </section>
    </template>
  </aside>
</template>

<style scoped>
.port-edit {
  display: flex;
  gap: 4px;
}
.port-edit .input {
  padding: 2px 6px;
  font-size: 12.5px;
}
.edit-port {
  width: 22px;
  height: 22px;
  margin-left: 2px;
  opacity: 0;
  vertical-align: middle;
}
tr:hover .edit-port,
.edit-port:focus-visible {
  opacity: 1;
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
  width: 360px;
  max-width: calc(100% - 24px);
  padding: 16px;
  overflow: hidden auto;
  box-shadow: var(--shadow);
  z-index: 5;
}
header {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  margin-bottom: 12px;
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
  font-size: 16px;
  overflow-wrap: anywhere;
}
.title .muted {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 12.5px;
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
.small {
  font-size: 12.5px;
  margin: 0 0 12px;
}
.classification {
  margin-bottom: 10px;
}
.facts.compact {
  margin-bottom: 8px;
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
  grid-template-columns: auto 1fr;
  gap: 6px 14px;
  margin: 0 0 16px;
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
  margin: 8px 0 8px;
  font-size: 13px;
  color: var(--text-muted);
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.03em;
}
section {
  margin-bottom: 14px;
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
    height: 60%;
  }
}
</style>
