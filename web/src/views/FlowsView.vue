<script setup lang="ts">
import { ArrowLeftRight, Search } from 'lucide-vue-next'
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'

import DeviceIcon from '@/components/DeviceIcon.vue'
import { api } from '@/lib/api'
import { formatAgo, formatBytes } from '@/lib/format'
import { displayName } from '@/lib/names'
import { serviceName } from '@/lib/services'
import type { Conversation, FlowsResponse, TopoNode } from '@/lib/types'

/** Who talks to whom: conversations from the flow exports. */
const { t, locale } = useI18n()
const route = useRoute()
const router = useRouter()

const RANGES = [15, 60, 360, 1440] as const
const minutes = ref<number>(60)
const query = ref('')
const data = ref<FlowsResponse>()
const nodes = ref(new Map<string, TopoNode>())
const loading = ref(true)
const node = computed(() => (typeof route.query.node === 'string' ? route.query.node : undefined))

async function load() {
  try {
    const [flows, topo] = await Promise.all([
      api.flows({ minutes: minutes.value, node: node.value }),
      nodes.value.size ? undefined : api.topology(),
    ])
    data.value = flows
    if (topo) nodes.value = new Map(topo.topology.nodes.map((n) => [n.id, n]))
  } finally {
    loading.value = false
  }
}
watch([minutes, node], load)

const nameOf = (id: string | undefined, ip: string) => {
  const n = id ? nodes.value.get(id) : undefined
  return n ? displayName(n, t) : ip
}
const rows = computed(() => {
  const q = query.value.trim().toLowerCase()
  return (data.value?.conversations ?? []).filter(
    (c) =>
      !q ||
      [
        c.a,
        c.b,
        nameOf(c.a_node, c.a),
        nameOf(c.b_node, c.b),
        ...c.ports.map((p) => serviceName(p.proto, p.port)),
      ]
        .join(' ')
        .toLowerCase()
        .includes(q),
  )
})
const total = (c: Conversation) => c.bytes_ab + c.bytes_ba
const max = computed(() => Math.max(1, ...rows.value.map(total)))

function open(id: string | undefined) {
  if (id) void router.push({ name: 'map', query: { node: id } })
}
function only(id: string | undefined) {
  void router.replace({ query: { ...route.query, node: id } })
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
        <h1>{{ t('flows.title') }}</h1>
        <p class="muted">{{ t('flows.subtitle') }}</p>
      </div>
    </header>

    <div class="filters">
      <label class="search">
        <Search :size="16" />
        <input v-model="query" class="input" type="search" :placeholder="t('flows.search')" />
      </label>
      <div class="segmented" role="group">
        <button
          v-for="r in RANGES"
          :key="r"
          class="btn small"
          :class="{ active: minutes === r }"
          :data-test="`flows-range-${r}`"
          @click="minutes = r"
        >
          {{ t(`flows.ranges.${r}`) }}
        </button>
      </div>
      <span v-if="node" class="chip" data-test="flows-node">
        {{ nameOf(node, node) }}
        <button class="link" type="button" @click="only(undefined)">×</button>
      </span>
    </div>

    <p v-if="loading" class="muted">{{ t('common.loading') }}</p>
    <div
      v-else-if="!data?.listening && !data?.conversations.length"
      class="card empty"
      data-test="flows-off"
    >
      <ArrowLeftRight :size="26" />
      <p>{{ t('flows.off') }}</p>
      <p class="muted small">{{ t('flows.offHint') }}</p>
      <RouterLink class="btn primary" :to="{ path: '/integrations', query: { add: 'flows' } }">
        {{ t('flows.enable') }}
      </RouterLink>
    </div>
    <template v-else>
      <div class="card table-wrap">
        <table class="table">
          <thead>
            <tr>
              <th>{{ t('flows.between') }}</th>
              <th class="num">{{ t('flows.traffic') }}</th>
              <th class="hide-sm">{{ t('flows.services') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="c in rows" :key="c.a + c.b" data-test="conversation">
              <td>
                <div class="pair">
                  <button
                    class="end"
                    type="button"
                    :title="c.a"
                    @click="c.a_node ? only(c.a_node) : undefined"
                  >
                    <DeviceIcon
                      v-if="c.a_node && nodes.get(c.a_node)"
                      :device="nodes.get(c.a_node)!"
                      :size="15"
                    />
                    <span>{{ nameOf(c.a_node, c.a) }}</span>
                  </button>
                  <ArrowLeftRight :size="13" class="muted" />
                  <button
                    class="end"
                    type="button"
                    :title="c.b"
                    @click="c.b_node ? only(c.b_node) : undefined"
                  >
                    <DeviceIcon
                      v-if="c.b_node && nodes.get(c.b_node)"
                      :device="nodes.get(c.b_node)!"
                      :size="15"
                    />
                    <span>{{ nameOf(c.b_node, c.b) }}</span>
                  </button>
                </div>
                <div class="bar"><i :style="{ width: `${(total(c) / max) * 100}%` }" /></div>
              </td>
              <td class="num mono">
                {{ formatBytes(total(c)) }}
                <div class="dirs muted">
                  → {{ formatBytes(c.bytes_ab) }} · ← {{ formatBytes(c.bytes_ba) }}
                </div>
              </td>
              <td class="hide-sm">
                <span v-for="p in c.ports" :key="`${p.proto}/${p.port}`" class="svc">{{
                  serviceName(p.proto, p.port)
                }}</span>
                <button
                  v-if="c.a_node || c.b_node"
                  class="link small map-link"
                  type="button"
                  @click="open(c.b_node ?? c.a_node)"
                >
                  {{ t('insights.onMap') }}
                </button>
              </td>
            </tr>
            <tr v-if="!rows.length">
              <td colspan="3" class="muted empty-row">{{ t('flows.none') }}</td>
            </tr>
          </tbody>
        </table>
      </div>
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
  gap: 12px;
  margin-bottom: 14px;
  flex-wrap: wrap;
}
.search {
  position: relative;
  display: flex;
  align-items: center;
  flex: 1;
  min-width: 200px;
  max-width: 320px;
}
.search svg {
  position: absolute;
  left: 10px;
  color: var(--text-muted);
}
.search .input {
  padding-left: 34px;
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
.table-wrap {
  overflow-x: auto;
}
.pair {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.end {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 0;
  border: 0;
  background: none;
  color: var(--text);
  font: inherit;
  cursor: pointer;
}
.end:hover span {
  text-decoration: underline;
}
.bar {
  height: 3px;
  margin-top: 6px;
  border-radius: 2px;
  background: var(--border);
}
.bar i {
  display: block;
  height: 100%;
  border-radius: 2px;
  background: var(--accent);
}
.num {
  text-align: right;
  white-space: nowrap;
}
.dirs {
  font-size: 11.5px;
}
.map-link {
  margin-left: 4px;
  font-size: 12px;
}
.svc {
  display: inline-block;
  margin: 0 4px 4px 0;
  padding: 1px 7px;
  border: 1px solid var(--border);
  border-radius: 999px;
  font-size: 11.5px;
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
  padding: 24px;
  text-align: center;
}
.small {
  font-size: 12.5px;
}
.exporters span + span::before {
  content: ' · ';
}
@media (max-width: 760px) {
  .hide-sm {
    display: none;
  }
}
</style>
