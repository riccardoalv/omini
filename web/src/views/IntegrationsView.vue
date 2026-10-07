<script setup lang="ts">
import { ChevronDown, Plus, Store, Trash2 } from 'lucide-vue-next'
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'

import IntegrationDetails from '@/components/IntegrationDetails.vue'
import IntegrationForm from '@/components/IntegrationForm.vue'
import ModalDialog from '@/components/ModalDialog.vue'
import PluginStore from '@/components/PluginStore.vue'
import ToggleSwitch from '@/components/ToggleSwitch.vue'
import { api } from '@/lib/api'
import { formatAgo } from '@/lib/format'
import type { CollectionStatus, Config, Integration, IntegrationType } from '@/lib/types'

const { t, locale } = useI18n()
const route = useRoute()
const router = useRouter()

const items = ref<Integration[]>([])
const types = ref<IntegrationType[]>([])
const loading = ref(true)
const choosing = ref(false)
const form = ref<{
  type: IntegrationType
  existing?: Integration
  prefill?: Config
}>()

const typeByName = computed(() => new Map(types.value.map((x) => [x.type, x])))

async function load() {
  try {
    ;[items.value, types.value] = await Promise.all([api.integrations(), api.integrationTypes()])
  } finally {
    loading.value = false
  }
}

// The plugin store; "Add integration" from it opens that plugin's form.
const storeOpen = ref(false)
function addFromStore(type: string) {
  storeOpen.value = false
  const found = types.value.find((x) => x.type === type)
  if (found) openAdd(found)
}
function openStore() {
  choosing.value = false
  storeOpen.value = true
}

function openAdd(type: IntegrationType) {
  choosing.value = false
  form.value = { type }
}

const expanded = ref<number>()

function toggleExpand(item: Integration) {
  expanded.value = expanded.value === item.id ? undefined : item.id
}

function onRan(item: Integration, status: CollectionStatus) {
  items.value = items.value.map((i) => (i.id === item.id ? { ...i, status } : i))
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
  else if (add) choosing.value = true
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
        <button class="btn" data-test="store" @click="storeOpen = true">
          <Store :size="16" />{{ t('store.title') }}
        </button>
        <button class="btn primary" @click="choosing = true">
          <Plus :size="16" />{{ t('integrations.add') }}
        </button>
      </div>
    </header>

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
          <span
            class="dot"
            :class="{
              online: item.enabled && item.status?.ok,
              error: item.enabled && item.status && !item.status.ok,
            }"
          />
          <div class="info">
            <div class="name-row">
              <strong>{{ item.name }}</strong>
              <span class="badge">{{ typeByName.get(item.type)?.name ?? item.type }}</span>
              <span v-if="item.config.host" class="muted mono">{{ item.config.host }}</span>
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
          @ran="(st) => onRan(item, st)"
        />
      </li>
    </ul>

    <ModalDialog
      v-if="choosing"
      :title="t('integrations.chooseType')"
      wide
      @close="choosing = false"
    >
      <div class="types">
        <button v-for="type in types" :key="type.type" class="card type" @click="openAdd(type)">
          <strong>{{ type.name }}</strong>
          <span class="badge">{{
            type.kind === 'core' ? t('integrations.core') : t('integrations.plugin')
          }}</span>
          <span class="muted">{{ type.description }}</span>
        </button>
        <button class="card type more" data-test="open-store" @click="openStore">
          <Store :size="20" />
          <strong>{{ t('integrations.moreInStore') }}</strong>
          <span class="muted">{{ t('integrations.moreInStoreHint') }}</span>
        </button>
      </div>
    </ModalDialog>

    <PluginStore v-if="storeOpen" @close="storeOpen = false" @changed="load" @add="addFromStore" />

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
  padding: 14px 12px 14px 18px;
  cursor: pointer;
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
  font-size: 13px;
  margin-top: 2px;
}
.error-text {
  margin: 6px 0 0;
  color: var(--danger);
  font-size: 13px;
}
.types {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(190px, 1fr));
  gap: 10px;
}
.type {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 6px;
  padding: 14px;
  text-align: left;
  color: var(--text);
  font: inherit;
  cursor: pointer;
}
.type:hover {
  border-color: var(--accent);
}
.type .muted {
  font-size: 12.5px;
}
.type.more {
  border-style: dashed;
  color: var(--text-muted);
}
.type.more strong {
  color: var(--text);
}
</style>
