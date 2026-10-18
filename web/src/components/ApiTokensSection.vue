<script setup lang="ts">
import { Check, Copy, KeyRound, Trash2 } from 'lucide-vue-next'
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { api, ApiError } from '@/lib/api'
import { formatAgo, formatDateTime } from '@/lib/format'
import type { ApiToken, NewApiToken } from '@/lib/types'

/**
 * Settings → API tokens: read-only tokens for scripts and AI assistants (the
 * HTTP API's GET routes and the MCP server). A token is shown once, when it
 * is created; Omini keeps only its hash.
 */
const { t, locale } = useI18n()

const tokens = ref<ApiToken[]>([])
const name = ref('')
const busy = ref(false)
const error = ref('')
const created = ref<NewApiToken | null>(null)
const copied = ref('')

const mcpURL = computed(() => `${window.location.origin}/mcp`)
const shownToken = computed(() => created.value?.token ?? 'omini_…')
const cliSnippet = computed(
  () =>
    `claude mcp add --transport http omini ${mcpURL.value} \\\n  --header "Authorization: Bearer ${shownToken.value}"`,
)
const jsonSnippet = computed(() =>
  JSON.stringify(
    {
      mcpServers: {
        omini: {
          type: 'http',
          url: mcpURL.value,
          headers: { Authorization: `Bearer ${shownToken.value}` },
        },
      },
    },
    null,
    2,
  ),
)

async function load() {
  try {
    tokens.value = await api.apiTokens()
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : t('common.error')
  }
}

async function create() {
  error.value = ''
  if (!name.value.trim()) return
  busy.value = true
  try {
    created.value = await api.createApiToken(name.value.trim())
    name.value = ''
    await load()
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : t('common.error')
  } finally {
    busy.value = false
  }
}

async function remove(tok: ApiToken) {
  if (!window.confirm(t('tokens.confirmDelete', { name: tok.name }))) return
  error.value = ''
  try {
    await api.deleteApiToken(tok.id)
    if (created.value?.id === tok.id) created.value = null
    await load()
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : t('common.error')
  }
}

async function copy(what: string, text: string) {
  try {
    await navigator.clipboard.writeText(text)
    copied.value = what
    setTimeout(() => {
      if (copied.value === what) copied.value = ''
    }, 2000)
  } catch {
    copied.value = ''
  }
}

onMounted(load)
</script>

<template>
  <div class="tokens" data-test="api-tokens">
    <div class="card intro">
      <p>{{ t('tokens.intro') }}</p>
      <div class="url">
        <span class="label">{{ t('tokens.mcpUrl') }}</span>
        <code class="mono" data-test="mcp-url">{{ mcpURL }}</code>
        <button
          class="btn ghost icon small"
          type="button"
          :aria-label="t('tokens.copy')"
          @click="copy('url', mcpURL)"
        >
          <Check v-if="copied === 'url'" :size="14" />
          <Copy v-else :size="14" />
        </button>
      </div>
      <small class="muted">{{ t('tokens.mcpHint') }}</small>
    </div>

    <div v-if="created" class="card created" role="status" data-test="new-token">
      <strong>{{ t('tokens.createdTitle', { name: created.name }) }}</strong>
      <p class="warn">{{ t('tokens.onceWarning') }}</p>
      <div class="secret">
        <code class="mono" data-test="token-value">{{ created.token }}</code>
        <button
          class="btn"
          type="button"
          data-test="copy-token"
          @click="copy('token', created.token)"
        >
          <Check v-if="copied === 'token'" :size="14" />
          <Copy v-else :size="14" />
          {{ copied === 'token' ? t('tokens.copied') : t('tokens.copy') }}
        </button>
      </div>
      <button class="btn small done" type="button" data-test="token-done" @click="created = null">
        {{ t('tokens.done') }}
      </button>
    </div>

    <div class="card">
      <form class="create" data-test="token-form" @submit.prevent="create">
        <input
          v-model="name"
          class="input"
          maxlength="64"
          :placeholder="t('tokens.namePlaceholder')"
          :aria-label="t('tokens.name')"
          data-test="token-name"
        />
        <button class="btn primary" type="submit" :disabled="busy || !name.trim()">
          <KeyRound :size="14" />
          {{ t('tokens.create') }}
        </button>
      </form>
      <p v-if="error" class="error" role="alert">{{ error }}</p>
      <p v-if="!tokens.length" class="muted empty" data-test="no-tokens">{{ t('tokens.none') }}</p>
      <ul v-else class="list">
        <li v-for="tok in tokens" :key="tok.id" class="token" data-test="token">
          <div class="grow">
            <span class="name">{{ tok.name }}</span>
            <span class="muted small">
              {{ t('tokens.createdAt', { at: formatDateTime(tok.created_at, locale) }) }} ·
              {{
                tok.last_used_at
                  ? t('tokens.lastUsed', { ago: formatAgo(tok.last_used_at, locale) })
                  : t('tokens.neverUsed')
              }}
            </span>
          </div>
          <button
            class="btn ghost icon small"
            type="button"
            :aria-label="t('common.delete')"
            data-test="delete-token"
            @click="remove(tok)"
          >
            <Trash2 :size="14" />
          </button>
        </li>
      </ul>
    </div>

    <div class="card example">
      <strong>{{ t('tokens.exampleTitle') }}</strong>
      <small class="muted">{{ t('tokens.exampleCli') }}</small>
      <pre class="mono" data-test="snippet-cli">{{ cliSnippet }}</pre>
      <small class="muted">{{ t('tokens.exampleJson') }}</small>
      <pre class="mono" data-test="snippet-json">{{ jsonSnippet }}</pre>
    </div>
  </div>
</template>

<style scoped>
.tokens {
  display: grid;
  gap: 16px;
}
.card {
  display: grid;
  gap: 8px;
  padding: 16px 20px;
}
.intro p,
.created p {
  margin: 0;
}
.url,
.secret {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}
.url .label {
  font-weight: 600;
}
code,
pre {
  overflow-wrap: anywhere;
  font-size: 13px;
}
.secret code {
  padding: 6px 10px;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--bg);
  user-select: all;
}
pre {
  margin: 0;
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--bg);
  white-space: pre-wrap;
}
.created {
  border-color: var(--warn);
}
.warn {
  color: var(--warn);
}
.done {
  justify-self: start;
}
.create {
  display: flex;
  gap: 8px;
}
.create .input {
  flex: 1;
  max-width: 360px;
}
.list {
  display: grid;
  margin: 0;
  padding: 0;
  list-style: none;
}
.token {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 0;
}
.token + .token {
  border-top: 1px solid var(--border);
}
.grow {
  display: grid;
  flex: 1;
  gap: 2px;
  min-width: 0;
}
.name {
  font-weight: 600;
}
.small {
  font-size: 12px;
}
.empty {
  margin: 4px 0 0;
}
.error {
  margin: 0;
  color: var(--danger);
}
</style>
