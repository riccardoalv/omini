<script setup lang="ts">
import { ChevronDown, ChevronRight, Globe } from 'lucide-vue-next'
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'

import DeviceIcon from '@/components/DeviceIcon.vue'
import { api } from '@/lib/api'
import { formatAgo, formatBytes } from '@/lib/format'
import { displayName } from '@/lib/names'
import { serviceName } from '@/lib/services'
import { formatRate } from '@/lib/traffic'
import type {
  Destination,
  InternetRange,
  InternetResponse,
  InternetUsage,
  TopoNode,
} from '@/lib/types'

/**
 * Who uses the internet, from the flow exports: each device's download and
 * upload over a period, busiest first, and where it went (by name when the
 * network's DNS server saw the lookup).
 */
const { t, locale } = useI18n()
const router = useRouter()

const RANGES: InternetRange[] = ['15m', '1h', 'today', '7d', '30d']
const range = ref<InternetRange>('15m')
const data = ref<InternetResponse>()
const nodes = ref(new Map<string, TopoNode>())
const loading = ref(true)
const open = ref<string>()
const dests = ref<Destination[]>()

async function load() {
  try {
    const [usage, topo] = await Promise.all([
      api.internet(range.value),
      nodes.value.size ? undefined : api.topology(),
    ])
    data.value = usage
    if (topo) nodes.value = new Map(topo.topology.nodes.map((n) => [n.id, n]))
  } finally {
    loading.value = false
  }
}
watch(range, () => {
  void load()
  if (open.value) void loadDests(open.value)
})

/** Where a device went: the last hour for the short periods, else the last day. */
const destHours = computed(() => (range.value === '15m' || range.value === '1h' ? 1 : 24))
async function loadDests(id: string) {
  dests.value = undefined
  try {
    const res = await api.destinations(id, destHours.value)
    if (open.value === id) dests.value = res.destinations.slice(0, 12)
  } catch {
    if (open.value === id) dests.value = []
  }
}
function toggle(id: string) {
  open.value = open.value === id ? undefined : id
  if (open.value) void loadDests(id)
}

const nameOf = (u: InternetUsage) => {
  const n = nodes.value.get(u.node)
  return n ? displayName(n, t) : u.label
}
const rows = computed(() => data.value?.devices ?? [])
const max = computed(() => Math.max(1, ...rows.value.map((u) => u.rx + u.tx)))
const destMax = computed(() => Math.max(1, ...(dests.value ?? []).map((d) => d.rx + d.tx)))
/** The average rate, for the short periods. */
const rate = (bytes: number) =>
  data.value && (range.value === '15m' || range.value === '1h')
    ? formatRate((bytes * 8) / Math.max(1, data.value.seconds))
    : ''

function showOnMap(id: string) {
  void router.push({ name: 'map', query: { node: id } })
}

let timer: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  void load()
  timer = setInterval(load, 60_000)
})
onBeforeUnmount(() => clearInterval(timer))
</script>

<template>
  <div class="page">
    <header class="page-header">
      <div>
        <h1>{{ t('internet.title') }}</h1>
        <p class="muted">{{ t('internet.subtitle') }}</p>
      </div>
    </header>

    <div class="filters">
      <div class="segmented" role="group">
        <button
          v-for="r in RANGES"
          :key="r"
          class="btn small"
          :class="{ active: range === r }"
          :data-test="`internet-range-${r}`"
          @click="range = r"
        >
          {{ t(`internet.ranges.${r}`) }}
        </button>
      </div>
      <p v-if="data && rows.length" class="total" data-test="internet-total">
        <span>↓ {{ formatBytes(data.total.rx) }}</span>
        <span>↑ {{ formatBytes(data.total.tx) }}</span>
        <span v-if="rate(data.total.rx)" class="muted">
          {{ t('internet.average', { down: rate(data.total.rx), up: rate(data.total.tx) }) }}
        </span>
      </p>
    </div>

    <p v-if="loading" class="muted">{{ t('common.loading') }}</p>
    <div v-else-if="!data?.listening && !rows.length" class="card empty" data-test="internet-off">
      <Globe :size="26" />
      <p>{{ t('flows.off') }}</p>
      <p class="muted small">{{ t('flows.offHint') }}</p>
      <RouterLink class="btn primary" :to="{ path: '/integrations', query: { add: 'flows' } }">
        {{ t('flows.enable') }}
      </RouterLink>
    </div>
    <template v-else>
      <div class="card list">
        <div v-for="u in rows" :key="u.node" class="device" data-test="internet-device">
          <button
            class="row"
            type="button"
            :aria-expanded="open === u.node"
            @click="toggle(u.node)"
          >
            <component
              :is="open === u.node ? ChevronDown : ChevronRight"
              :size="15"
              class="muted"
            />
            <DeviceIcon v-if="nodes.get(u.node)" :device="nodes.get(u.node)!" :size="16" />
            <span class="name">{{ nameOf(u) }}</span>
            <span class="bar" :title="`↓ ${formatBytes(u.rx)} · ↑ ${formatBytes(u.tx)}`">
              <i class="down" :style="{ width: `${(u.rx / max) * 100}%` }" />
              <i class="up" :style="{ width: `${(u.tx / max) * 100}%` }" />
            </span>
            <span class="mono num">↓ {{ formatBytes(u.rx) }}</span>
            <span class="mono num muted">↑ {{ formatBytes(u.tx) }}</span>
          </button>
          <div v-if="open === u.node" class="dests" data-test="internet-dests">
            <p v-if="!dests" class="muted small">{{ t('common.loading') }}</p>
            <p v-else-if="!dests.length" class="muted small">{{ t('internet.noDestinations') }}</p>
            <template v-else>
              <p class="muted small">{{ t('internet.wentTo', { hours: destHours }, destHours) }}</p>
              <ul>
                <li v-for="d in dests" :key="d.name">
                  <span class="dest" :title="d.name">{{ d.name }}</span>
                  <span class="muted small svc">{{
                    d.ports
                      .slice(0, 2)
                      .map((p) => serviceName(p.proto, p.port))
                      .join(', ')
                  }}</span>
                  <span class="bar thin"
                    ><i class="down" :style="{ width: `${((d.rx + d.tx) / destMax) * 100}%` }"
                  /></span>
                  <span class="mono num">{{ formatBytes(d.rx + d.tx) }}</span>
                </li>
              </ul>
            </template>
            <button v-if="u.on_map" class="link small" type="button" @click="showOnMap(u.node)">
              {{ t('insights.onMap') }}
            </button>
          </div>
        </div>
        <p v-if="!rows.length" class="muted empty-row">{{ t('internet.none') }}</p>
      </div>
      <p class="muted small foot">{{ t('internet.namesHint') }}</p>
      <p v-if="data?.exporters.length" class="muted small exporters" data-test="flows-exporters">
        {{ t('flows.exporters') }}:
        <span v-for="e in data.exporters" :key="e.ip">
          {{ e.ip }} ({{ e.kind === 'sflow' ? 'sFlow' : `NetFlow ${e.version ?? ''}` }},
          {{ formatAgo(e.last_at, locale) }})
        </span>
      </p>
    </template>
  </div>
</template>

<style scoped>
.filters {
  display: flex;
  align-items: center;
  gap: 16px;
  margin-bottom: 14px;
  flex-wrap: wrap;
}
.segmented {
  display: flex;
}
.segmented .btn {
  border-radius: 0;
}
.segmented .btn:first-child {
  border-radius: var(--radius-sm) 0 0 var(--radius-sm);
}
.segmented .btn:last-child {
  border-radius: 0 var(--radius-sm) var(--radius-sm) 0;
}
.segmented .btn + .btn {
  margin-left: -1px;
}
.segmented .btn.active {
  background: var(--accent-soft);
  color: var(--accent);
  border-color: var(--accent);
  z-index: 1;
}
.total {
  display: flex;
  gap: 12px;
  margin: 0;
  font-size: 14px;
  font-weight: 600;
  flex-wrap: wrap;
}
.total .muted {
  font-weight: 400;
}
.list {
  padding: 4px 0;
}
.device + .device {
  border-top: 1px solid var(--border);
}
.row {
  display: grid;
  grid-template-columns: 16px auto minmax(0, 1fr) minmax(60px, 2fr) 84px 84px;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 10px 14px;
  border: 0;
  background: none;
  color: var(--text);
  font: inherit;
  text-align: left;
  cursor: pointer;
}
.row:hover {
  background: var(--surface-hover, var(--accent-soft));
}
.name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.bar {
  display: flex;
  height: 6px;
  overflow: hidden;
  border-radius: 3px;
  background: var(--border);
}
.bar.thin {
  height: 3px;
}
.bar i {
  display: block;
  height: 100%;
}
.bar .down {
  background: var(--accent);
}
.bar .up {
  background: var(--warning, #d29922);
}
.num {
  text-align: right;
  white-space: nowrap;
  font-size: 12.5px;
}
.dests {
  padding: 0 14px 12px 42px;
}
.dests ul {
  display: grid;
  gap: 4px;
  margin: 6px 0 8px;
  padding: 0;
  list-style: none;
}
.dests li {
  display: grid;
  grid-template-columns: minmax(0, 2fr) minmax(0, 1fr) minmax(40px, 1fr) 72px;
  align-items: center;
  gap: 10px;
  font-size: 13px;
}
.dest,
.svc {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.empty {
  display: grid;
  justify-items: center;
  gap: 8px;
  padding: 32px;
  text-align: center;
}
.empty p {
  margin: 0;
}
.empty-row {
  margin: 0;
  padding: 24px;
  text-align: center;
}
.small {
  font-size: 12.5px;
}
.foot {
  margin: 10px 0 0;
}
.exporters span + span::before {
  content: ' · ';
}
@media (max-width: 760px) {
  .row {
    grid-template-columns: 16px auto minmax(0, 1fr) 72px;
  }
  .row .bar,
  .row .num.muted {
    display: none;
  }
  .dests {
    padding-left: 14px;
  }
  .dests li {
    grid-template-columns: minmax(0, 1fr) 72px;
  }
  .dests li .svc,
  .dests li .bar {
    display: none;
  }
}
</style>
