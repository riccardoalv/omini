<script setup lang="ts">
import { ChevronDown, Plus, Trash2 } from 'lucide-vue-next'
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'

import IntegrationDetails from '@/components/IntegrationDetails.vue'
import IntegrationForm from '@/components/IntegrationForm.vue'
import IntegrationLogo from '@/components/IntegrationLogo.vue'
import CollectionRound from '@/components/CollectionRound.vue'
import MoveAddressButton from '@/components/MoveAddressButton.vue'
import PluginStore from '@/components/PluginStore.vue'
import ToggleSwitch from '@/components/ToggleSwitch.vue'
import UpdatePlugins from '@/components/UpdatePlugins.vue'
import { alertsState, moved } from '@/lib/alerts'
import { api } from '@/lib/api'
import { formatAgo } from '@/lib/format'
import type { Alert, Config, Integration, IntegrationType } from '@/lib/types'

const { t, locale } = useI18n()
const route = useRoute()
const router = useRouter()

const items = ref<Integration[]>([])
const types = ref<IntegrationType[]>([])
const loading = ref(true)
const form = ref<{
  type: IntegrationType
  existing?: Integration
  prefill?: Config
}>()

const typeByName = computed(() => new Map(types.value.map((x) => [x.type, x])))
// Logos come from the store catalog (by plugin id = integration type).
const logos = ref<Record<string, string | undefined>>({})

async function load() {
  try {
    ;[items.value, types.value] = await Promise.all([api.integrations(), api.integrationTypes()])
  } finally {
    loading.value = false
  }
  try {
    logos.value = Object.fromEntries((await api.pluginCatalog()).map((e) => [e.id, e.icon]))
  } catch {
    // logos are a nicety
  }
}

// "Add integration" opens the store: built-in integrations, installed and
// available plugins. Picking one there opens its form.
// The "Update plugins" button: refreshed when the store changes plugins.
const updater = ref<InstanceType<typeof UpdatePlugins>>()
function onPluginsChanged() {
  void load()
  void updater.value?.load()
}

const storeOpen = ref(false)
function addFromStore(type: string) {
  storeOpen.value = false
  const found = types.value.find((x) => x.type === type)
  if (found) openAdd(found)
}

function openAdd(type: IntegrationType) {
  form.value = { type }
}

// A failing integration whose device answers at another address now (DHCP
// gave it another one): its open alert says where, with the button.
const movedAlerts = computed(() => {
  const out = new Map<number, Alert>()
  for (const a of alertsState.list) {
    const m = moved(a)
    if (m && !out.has(m.integration)) out.set(m.integration, a)
  }
  return out
})

const expanded = ref<number>()

function toggleExpand(item: Integration) {
  expanded.value = expanded.value === item.id ? undefined : item.id
}

function onDetailsSaved(updated: Integration) {
  items.value = items.value.map((i) => (i.id === updated.id ? { ...updated, status: i.status } : i))
  setTimeout(load, 2000) // the new settings apply on the next collection
}

const toggling = ref<number>()

async function toggle(item: Integration) {
  toggling.value = item.id
  try {
    const updated = await api.updateIntegration(item.id, { enabled: !item.enabled })
    items.value = items.value.map((i) => (i.id === item.id ? { ...updated, status: i.status } : i))
  } finally {
    toggling.value = undefined
  }
}

async function remove(item: Integration) {
  if (!window.confirm(t('integrations.confirmDelete', { name: item.name }))) return
  await api.deleteIntegration(item.id)
  items.value = items.value.filter((i) => i.id !== item.id)
}

async function onSaved() {
  form.value = undefined
  await load()
  // The first collection takes a moment; refresh statuses shortly after.
  setTimeout(load, 2000)
}

onMounted(async () => {
  await load()
  const add = route.query.add
  const type = typeof add === 'string' ? types.value.find((x) => x.type === add) : undefined
  if (type) openAdd(type)
  else if (add) storeOpen.value = true
  if (route.query.store) storeOpen.value = true
  if (add || route.query.store) router.replace({ query: {} })
})
</script>

<template>
  <div class="page">
    <header class="page-header">
      <div>
        <h1>{{ t('integrations.title') }}</h1>
        <p class="muted">{{ t('integrations.subtitle') }}</p>
      </div>
      <div class="header-actions">
        <UpdatePlugins ref="updater" @updated="load" />
        <button class="btn primary" data-test="add" @click="storeOpen = true">
          <Plus :size="16" />{{ t('integrations.add') }}
        </button>
      </div>
    </header>

    <CollectionRound v-if="items.length" :integrations="items" @ran="load" />

    <p v-if="loading" class="muted">{{ t('common.loading') }}</p>
    <p v-else-if="!items.length" class="card empty muted">{{ t('integrations.empty') }}</p>

    <ul v-else class="list">
      <li
        v-for="item in items"
        :key="item.id"
        class="card item"
        :class="{ open: expanded === item.id }"
      >
        <div
          class="row"
          role="button"
          tabindex="0"
          :aria-expanded="expanded === item.id"
          data-test="integration-row"
          @click="toggleExpand(item)"
          @keydown.enter.self="toggleExpand(item)"
        >
          <span class="logo-tile">
            <IntegrationLogo :type="item.type" :icon="logos[item.type]" :size="22" />
            <span
              class="dot"
              :class="{
                online: item.enabled && item.status?.ok,
                error: item.enabled && item.status && !item.status.ok,
              }"
            />
          </span>
          <div class="info">
            <div class="name-row">
              <strong>{{ item.name }}</strong>
              <span v-if="item.config.host" class="muted mono host">{{ item.config.host }}</span>
            </div>
            <div class="muted status">
              <template v-if="!item.enabled">{{ t('integrations.disabled') }}</template>
              <template v-else-if="!item.status">{{ t('integrations.neverCollected') }}</template>
              <template v-else>
                {{
                  t('integrations.lastCollected', {
                    ago: formatAgo(item.status.collected_at, locale),
                  })
                }}
                · {{ t('integrations.deviceCount', item.status.devices) }}
              </template>
            </div>
            <p v-if="item.enabled && item.status?.error" class="error-text">
              {{ item.status.error }}
            </p>
            <p
              v-if="item.enabled && movedAlerts.get(item.id)"
              class="moved-row"
              data-test="integration-moved"
            >
              <span>{{
                t('moved.answersAt', { to: movedAlerts.get(item.id)!.params.moved_to })
              }}</span>
              <MoveAddressButton :alert="movedAlerts.get(item.id)!" @moved="load" />
            </p>
          </div>
          <ToggleSwitch
            :model-value="item.enabled"
            :label="item.enabled ? t('integrations.enabled') : t('integrations.disabled')"
            :disabled="toggling === item.id"
            @click.stop
            @update:model-value="toggle(item)"
          />
          <button
            class="btn ghost icon danger"
            :aria-label="t('common.delete')"
            @click.stop="remove(item)"
          >
            <Trash2 :size="16" />
          </button>
          <ChevronDown :size="18" class="chevron" />
        </div>
        <IntegrationDetails
          v-if="expanded === item.id && typeByName.get(item.type)"
          :integration="item"
          :type="typeByName.get(item.type)!"
          @saved="onDetailsSaved"
        />
      </li>
    </ul>

    <PluginStore
      v-if="storeOpen"
      @close="storeOpen = false"
      @changed="onPluginsChanged"
      @add="addFromStore"
    />

    <IntegrationForm
      v-if="form"
      :type="form.type"
      :existing="form.existing"
      :prefill="form.prefill"
      @close="form = undefined"
      @saved="onSaved"
    />
  </div>
</template>

<style scoped>
.header-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
.empty {
  padding: 28px;
  text-align: center;
}
.list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.item {
  padding: 0;
  overflow: hidden;
}
.row {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 12px 12px 12px 14px;
  cursor: pointer;
}
/* The integration's logo, with its status as a dot in the corner. */
.logo-tile {
  position: relative;
  display: grid;
  place-items: center;
  width: 44px;
  height: 44px;
  flex: none;
  border: 1px solid var(--border);
  border-radius: 10px;
  background: var(--surface-hover);
}
.logo-tile .dot {
  position: absolute;
  right: -3px;
  bottom: -3px;
  width: 11px;
  height: 11px;
  box-shadow: 0 0 0 2px var(--bg-elevated);
}
.name-row strong {
  font-size: 15px;
}
.host {
  font-size: 12.5px;
}
.row:hover {
  background: var(--surface-hover);
}
.chevron {
  color: var(--text-muted);
  transition: transform 0.15s;
}
.open .chevron {
  transform: rotate(180deg);
}
.info {
  flex: 1;
  min-width: 0;
}
.name-row {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.status {
  font-size: 12.5px;
  margin-top: 3px;
}
.error-text {
  margin: 6px 0 0;
  color: var(--danger);
  font-size: 13px;
}
.moved-row {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  margin: 6px 0 0;
  font-size: 13px;
}
</style>
