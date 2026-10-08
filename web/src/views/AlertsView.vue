<script setup lang="ts">
import {
  AlertOctagon,
  AlertTriangle,
  Eye,
  EyeOff,
  Info,
  LogIn,
  LogOut,
  MapPin,
  Sparkles,
} from 'lucide-vue-next'
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'

import { alertsState, alertText, refreshAlerts, SEVERITIES, sortAlerts } from '@/lib/alerts'
import { api } from '@/lib/api'
import { formatAgo } from '@/lib/format'
import { displayName as nodeName } from '@/lib/names'
import type { Alert, InventoryEntry, PresenceEvent, Severity } from '@/lib/types'

const { t, locale } = useI18n()
const route = useRoute()
const router = useRouter()

const tab = computed<'alerts' | 'timeline'>(() =>
  route.query.tab === 'timeline' ? 'timeline' : 'alerts',
)
function setTab(v: 'alerts' | 'timeline') {
  void router.replace({ query: { ...route.query, tab: v === 'alerts' ? undefined : v } })
}

// --- alerts ---
const severity = ref<Severity | 'all'>('all')
const showDismissed = ref(false)
const showResolved = ref(false)
const resolved = ref<Alert[]>([])

async function loadResolved() {
  if (!showResolved.value) return
  try {
    resolved.value = (await api.alerts(168)).filter((a) => a.resolved_at)
  } catch {
    resolved.value = []
  }
}
watch(showResolved, loadResolved)

const shown = computed(() => {
  const list = [...alertsState.list, ...(showResolved.value ? resolved.value : [])]
  return sortAlerts(
    list.filter(
      (a) =>
        (severity.value === 'all' || a.severity === severity.value) &&
        (showDismissed.value || !a.dismissed),
    ),
  )
})
const counts = computed(() => {
  const out: Record<Severity, number> = { critical: 0, warning: 0, info: 0 }
  for (const a of alertsState.list) if (!a.dismissed) out[a.severity]++
  return out
})
const dismissedCount = computed(() => alertsState.list.filter((a) => a.dismissed).length)

const icons = { critical: AlertOctagon, warning: AlertTriangle, info: Info }

async function toggleDismiss(a: Alert) {
  await api.dismissAlert(a.id, !a.dismissed)
  alertsState.list = alertsState.list.map((x) =>
    x.id === a.id ? { ...x, dismissed: !a.dismissed } : x,
  )
}

function showOnMap(nodeId: string) {
  void router.push({ name: 'map', query: { node: nodeId } })
}

// --- timeline ---
const PAGE = 100
const events = ref<PresenceEvent[]>([])
const newOnly = ref(false)
const more = ref(true)
const loadingEvents = ref(false)

async function loadEvents(reset = false) {
  loadingEvents.value = true
  try {
    const before = reset ? undefined : events.value[events.value.length - 1]?.id
    const page = await api.presence({ before, limit: PAGE, first: newOnly.value })
    events.value = reset ? page : [...events.value, ...page]
    more.value = page.length === PAGE
  } finally {
    loadingEvents.value = false
  }
}
watch(newOnly, () => loadEvents(true))

const dayFormat = computed(
  () => new Intl.DateTimeFormat(locale.value, { weekday: 'long', day: 'numeric', month: 'long' }),
)
const timeFormat = computed(
  () => new Intl.DateTimeFormat(locale.value, { hour: '2-digit', minute: '2-digit' }),
)
function dayName(iso: string): string {
  const d = new Date(iso)
  const today = new Date()
  const yesterday = new Date(Date.now() - 86400_000)
  const same = (a: Date, b: Date) => a.toDateString() === b.toDateString()
  if (same(d, today)) return t('insights.today')
  if (same(d, yesterday)) return t('insights.yesterday')
  return dayFormat.value.format(d)
}
const days = computed(() => {
  const out: { day: string; events: PresenceEvent[] }[] = []
  for (const e of events.value) {
    const day = dayName(e.at)
    const last = out[out.length - 1]
    if (last?.day === day) last.events.push(e)
    else out.push({ day, events: [e] })
  }
  return out
})
// Devices are named as on the other screens (the user's name, else the identified one).
const inventory = ref(new Map<string, InventoryEntry>())
async function loadInventory() {
  try {
    inventory.value = new Map((await api.inventory()).map((e) => [e.id, e]))
  } catch {
    // names fall back to the event's
  }
}
function deviceName(e: PresenceEvent): string {
  const entry = inventory.value.get(e.node_id)
  if (entry?.alias) return entry.alias
  if (entry && entry.kind !== 'segment')
    return nodeName({ ...entry, type: entry.device_type || entry.type }, t)
  return e.label || e.mac || e.node_id
}

function eventText(e: PresenceEvent): string {
  if (e.first) return t('insights.firstSeen')
  return e.kind === 'join' ? t('insights.joined') : t('insights.left')
}

let timer: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  void refreshAlerts()
  void loadEvents(true)
  void loadInventory()
  timer = setInterval(() => {
    void refreshAlerts()
    void loadResolved()
  }, 30_000)
})
onBeforeUnmount(() => clearInterval(timer))
</script>

<template>
  <div class="page">
    <header class="page-header">
      <div>
        <h1>{{ t('insights.title') }}</h1>
        <p class="muted">{{ t('insights.subtitle') }}</p>
      </div>
    </header>

    <div class="tabs" role="tablist">
      <button
        role="tab"
        class="tab"
        data-test="tab-alerts"
        :aria-selected="tab === 'alerts'"
        @click="setTab('alerts')"
      >
        {{ t('insights.alerts') }}
        <span v-if="counts.critical + counts.warning" class="count">{{
          counts.critical + counts.warning
        }}</span>
      </button>
      <button
        role="tab"
        class="tab"
        data-test="tab-timeline"
        :aria-selected="tab === 'timeline'"
        @click="setTab('timeline')"
      >
        {{ t('insights.timeline') }}
      </button>
    </div>

    <template v-if="tab === 'alerts'">
      <div class="filters">
        <div class="segmented" role="group">
          <button
            class="btn small"
            :class="{ active: severity === 'all' }"
            data-test="sev-all"
            @click="severity = 'all'"
          >
            {{ t('insights.all') }}
          </button>
          <button
            v-for="s in SEVERITIES"
            :key="s"
            class="btn small"
            :class="{ active: severity === s }"
            :data-test="`sev-${s}`"
            @click="severity = s"
          >
            {{ t(`insights.severity.${s}`) }} ({{ counts[s] }})
          </button>
        </div>
        <label v-if="dismissedCount" class="checkbox">
          <input v-model="showDismissed" type="checkbox" />{{ t('insights.showDismissed') }} ({{
            dismissedCount
          }})
        </label>
        <label class="checkbox">
          <input v-model="showResolved" type="checkbox" data-test="show-resolved" />{{
            t('insights.showResolved')
          }}
        </label>
      </div>

      <p v-if="alertsState.loaded && !shown.length" class="card empty muted" data-test="no-alerts">
        {{ t('insights.none') }}
      </p>
      <ul class="alerts">
        <li
          v-for="a in shown"
          :key="a.id"
          class="card alert-row"
          :class="[a.severity, { dismissed: a.dismissed, resolved: a.resolved_at }]"
          data-test="alert"
        >
          <component :is="icons[a.severity]" :size="18" class="sev-icon" />
          <div class="grow">
            <strong>{{ alertText(a, t, locale).title }}</strong>
            <p class="detail">{{ alertText(a, t, locale).detail }}</p>
            <span class="meta">
              <span class="sev">{{ t(`insights.severity.${a.severity}`) }}</span>
              ·
              <template v-if="a.resolved_at">{{
                t('insights.resolved', { ago: formatAgo(a.resolved_at, locale) })
              }}</template>
              <template v-else>{{
                t('insights.since', { ago: formatAgo(a.opened_at, locale) })
              }}</template>
              <template v-if="a.dismissed"> · {{ t('insights.dismissed') }}</template>
            </span>
          </div>
          <div class="row-actions">
            <button
              v-if="a.node_id"
              class="btn ghost icon small"
              :title="t('insights.onMap')"
              :aria-label="t('insights.onMap')"
              data-test="alert-map"
              @click="showOnMap(a.node_id)"
            >
              <MapPin :size="15" />
            </button>
            <button
              v-if="!a.resolved_at"
              class="btn ghost icon small"
              :title="a.dismissed ? t('insights.undismiss') : t('insights.dismiss')"
              :aria-label="a.dismissed ? t('insights.undismiss') : t('insights.dismiss')"
              data-test="alert-dismiss"
              @click="toggleDismiss(a)"
            >
              <component :is="a.dismissed ? Eye : EyeOff" :size="15" />
            </button>
          </div>
        </li>
      </ul>
    </template>

    <template v-else>
      <div class="filters">
        <label class="checkbox">
          <input v-model="newOnly" type="checkbox" data-test="new-only" />{{
            t('insights.newOnly')
          }}
        </label>
      </div>
      <p v-if="!loadingEvents && !events.length" class="card empty muted" data-test="no-events">
        {{ t('insights.noEvents') }}
      </p>
      <section v-for="d in days" :key="d.day" class="day">
        <h3>{{ d.day }}</h3>
        <ul class="card events">
          <li
            v-for="e in d.events"
            :key="e.id"
            class="event"
            :class="e.first ? 'first' : e.kind"
            data-test="event"
            @click="showOnMap(e.node_id)"
          >
            <span class="time mono">{{ timeFormat.format(new Date(e.at)) }}</span>
            <component
              :is="e.first ? Sparkles : e.kind === 'join' ? LogIn : LogOut"
              :size="15"
              class="kind"
            />
            <span class="grow">
              <strong>{{ deviceName(e) }}</strong>
              {{ eventText(e) }}
            </span>
            <span class="muted mono hide-sm">{{ e.ip }}</span>
          </li>
        </ul>
      </section>
      <button
        v-if="more && events.length"
        class="btn"
        :disabled="loadingEvents"
        data-test="more-events"
        @click="loadEvents()"
      >
        {{ t('insights.loadMore') }}
      </button>
    </template>
  </div>
</template>

<style scoped>
.tabs {
  display: flex;
  gap: 4px;
  margin-bottom: 16px;
  border-bottom: 1px solid var(--border);
}
.tab {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 14px;
  border: 0;
  border-bottom: 2px solid transparent;
  background: none;
  color: var(--text-muted);
  font-size: 14px;
  cursor: pointer;
}
.tab[aria-selected='true'] {
  border-bottom-color: var(--accent);
  color: var(--text);
  font-weight: 600;
}
.count {
  min-width: 18px;
  padding: 0 5px;
  border-radius: 999px;
  background: var(--danger);
  color: #fff;
  font-size: 11px;
  line-height: 18px;
  text-align: center;
}
.filters {
  display: flex;
  align-items: center;
  gap: 14px;
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
.empty {
  padding: 24px;
  text-align: center;
}
.alerts {
  display: grid;
  gap: 8px;
  margin: 0;
  padding: 0;
  list-style: none;
}
.alert-row {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  padding: 12px 14px;
  border-left: 3px solid var(--sev);
}
.alert-row.critical {
  --sev: var(--danger);
}
.alert-row.warning {
  --sev: var(--warn);
}
.alert-row.info {
  --sev: var(--accent);
}
.alert-row.dismissed,
.alert-row.resolved {
  opacity: 0.6;
}
.sev-icon {
  flex: none;
  margin-top: 1px;
  color: var(--sev);
}
.grow {
  flex: 1;
  min-width: 0;
}
.detail {
  margin: 2px 0 4px;
  color: var(--text-muted);
  font-size: 13px;
  overflow-wrap: anywhere;
}
.meta {
  color: var(--text-muted);
  font-size: 12px;
}
.sev {
  color: var(--sev);
  font-weight: 600;
}
.row-actions {
  display: flex;
  gap: 2px;
}
.day h3 {
  margin: 18px 0 8px;
  color: var(--text-muted);
  font-size: 12.5px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.03em;
}
.events {
  margin: 0;
  padding: 4px 0;
  list-style: none;
}
.event {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 7px 14px;
  font-size: 13.5px;
  cursor: pointer;
}
.event:hover {
  background: var(--bg-hover, rgba(127, 127, 127, 0.08));
}
.time {
  flex: none;
  width: 44px;
  color: var(--text-muted);
  font-size: 12px;
}
.kind {
  flex: none;
}
.event.join .kind {
  color: var(--ok);
}
.event.leave .kind {
  color: var(--text-muted);
}
.event.first .kind {
  color: var(--accent);
}
.btn[data-test='more-events'] {
  margin-top: 14px;
}
@media (max-width: 760px) {
  .hide-sm {
    display: none;
  }
}
</style>
