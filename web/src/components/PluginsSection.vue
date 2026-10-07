<script setup lang="ts">
import { Store } from 'lucide-vue-next'
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'

import { api } from '@/lib/api'
import type { PluginInfo } from '@/lib/types'

import PluginStore from './PluginStore.vue'
import PluginTrust from './PluginTrust.vue'

/** Installed plugins at a glance; the store does the rest. */
const { t } = useI18n()
const router = useRouter()
const plugins = ref<PluginInfo[]>([])
const storeOpen = ref(false)

async function load() {
  try {
    plugins.value = await api.plugins()
  } catch {
    plugins.value = []
  }
}

function addIntegration(type: string) {
  storeOpen.value = false
  router.push({ path: '/integrations', query: { add: type } })
}

onMounted(load)
</script>

<template>
  <section class="card section">
    <div class="head">
      <h2>{{ t('plugins.title') }}</h2>
      <button class="btn small primary" data-test="open-store" @click="storeOpen = true">
        <Store :size="14" />{{ t('store.open') }}
      </button>
    </div>
    <p class="help">{{ t('plugins.hint') }}</p>
    <ul v-if="plugins.length" class="list">
      <li v-for="p in plugins" :key="p.manifest.id" data-test="plugin">
        <strong>{{ p.manifest.name }}</strong>
        <span class="muted">{{ p.source?.version ?? p.manifest.version }}</span>
        <span v-if="p.dev" class="badge">{{ t('plugins.dev') }}</span>
        <PluginTrust v-else :publisher="p.publisher" :trust="p.trust" />
      </li>
    </ul>
    <p v-else class="muted">{{ t('plugins.none') }}</p>
    <PluginStore
      v-if="storeOpen"
      @close="storeOpen = false"
      @changed="load"
      @add="addIntegration"
    />
  </section>
</template>

<style scoped>
.section {
  padding: 18px 20px 12px;
  margin-bottom: 14px;
}
.head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 6px;
}
.head h2 {
  margin: 0;
  font-size: 15px;
}
.help {
  margin: 0 0 8px;
}
.list {
  list-style: none;
  margin: 0;
  padding: 0;
}
.list li {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  padding: 8px 0;
  border-bottom: 1px solid var(--border);
}
</style>
