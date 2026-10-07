<script setup lang="ts">
import { Pencil, Pin, Search, Trash2 } from 'lucide-vue-next'
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import NodeIcon from '@/components/NodeIcon.vue'
import { api } from '@/lib/api'
import { formatAgo, formatDateTime } from '@/lib/format'
import { iconFor } from '@/lib/graph'
import { displayName, filterInventory, isRandomMAC, type StatusFilter } from '@/lib/inventory'
import type { InventoryEntry } from '@/lib/types'

const { t, locale } = useI18n()

const entries = ref<InventoryEntry[]>([])
const loading = ref(true)
const query = ref('')
const status = ref<StatusFilter>('all')
const randomOnly = ref(false)
const selected = ref(new Set<string>())

// Segments are named by the server in English; translate unless the user renamed them.
const nameOf = (e: InventoryEntry) =>
  e.kind === 'segment' && !e.alias ? t('map.segment') : displayName(e)

const byId = computed(() => new Map(entries.value.map((e) => [e.id, e])))
const rows = computed(() =>
  filterInventory(entries.value, {
    query: query.value,
    status: status.value,
    randomOnly: randomOnly.value,
  }),
)
const allSelected = computed(
  () => rows.value.length > 0 && rows.value.every((r) => selected.value.has(r.id)),
)

async function load() {
  try {
    entries.value = await api.inventory()
  } finally {
    loading.value = false
  }
}

function toggle(id: string) {
  const next = new Set(selected.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  selected.value = next
}

function toggleAll() {
  selected.value = allSelected.value ? new Set() : new Set(rows.value.map((r) => r.id))
}

async function rename(e: InventoryEntry) {
  const alias = window.prompt(t('devices.renamePrompt'), e.alias || displayName(e))
  if (alias === null) return
  const updated = await api.updateInventory(e.id, { alias })
  entries.value = entries.value.map((x) => (x.id === e.id ? { ...updated, online: x.online } : x))
}

async function pin(e: InventoryEntry) {
  const updated = await api.updateInventory(e.id, { pinned: !e.pinned })
  entries.value = entries.value.map((x) => (x.id === e.id ? { ...updated, online: x.online } : x))
}

async function deleteSelected() {
  const ids = [...selected.value]
  if (!window.confirm(t('devices.confirmDelete', { n: ids.length }))) return
  await api.deleteInventory(ids)
  entries.value = entries.value.filter((e) => !selected.value.has(e.id))
  selected.value = new Set()
}

onMounted(load)
</script>

<template>
  <div class="page">
    <header class="page-header">
      <div>
        <h1>{{ t('devices.title') }}</h1>
        <p class="muted">{{ t('devices.subtitle') }}</p>
      </div>
    </header>

    <div class="filters">
      <label class="search">
        <Search :size="16" />
        <input v-model="query" class="input" type="search" :placeholder="t('common.search')" />
      </label>
      <div class="segmented" role="group">
        <button
          v-for="s in ['all', 'online', 'offline'] as const"
          :key="s"
          class="btn small"
          :class="{ active: status === s }"
          @click="status = s"
        >
          {{
            s === 'all'
              ? t('devices.all')
              : s === 'online'
                ? t('devices.onlyOnline')
                : t('devices.onlyOffline')
          }}
        </button>
      </div>
      <label class="checkbox">
        <input v-model="randomOnly" type="checkbox" />{{ t('devices.randomOnly') }}
      </label>
      <button v-if="selected.size" class="btn small danger" @click="deleteSelected">
        <Trash2 :size="14" />{{ t('devices.deleteSelected', { n: selected.size }) }}
      </button>
    </div>

    <p v-if="loading" class="muted">{{ t('common.loading') }}</p>
    <div v-else class="card table-wrap">
      <table class="table">
        <thead>
          <tr>
            <th><input type="checkbox" :checked="allSelected" @change="toggleAll" /></th>
            <th>{{ t('devices.name') }}</th>
            <th>{{ t('devices.ip') }}</th>
            <th class="hide-sm">{{ t('devices.mac') }}</th>
            <th class="hide-sm">{{ t('devices.attachedTo') }}</th>
            <th>{{ t('devices.lastSeen') }}</th>
            <th />
          </tr>
        </thead>
        <tbody>
          <tr v-for="e in rows" :key="e.id">
            <td>
              <input type="checkbox" :checked="selected.has(e.id)" @change="toggle(e.id)" />
            </td>
            <td>
              <div class="name">
                <span class="dot" :class="{ online: e.online }" />
                <NodeIcon
                  :name="
                    iconFor({
                      kind: e.kind,
                      role: e.role ?? (e.kind === 'client' ? 'client' : undefined),
                      random_mac: isRandomMAC(e.mac),
                    })
                  "
                  :size="15"
                />
                <span>{{ nameOf(e) }}</span>
                <Pin v-if="e.pinned" :size="12" class="muted" />
              </div>
            </td>
            <td class="mono">{{ e.ip }}</td>
            <td class="mono hide-sm">
              {{ e.mac }}
              <span v-if="isRandomMAC(e.mac)" class="badge">random</span>
            </td>
            <td class="hide-sm">
              <template v-if="e.parent_id">
                {{ byId.get(e.parent_id) ? nameOf(byId.get(e.parent_id)!) : '' }}
                <span v-if="e.port" class="muted"> · {{ e.port }}</span>
              </template>
            </td>
            <td :title="`${t('devices.firstSeen')}: ${formatDateTime(e.first_seen, locale)}`">
              {{ e.online ? t('common.online') : formatAgo(e.last_seen, locale) }}
            </td>
            <td class="actions">
              <button
                class="btn ghost icon small"
                :aria-label="t('panel.rename')"
                @click="rename(e)"
              >
                <Pencil :size="14" />
              </button>
              <button
                class="btn ghost icon small"
                :aria-label="e.pinned ? t('panel.unpin') : t('panel.pin')"
                @click="pin(e)"
              >
                <Pin :size="14" />
              </button>
            </td>
          </tr>
          <tr v-if="!rows.length">
            <td colspan="7" class="muted empty">{{ t('devices.empty') }}</td>
          </tr>
        </tbody>
      </table>
    </div>
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
.name {
  display: flex;
  align-items: center;
  gap: 8px;
}
.actions {
  white-space: nowrap;
  text-align: right;
}
.empty {
  text-align: center;
  padding: 24px;
}
@media (max-width: 760px) {
  .hide-sm {
    display: none;
  }
}
</style>
