<script setup lang="ts">
import { Download, Trash2 } from 'lucide-vue-next'
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { api, ApiError } from '@/lib/api'
import type { PluginInfo } from '@/lib/types'

const { t } = useI18n()

const plugins = ref<PluginInfo[]>([])
const url = ref('')
const version = ref('')
const installing = ref(false)
const error = ref('')
const installed = ref('')
const confirmRemove = ref<string>()

const message = (e: unknown) => (e instanceof ApiError ? e.message : t('common.error'))

async function load() {
  try {
    plugins.value = await api.plugins()
  } catch (e) {
    error.value = message(e)
  }
}

async function install() {
  installing.value = true
  error.value = ''
  installed.value = ''
  try {
    const p = await api.installPlugin(url.value.trim(), version.value.trim() || undefined)
    installed.value = t('plugins.installed', { name: p.manifest.name, v: p.manifest.version })
    url.value = ''
    version.value = ''
    await load()
  } catch (e) {
    error.value = message(e)
  } finally {
    installing.value = false
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
    <ul v-if="plugins.length" class="list">
      <li v-for="p in plugins" :key="p.manifest.id" data-test="plugin">
        <div class="grow">
          <strong>{{ p.manifest.name }}</strong>
          <span class="muted"> {{ p.manifest.version }}</span>
          <span v-if="p.dev" class="badge">{{ t('plugins.dev') }}</span>
          <div v-if="p.source" class="muted small">
            <a :href="p.source.url" target="_blank" rel="noopener noreferrer">{{ p.source.url }}</a>
          </div>
        </div>
        <button v-if="!p.dev" class="btn small danger" data-test="remove" @click="remove(p)">
          <Trash2 :size="14" />
          {{ confirmRemove === p.manifest.id ? t('plugins.confirmRemove') : t('plugins.remove') }}
        </button>
      </li>
    </ul>
    <p v-else class="muted">{{ t('plugins.none') }}</p>

    <form class="install" @submit.prevent="install">
      <div class="field grow">
        <label for="p-url">{{ t('plugins.url') }}</label>
        <input
          id="p-url"
          v-model="url"
          class="input"
          type="url"
          required
          placeholder="https://github.com/riccardoalv/omini-plugin-opnsense"
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
      <button class="btn primary" type="submit" data-test="install" :disabled="installing || !url">
        <Download :size="15" />
        {{ installing ? t('plugins.installing') : t('plugins.install') }}
      </button>
    </form>
    <p class="help">{{ t('plugins.hint') }}</p>
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
  margin-bottom: 14px;
}
.list {
  list-style: none;
  margin: 0 0 14px;
  padding: 0;
}
.list li {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 0;
  border-bottom: 1px solid var(--border);
}
.grow {
  flex: 1;
  min-width: 0;
}
.small {
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
  margin: 0 0 12px;
}
.alert {
  margin: 0 0 14px;
}
.badge {
  margin-left: 6px;
}
</style>
