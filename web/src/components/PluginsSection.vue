<script setup lang="ts">
import { Download, RefreshCw, Trash2 } from 'lucide-vue-next'
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { api, ApiError } from '@/lib/api'
import type { CatalogEntry, PluginInfo } from '@/lib/types'

import PluginTrust from './PluginTrust.vue'

const { t } = useI18n()

const plugins = ref<PluginInfo[]>([])
const catalog = ref<CatalogEntry[]>([])
const url = ref('')
const version = ref('')
const busy = ref<string>() // id or url being installed
const error = ref('')
const installed = ref('')
const confirmRemove = ref<string>()

const available = computed(() => catalog.value.filter((e) => !e.installed))
const message = (e: unknown) => (e instanceof ApiError ? e.message : t('common.error'))

async function load() {
  try {
    ;[plugins.value, catalog.value] = await Promise.all([api.plugins(), api.pluginCatalog()])
  } catch (e) {
    error.value = message(e)
  }
}

async function install(address: string, ver?: string, key = address) {
  busy.value = key
  error.value = ''
  installed.value = ''
  try {
    const p = await api.installPlugin(address.trim(), ver?.trim() || undefined)
    installed.value = t('plugins.installed', { name: p.manifest.name, v: p.manifest.version })
    if (address === url.value) {
      url.value = ''
      version.value = ''
    }
    await load()
  } catch (e) {
    error.value = message(e)
  } finally {
    busy.value = undefined
  }
}

// Removing takes a second click (no blocking dialog).
async function remove(p: PluginInfo) {
  if (confirmRemove.value !== p.manifest.id) {
    confirmRemove.value = p.manifest.id
    return
  }
  confirmRemove.value = undefined
  error.value = ''
  try {
    await api.removePlugin(p.manifest.id)
    await load()
  } catch (e) {
    error.value = message(e)
  }
}

onMounted(load)
</script>

<template>
  <section class="card section">
    <h2>{{ t('plugins.title') }}</h2>
    <p class="help">{{ t('plugins.hint') }}</p>

    <ul v-if="plugins.length" class="list">
      <li v-for="p in plugins" :key="p.manifest.id" data-test="plugin">
        <div class="grow">
          <div class="line">
            <strong>{{ p.manifest.name }}</strong>
            <span class="muted">{{ p.source?.version ?? p.manifest.version }}</span>
            <span v-if="p.dev" class="badge">{{ t('plugins.dev') }}</span>
            <PluginTrust v-else :publisher="p.publisher" :trust="p.trust" />
          </div>
          <div v-if="p.source" class="muted small">
            <a :href="p.source.url" target="_blank" rel="noopener noreferrer">{{ p.source.url }}</a>
          </div>
        </div>
        <template v-if="!p.dev && p.source">
          <button
            class="btn small"
            data-test="update"
            :disabled="!!busy"
            @click="install(p.source.url, undefined, p.manifest.id)"
          >
            <RefreshCw :size="14" :class="{ spin: busy === p.manifest.id }" />
            {{ busy === p.manifest.id ? t('plugins.updating') : t('plugins.update') }}
          </button>
          <button class="btn small danger" data-test="remove" @click="remove(p)">
            <Trash2 :size="14" />
            {{ confirmRemove === p.manifest.id ? t('plugins.confirmRemove') : t('plugins.remove') }}
          </button>
        </template>
      </li>
    </ul>
    <p v-else class="muted">{{ t('plugins.none') }}</p>

    <template v-if="available.length">
      <h3>{{ t('plugins.available') }}</h3>
      <ul class="list">
        <li v-for="e in available" :key="e.id" data-test="catalog-entry">
          <div class="grow">
            <div class="line">
              <strong>{{ e.name }}</strong>
              <PluginTrust :publisher="e.publisher" :trust="e.trust" />
            </div>
            <div class="muted small">{{ e.description }}</div>
          </div>
          <button
            class="btn small primary"
            data-test="install-catalog"
            :disabled="!!busy"
            @click="install(e.url, undefined, e.id)"
          >
            <Download :size="14" />
            {{ busy === e.id ? t('plugins.installing') : t('plugins.install') }}
          </button>
        </li>
      </ul>
    </template>

    <h3>{{ t('plugins.fromUrl') }}</h3>
    <form class="install" @submit.prevent="install(url, version)">
      <div class="field grow">
        <label for="p-url">{{ t('plugins.url') }}</label>
        <input
          id="p-url"
          v-model="url"
          class="input"
          type="url"
          required
          placeholder="https://github.com/user/omini-plugin-…"
        />
      </div>
      <div class="field">
        <label for="p-version">{{ t('plugins.version') }}</label>
        <input
          id="p-version"
          v-model="version"
          class="input short"
          :placeholder="t('plugins.latest')"
        />
      </div>
      <button class="btn primary" type="submit" data-test="install" :disabled="!!busy || !url">
        <Download :size="15" />
        {{ busy === url && url ? t('plugins.installing') : t('plugins.install') }}
      </button>
    </form>
    <p v-if="installed" class="alert ok" role="status">{{ installed }}</p>
    <p v-if="error" class="alert error" role="alert">{{ error }}</p>
  </section>
</template>

<style scoped>
.section {
  padding: 18px 20px 6px;
  margin-bottom: 14px;
}
.section h2 {
  font-size: 15px;
  margin-bottom: 6px;
}
.section h3 {
  margin: 16px 0 6px;
  font-size: 12.5px;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.03em;
}
.list {
  list-style: none;
  margin: 0 0 6px;
  padding: 0;
}
.list li {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 0;
  border-bottom: 1px solid var(--border);
}
.line {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.grow {
  flex: 1;
  min-width: 0;
}
.small {
  margin-top: 3px;
  font-size: 12.5px;
  overflow: hidden;
  text-overflow: ellipsis;
}
.install {
  display: flex;
  align-items: flex-end;
  gap: 8px;
  flex-wrap: wrap;
}
.install .btn {
  margin-bottom: 14px;
}
.short {
  max-width: 130px;
}
.help {
  margin: 0 0 8px;
}
.alert {
  margin: 0 0 14px;
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
