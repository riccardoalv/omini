<script setup lang="ts">
import { Check, ExternalLink, Pencil, Pin, PinOff, X } from 'lucide-vue-next'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import NodeIcon from '@/components/NodeIcon.vue'
import { api, ApiError } from '@/lib/api'
import { formatAgo, formatSpeed, formatUptime } from '@/lib/format'
import type { ClientGroup } from '@/lib/graph'
import { childrenOf, iconFor, linkOnPort } from '@/lib/graph'
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
const roleLabel = computed(() => (n.value?.role ? t(`roles.${n.value.role}`) : ''))
const title = computed(() => {
  if (!n.value) return ''
  if (n.value.kind === 'segment' && n.value.label === 'Unmanaged segment') return t('map.segment')
  return n.value.label
})

function startEdit() {
  alias.value = n.value?.label ?? ''
  editing.value = true
}

async function save(patch: { alias?: string; pinned?: boolean }) {
  if (!n.value) return
  error.value = ''
  try {
    await api.updateInventory(n.value.id, patch)
    const local: Partial<TopoNode> = {}
    if (patch.alias !== undefined && patch.alias !== '') local.label = patch.alias
    if (patch.pinned !== undefined) local.pinned = patch.pinned
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
          <h2>{{ t('map.groupLabel', { n: group.clients.length }) }}</h2>
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
          <NodeIcon :name="iconFor(c)" :size="15" />
          <span class="grow">{{ c.label }}</span>
          <span class="muted mono">{{ c.ip }}</span>
        </li>
      </ul>
    </template>

    <template v-else-if="n">
      <header>
        <span class="icon"><NodeIcon :name="iconFor(n)" /></span>
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
            <NodeIcon :name="iconFor(c)" :size="15" />
            <span class="grow">{{ c.label }}</span>
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
