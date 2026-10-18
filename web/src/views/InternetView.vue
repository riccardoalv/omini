<script setup lang="ts">
import { ChevronDown, ChevronRight, Globe } from 'lucide-vue-next'
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'

import DeviceIcon from '@/components/DeviceIcon.vue'
import TrafficChart from '@/components/TrafficChart.vue'
import WorldMap from '@/components/WorldMap.vue'
import { api } from '@/lib/api'
import { countryName, flag } from '@/lib/flags'
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
  TrafficPoint,
} from '@/lib/types'

/**
 * The network's internet, from the flow exports: how much it uses over
 * time, where it goes in the world and to whom (the networks' owners), which
 * devices use it most, and each device's destinations.
 */
const { t, locale } = useI18n()
const router = useRouter()

const RANGES: InternetRange[] = ['15m', '1h', 'today', '7d', '30d']
const range = ref<InternetRange>('today')
const data = ref<InternetResponse>()
const nodes = ref(new Map<string, TopoNode>())
const loading = ref(true)
const country = ref<string>()
const listOpen = ref(false)
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
  country.value = undefined
  void load()
  if (open.value) void loadDests(open.value)
})

const empty = computed(() => !data.value?.devices.length)
const points = computed<TrafficPoint[]>(() => {
  const d = data.value
  if (!d) return []
  const step = Math.max(1, d.step_s)
  return d.series.map((p) => ({ at: p.at, rx_bps: (p.rx * 8) / step, tx_bps: (p.tx * 8) / step }))
})
const span = computed<[number, number] | undefined>(() => {
  const s = data.value?.series
  return s?.length ? [Date.parse(s[0]!.at), Date.now()] : undefined
})

const nameOf = (u: InternetUsage) => {
  const n = nodes.value.get(u.node)
  return n ? displayName(n, t) : u.label
}
const devices = computed(() => data.value?.devices ?? [])
const topDevices = computed(() => devices.value.slice(0, 8))
const deviceMax = computed(() => Math.max(1, ...devices.value.map((u) => u.rx + u.tx)))
const orgs = computed(() =>
  (data.value?.orgs ?? [])
    .filter((o) => !country.value || o.countries.includes(country.value))
    .slice(0, 8),
)
const orgMax = computed(() => Math.max(1, ...orgs.value.map((o) => o.rx + o.tx)))
/** Each person's use: the sum of their devices. */
const people = computed(() => data.value?.people ?? [])
const peopleMax = computed(() => Math.max(1, ...people.value.map((p) => p.rx + p.tx)))

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
/** A device picked in the ranking: its row in the full list, open. */
function showDevice(id: string) {
  listOpen.value = true
  if (open.value !== id) toggle(id)
  requestAnimationFrame(() =>
    document.querySelector(`[data-node="${CSS.escape(id)}"]`)?.scrollIntoView({ block: 'center' }),
  )
}
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
  <div class="page internet">
    <header class="page-header">
      <div>
        <h1>{{ t('internet.title') }}</h1>
        <p class="muted">{{ t('internet.subtitle') }}</p>
      </div>
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
    </header>

    <p v-if="loading" class="muted">{{ t('common.loading') }}</p>
    <div v-else-if="!data?.listening && empty" class="card empty" data-test="internet-off">
      <Globe :size="26" />
      <p>{{ t('flows.off') }}</p>
      <p class="muted small">{{ t('flows.offHint') }}</p>
      <RouterLink class="btn primary" :to="{ path: '/integrations', query: { add: 'flows' } }">
        {{ t('flows.enable') }}
      </RouterLink>
    </div>
    <template v-else-if="data">
      <section class="kpis" data-test="internet-kpis">
        <div class="card kpi">
          <span class="muted">{{ t('internet.now') }}</span>
          <strong
            ><span class="down">↓ {{ formatRate(data.now.rx_bps) }}</span>
            <span class="up">↑ {{ formatRate(data.now.tx_bps) }}</span></strong
          >
        </div>
        <div class="card kpi" data-test="internet-total">
          <span class="muted">{{ t(`internet.totals.${range}`) }}</span>
          <strong
            ><span class="down">↓ {{ formatBytes(data.total.rx) }}</span>
            <span class="up">↑ {{ formatBytes(data.total.tx) }}</span></strong
          >
        </div>
        <div class="card kpi">
          <span class="muted">{{ t('internet.devices') }}</span>
          <strong>{{ devices.length }}</strong>
        </div>
        <div class="card kpi">
          <span class="muted">{{ t('internet.countries') }}</span>
          <strong>{{ data.countries.length || '–' }}</strong>
        </div>
      </section>

      <section class="card block">
        <h2>{{ t('internet.usage') }}</h2>
        <TrafficChart :data="points" :span="span" />
      </section>

      <section class="card block" data-test="internet-world">
        <div class="block-head">
          <h2>{{ t('internet.world') }}</h2>
          <button
            v-if="country"
            class="chip"
            type="button"
            data-test="country-filter"
            @click="country = undefined"
          >
            {{ flag(country) }} {{ countryName(country, locale) }} ×
          </button>
        </div>
        <WorldMap
          v-if="data.attribution"
          :countries="data.countries"
          :home="data.home"
          :selected="country"
          @select="(c) => (country = c)"
        />
        <p v-else class="muted small" data-test="no-geo">{{ t('internet.noGeo') }}</p>
        <ul v-if="data.countries.length" class="chips">
          <li v-for="c in data.countries.slice(0, 8)" :key="c.country">
            <button
              type="button"
              class="chip"
              :class="{ on: country === c.country }"
              @click="country = country === c.country ? undefined : c.country"
            >
              {{ flag(c.country) }} {{ countryName(c.country, locale) }}
              <span class="muted">{{ formatBytes(c.rx + c.tx) }}</span>
            </button>
          </li>
        </ul>
      </section>

      <div class="pair">
        <section class="card block" data-test="internet-orgs">
          <h2>{{ t('internet.goesTo') }}</h2>
          <p v-if="!orgs.length" class="muted small">{{ t('internet.none') }}</p>
          <ul class="ranking">
            <li v-for="o in orgs" :key="o.org">
              <span class="label" :title="o.org">
                <span class="flags">{{ o.countries.slice(0, 2).map(flag).join(' ') }}</span>
                {{ o.org }}
              </span>
              <span class="bar"
                ><i class="down" :style="{ width: `${(o.rx / orgMax) * 100}%` }" /><i
                  class="up"
                  :style="{ width: `${(o.tx / orgMax) * 100}%` }"
              /></span>
              <span class="mono num">{{ formatBytes(o.rx + o.tx) }}</span>
            </li>
          </ul>
        </section>
        <section class="card block" data-test="internet-top">
          <h2>{{ t('internet.topDevices') }}</h2>
          <ul class="ranking">
            <li v-for="u in topDevices" :key="u.node">
              <button class="label link" type="button" @click="showDevice(u.node)">
                <DeviceIcon v-if="nodes.get(u.node)" :device="nodes.get(u.node)!" :size="14" />
                {{ nameOf(u) }}
              </button>
              <span class="bar"
                ><i class="down" :style="{ width: `${(u.rx / deviceMax) * 100}%` }" /><i
                  class="up"
                  :style="{ width: `${(u.tx / deviceMax) * 100}%` }"
              /></span>
              <span class="mono num">{{ formatBytes(u.rx + u.tx) }}</span>
            </li>
          </ul>
        </section>
      </div>

      <section v-if="people.length" class="card block" data-test="internet-people">
        <h2>{{ t('internet.people') }}</h2>
        <ul class="ranking">
          <li v-for="p in people" :key="p.id">
            <span class="label">{{ p.name }}</span>
            <span class="bar"
              ><i class="down" :style="{ width: `${(p.rx / peopleMax) * 100}%` }" /><i
                class="up"
                :style="{ width: `${(p.tx / peopleMax) * 100}%` }"
            /></span>
            <span class="mono num">{{ formatBytes(p.rx + p.tx) }}</span>
          </li>
        </ul>
      </section>

      <section class="card list">
        <button
          class="list-head"
          type="button"
          :aria-expanded="listOpen"
          data-test="internet-all"
          @click="listOpen = !listOpen"
        >
          <component :is="listOpen ? ChevronDown : ChevronRight" :size="16" />
          {{ t('internet.allDevices', { n: devices.length }) }}
        </button>
        <template v-if="listOpen">
          <div
            v-for="u in devices"
            :key="u.node"
            class="device"
            :data-node="u.node"
            data-test="internet-device"
          >
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
              <span class="mono num">↓ {{ formatBytes(u.rx) }}</span>
              <span class="mono num muted">↑ {{ formatBytes(u.tx) }}</span>
            </button>
            <div v-if="open === u.node" class="dests" data-test="internet-dests">
              <p v-if="!dests" class="muted small">{{ t('common.loading') }}</p>
              <p v-else-if="!dests.length" class="muted small">
                {{ t('internet.noDestinations') }}
              </p>
              <template v-else>
                <p class="muted small">
                  {{ t('internet.wentTo', { hours: destHours }, destHours) }}
                </p>
                <ul>
                  <li v-for="d in dests" :key="d.name">
                    <span class="dest" :title="d.org ? `${d.name} · ${d.org}` : d.name">
                      {{ flag(d.country) }} {{ d.name }}
                      <span v-if="d.org && d.org !== d.name" class="muted small">{{ d.org }}</span>
                    </span>
                    <span class="muted small svc">{{
                      d.ports
                        .slice(0, 2)
                        .map((p) => serviceName(p.proto, p.port))
                        .join(', ')
                    }}</span>
                    <span class="mono num">{{ formatBytes(d.rx + d.tx) }}</span>
                  </li>
                </ul>
              </template>
              <button v-if="u.on_map" class="link small" type="button" @click="showOnMap(u.node)">
                {{ t('insights.onMap') }}
              </button>
            </div>
          </div>
        </template>
      </section>

      <p class="muted small foot">
        {{ t('internet.namesHint') }}
        <template v-if="data.attribution">
          ·
          <a href="https://db-ip.com" target="_blank" rel="noopener">{{ data.attribution }}</a>
        </template>
      </p>
      <p v-if="data.exporters.length" class="muted small exporters" data-test="flows-exporters">
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
.internet {
  --down: #38bdf8;
  --up: #a78bfa;
  display: grid;
  gap: 14px;
  align-content: start;
}
.page-header {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 12px;
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
.kpis {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
}
.kpi {
  display: grid;
  gap: 4px;
  padding: 12px 14px;
  font-size: 12.5px;
}
.kpi strong {
  display: flex;
  gap: 10px;
  font-size: 18px;
  flex-wrap: wrap;
}
.down {
  color: var(--down);
}
.up {
  color: var(--up);
}
.block {
  padding: 14px 16px;
}
.block h2 {
  margin: 0 0 10px;
  font-size: 14px;
}
.block-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}
.chips {
  display: flex;
  gap: 6px;
  margin: 10px 0 0;
  padding: 0;
  list-style: none;
  flex-wrap: wrap;
}
.chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 3px 9px;
  border: 1px solid var(--border);
  border-radius: 999px;
  background: none;
  color: var(--text);
  font: inherit;
  font-size: 12px;
  cursor: pointer;
}
.chip.on {
  border-color: var(--accent);
  background: var(--accent-soft);
}
.pair {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
}
.ranking {
  display: grid;
  gap: 7px;
  margin: 0;
  padding: 0;
  list-style: none;
  font-size: 13px;
}
.ranking li {
  display: grid;
  grid-template-columns: minmax(0, 1.4fr) minmax(40px, 1fr) 72px;
  align-items: center;
  gap: 10px;
}
.label {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
  overflow: hidden;
  text-align: left;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.flags {
  flex: none;
}
.bar {
  display: flex;
  height: 6px;
  overflow: hidden;
  border-radius: 3px;
  background: var(--border);
}
.bar i {
  display: block;
  height: 100%;
}
.bar .down {
  background: var(--down);
}
.bar .up {
  background: var(--up);
}
.num {
  text-align: right;
  white-space: nowrap;
  font-size: 12.5px;
}
.list {
  padding: 4px 0;
}
.list-head {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 10px 14px;
  border: 0;
  background: none;
  color: var(--text);
  font: inherit;
  font-weight: 600;
  cursor: pointer;
}
.device {
  border-top: 1px solid var(--border);
}
.row {
  display: grid;
  grid-template-columns: 16px auto minmax(0, 1fr) 84px 84px;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 9px 14px;
  border: 0;
  background: none;
  color: var(--text);
  font: inherit;
  text-align: left;
  cursor: pointer;
}
.name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
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
  grid-template-columns: minmax(0, 2fr) minmax(0, 1fr) 72px;
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
.small {
  font-size: 12.5px;
}
.foot,
.exporters {
  margin: 0;
}
.exporters span + span::before {
  content: ' · ';
}
@media (max-width: 900px) {
  .kpis {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .pair {
    grid-template-columns: minmax(0, 1fr);
  }
}
@media (max-width: 560px) {
  .row {
    grid-template-columns: 16px auto minmax(0, 1fr) 72px;
  }
  .row .num.muted {
    display: none;
  }
  .dests {
    padding-left: 14px;
  }
  .dests li {
    grid-template-columns: minmax(0, 1fr) 72px;
  }
  .dests li .svc {
    display: none;
  }
}
</style>
