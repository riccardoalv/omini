<script setup lang="ts">
import { ArrowRight, Check, RefreshCw, X } from 'lucide-vue-next'
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { api, ApiError } from '@/lib/api'
import type { PluginUpdate, PluginUpdateResult } from '@/lib/types'

import ModalDialog from './ModalDialog.vue'

/**
 * "Update plugins" on the Integrations screen: the number of plugins with a
 * newer release; a click confirms the list, updates them one by one (on the
 * server) and shows each result. Hidden when no plugin can be updated
 * (only development or pinned plugins, or none).
 */
const emit = defineEmits<{
  /** Plugins were updated: integration types changed. */
  updated: []
}>()
const { t } = useI18n()

const updates = ref<PluginUpdate[]>([])
const open = ref(false)
const running = ref(false)
const results = ref<PluginUpdateResult[]>()
const error = ref('')

const pending = computed(() => updates.value.filter((u) => u.update))
/** Plugins that follow releases: the button shows for them. */
const tracked = computed(() =>
  updates.value.filter((u) => u.reason !== 'dev' && u.reason !== 'pinned'),
)
const nameOf = (id: string) => updates.value.find((u) => u.id === id)?.name ?? id

async function load() {
  try {
    updates.value = await api.pluginUpdates()
  } catch {
    updates.value = [] // GitHub or the server unreachable: no button
  }
}

function start() {
  results.value = undefined
  error.value = ''
  open.value = true
}

async function run() {
  running.value = true
  error.value = ''
  try {
    results.value = await api.updateAllPlugins()
    if (results.value.some((r) => !r.error)) emit('updated')
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : t('common.error')
  } finally {
    running.value = false
  }
  await load()
}

function close() {
  if (running.value) return
  open.value = false
}

onMounted(load)
defineExpose({ load })
</script>

<template>
  <button
    v-if="tracked.length"
    class="btn"
    type="button"
    data-test="update-plugins"
    :disabled="!pending.length"
    @click="start"
  >
    <RefreshCw :size="16" />
    {{
      pending.length
        ? t('plugins.updateAll.button', pending.length)
        : t('plugins.updateAll.upToDate')
    }}
  </button>

  <ModalDialog v-if="open" :title="t('plugins.updateAll.title')" @close="close">
    <template v-if="!results">
      <p class="muted">
        {{ running ? t('plugins.updateAll.running') : t('plugins.updateAll.confirm') }}
      </p>
      <ul class="list" data-test="update-plan">
        <li v-for="u in pending" :key="u.id">
          <RefreshCw v-if="running" :size="14" class="spin" />
          <strong>{{ u.name }}</strong>
          <span class="mono muted">{{ u.installed }}</span>
          <ArrowRight :size="13" class="muted" />
          <span class="mono">{{ u.latest }}</span>
        </li>
      </ul>
    </template>
    <template v-else>
      <p v-if="!results.length" class="muted">{{ t('plugins.updateAll.nothing') }}</p>
      <ul class="list" data-test="update-results">
        <li v-for="r in results" :key="r.id" :class="r.error ? 'failed' : 'done'">
          <X v-if="r.error" :size="14" />
          <Check v-else :size="14" />
          <strong>{{ nameOf(r.id) }}</strong>
          <span class="mono muted">{{ r.from }}</span>
          <ArrowRight :size="13" class="muted" />
          <span class="mono">{{ r.to }}</span>
          <span class="state">{{
            r.error ? t('plugins.updateAll.failed') : t('plugins.updateAll.done')
          }}</span>
          <p v-if="r.error" class="error-text">{{ r.error }}</p>
        </li>
      </ul>
    </template>
    <p v-if="error" class="alert error" role="alert">{{ error }}</p>

    <template #footer>
      <template v-if="!results">
        <button class="btn" type="button" :disabled="running" @click="close">
          {{ t('common.cancel') }}
        </button>
        <button
          class="btn primary"
          type="button"
          data-test="update-confirm"
          :disabled="running"
          @click="run"
        >
          <RefreshCw :size="15" :class="{ spin: running }" />
          {{ running ? t('plugins.updating') : t('plugins.updateAll.start') }}
        </button>
      </template>
      <button v-else class="btn primary" type="button" data-test="update-close" @click="close">
        {{ t('common.close') }}
      </button>
    </template>
  </ModalDialog>
</template>

<style scoped>
.list {
  list-style: none;
  margin: 12px 0 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.list li {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
}
.done {
  color: var(--ok);
}
.failed {
  color: var(--danger);
}
.list strong {
  color: var(--text);
}
.state {
  margin-left: auto;
  font-size: 12.5px;
}
.error-text {
  flex-basis: 100%;
  margin: 0 0 0 20px;
  font-size: 13px;
  overflow-wrap: anywhere;
}
.spin {
  animation: spin 0.8s linear infinite;
}
@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}
</style>
