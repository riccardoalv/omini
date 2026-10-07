<script setup lang="ts">
import { Check, ExternalLink, Pencil, Pin, PinOff, X } from 'lucide-vue-next'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import DeviceIcon from '@/components/DeviceIcon.vue'
import NodeIcon from '@/components/NodeIcon.vue'
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
}>()
const { t, locale } = useI18n()

const editing = ref(false)
const alias = ref('')
const error = ref('')
const web = ref<WebService[]>([])
const detectingWeb = ref(false)

// Look for a web interface (admin page, NAS, hypervisor...) when a node with an IP is shown.
async function detectWeb(id: string | undefined, ip: string | undefined) {
  web.value = []
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
const ports = computed(() =>
  (n.value?.device?.interfaces ?? []).filter((i) => i.type !== 'loopback'),
)
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

function startEdit() {
  alias.value = n.value?.label ?? ''
  editing.value = true
}

async function save(patch: {
  alias?: string
  pinned?: boolean
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
              t(group.clients.every((c) => c.kind === 'app') ? 'map.appsLabel' : 'map.groupLabel', {
                n: group.clients.length,
              })
            }}
          </h2>
          <span class="muted">{{ t('map.groupOnline', { n: group.online }) }}</span>
        </div>
        <button class="btn ghost icon" :aria-label="t('common.close')" @click="emit('close')">
          <X :size="18" />
        </button>
      </header>
      <button class="btn primary wide" @click="emit('expand', group.parentId)">
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
          <h2 v-else>{{ title }}</h2>
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
      </div>
      <p v-if="error" class="alert error">{{ error }}</p>

      <div v-if="web.length" class="web">
        <a
          v-for="(svc, i) in web"
          :key="svc.url"
          :href="svc.url"
          target="_blank"
          rel="noopener noreferrer"
          class="btn small"
          :class="{ primary: i === 0 }"
          data-test="open-web"
        >
          <ExternalLink :size="14" />
          {{ i === 0 ? t('panel.openWeb') : svc.title || svc.url }}
          <span class="web-port">{{ t('panel.webPort', { port: svc.port }) }}</span>
        </a>
      </div>
      <p v-else-if="detectingWeb" class="muted small">{{ t('panel.detectingWeb') }}</p>

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
        <template v-if="n.device?.cpu_pct !== undefined">
          <dt>{{ t('panel.cpu') }}</dt>
          <dd>{{ Math.round(n.device.cpu_pct) }}%</dd>
        </template>
        <template v-if="n.device?.mem_pct !== undefined">
          <dt>{{ t('panel.memory') }}</dt>
          <dd>{{ Math.round(n.device.mem_pct) }}%</dd>
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
        <table v-else class="table ports">
          <tbody>
            <tr v-for="p in ports" :key="p.name">
              <td><span class="dot" :class="{ online: p.up }" /></td>
              <td class="mono">{{ p.name }}</td>
              <td>{{ p.up ? formatSpeed(p.speed_mbps) : t('panel.portDown') }}</td>
              <td class="grow">
                <template
                  v-for="other in [linkOnPort(n.id, p.name, nodes, edges)]"
                  :key="other?.id"
                >
                  <a v-if="other" href="#" @click.prevent="emit('select', other.id)">{{
                    other.label
                  }}</a>
                  <span v-else class="muted">{{ p.description }}</span>
                </template>
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
.panel {
  position: absolute;
  top: 12px;
  right: 12px;
  bottom: 12px;
  width: 360px;
  max-width: calc(100% - 24px);
  padding: 16px;
  overflow: auto;
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
