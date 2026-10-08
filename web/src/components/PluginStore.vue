<script setup lang="ts">
import {
  Check,
  Download,
  ExternalLink,
  Plus,
  Puzzle,
  RefreshCw,
  Search,
  Trash2,
  X,
} from 'lucide-vue-next'
import { computed, nextTick, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { api, ApiError } from '@/lib/api'
import { formatAgo } from '@/lib/format'
import type {
  CatalogEntry,
  PluginIndexStatus,
  Integration,
  IntegrationType,
  PluginInfo,
  PluginTrust,
} from '@/lib/types'

import IntegrationLogo from './IntegrationLogo.vue'
import ModalDialog from './ModalDialog.vue'
import PluginTrustBadges from './PluginTrust.vue'

/**
 * The plugin store: the curated catalog plus whatever was installed from a
 * GitHub address, with search, filters and "+" to add a repository by URL.
 */
const props = defineProps<{
  /** Shown as a full page (the Store screen) instead of a modal. */
  page?: boolean
}>()
const emit = defineEmits<{
  close: []
  /** Open the "add integration" form of this plugin. */
  add: [type: string]
  /** Plugins were installed, updated or removed: integration types changed. */
  changed: []
}>()
const { t, te, locale } = useI18n()

interface Item {
  id: string
  name: string
  description: string
  url?: string
  icon?: string
  publisher: 'official' | 'community'
  trust: PluginTrust
  installed?: PluginInfo
  dev: boolean
  /** Built into Omini (network scan): always available. */
  builtin?: boolean
  /** Single type already added: it cannot be added again. */
  added?: boolean
  categories: string[]
  reviewedVersion?: string
  reviewedAt?: string
  knownIssues?: string
}

const catalog = ref<CatalogEntry[]>([])
const installed = ref<PluginInfo[]>([])
const types = ref<IntegrationType[]>([])
const integrations = ref<Integration[]>([])
const loading = ref(true)
const query = ref('')
const filter = ref<'all' | 'installed' | 'available'>('all')
const category = ref<string>()
const trustFilter = ref<PluginTrust | ''>('')
const index = ref<PluginIndexStatus>()
const refreshingIndex = ref(false)
const busy = ref<string>()
const errors = ref<Record<string, string>>({})
const confirmRemove = ref<string>()

const message = (e: unknown) => (e instanceof ApiError ? e.message : t('common.error'))

async function load() {
  try {
    ;[catalog.value, installed.value, types.value, integrations.value] = await Promise.all([
      api.pluginCatalog(),
      api.plugins(),
      api.integrationTypes(),
      api.integrations(),
    ])
    if (props.page) index.value = await api.pluginIndex().catch(() => undefined)
  } finally {
    loading.value = false
  }
}

const items = computed<Item[]>(() => {
  const byId = new Map(installed.value.map((p) => [p.manifest.id, p]))
  const used = new Set(integrations.value.map((i) => i.type))
  const added = (type: string) =>
    !!types.value.find((t) => t.type === type)?.single && used.has(type)
  // Built-in integrations first.
  const builtins: Item[] = types.value
    .filter((t) => t.kind === 'core')
    .map((t) => ({
      id: t.type,
      name: t.name,
      description: t.description ?? '',
      icon: undefined,
      publisher: 'official',
      trust: 'plug-and-play',
      dev: false,
      builtin: true,
      added: added(t.type),
      categories: ['discovery'],
    }))
  const out: Item[] = catalog.value.map((e) => ({
    id: e.id,
    name: e.name,
    description: e.description,
    url: e.url,
    icon: e.icon,
    publisher: e.publisher,
    trust: e.trust,
    installed: byId.get(e.id),
    dev: !!byId.get(e.id)?.dev,
    added: added(e.id),
    categories: e.categories ?? [],
    reviewedVersion: e.reviewed_version,
    reviewedAt: e.reviewed_at,
    knownIssues: e.known_issues,
  }))
  // Installed from a GitHub address or a development folder.
  for (const p of installed.value) {
    if (out.some((i) => i.id === p.manifest.id)) continue
    out.push({
      id: p.manifest.id,
      name: p.manifest.name,
      description: p.manifest.description ?? '',
      url: p.source?.url,
      publisher: p.publisher,
      trust: p.trust,
      installed: p,
      dev: p.dev,
      added: added(p.manifest.id),
      categories: [],
    })
  }
  return [...builtins, ...out]
})

const shown = computed(() => {
  const q = query.value.trim().toLowerCase()
  return items.value.filter((i) => {
    if (category.value && !i.categories.includes(category.value)) return false
    if (trustFilter.value && (i.builtin || i.trust !== trustFilter.value)) return false
    const has = i.builtin || !!i.installed
    if (filter.value === 'installed' && !has) return false
    if (filter.value === 'available' && has) return false
    if (!q) return true
    return [i.name, i.description, i.url ?? '', i.id].some((s) => s.toLowerCase().includes(q))
  })
})

const counts = computed(() => ({
  all: items.value.length,
  installed: items.value.filter((i) => i.builtin || i.installed).length,
  available: items.value.filter((i) => !i.builtin && !i.installed).length,
}))

async function install(item: Pick<Item, 'id' | 'url'>, version?: string) {
  if (!item.url) return
  busy.value = item.id
  errors.value = { ...errors.value, [item.id]: '' }
  try {
    await api.installPlugin(item.url.trim(), version?.trim() || undefined)
    await load()
    emit('changed')
    return true
  } catch (e) {
    errors.value = { ...errors.value, [item.id]: message(e) }
    return false
  } finally {
    busy.value = undefined
  }
}

async function remove(item: Item) {
  if (confirmRemove.value !== item.id) {
    confirmRemove.value = item.id
    return
  }
  confirmRemove.value = undefined
  busy.value = item.id
  try {
    await api.removePlugin(item.id)
    await load()
    emit('changed')
  } catch (e) {
    errors.value = { ...errors.value, [item.id]: message(e) }
  } finally {
    busy.value = undefined
  }
}

// "+": install from any GitHub address.
const adding = ref(false)
const addURL = ref('')
const addVersion = ref('')
const urlInput = ref<HTMLInputElement>()
async function toggleAdd() {
  adding.value = !adding.value
  if (adding.value) {
    await nextTick()
    urlInput.value?.focus()
  }
}
async function addFromURL() {
  const ok = await install({ id: '_url', url: addURL.value }, addVersion.value)
  if (ok) {
    adding.value = false
    addURL.value = ''
    addVersion.value = ''
    filter.value = 'installed'
  }
}

/** Categories present in the store, in a fixed order. */
const CATEGORY_ORDER = ['discovery', 'firewall', 'router', 'switch', 'wifi', 'hypervisor', 'nas']
const categories = computed(() => {
  const present = new Set(items.value.flatMap((i) => i.categories))
  return [
    ...CATEGORY_ORDER.filter((c) => present.has(c)),
    ...[...present].filter((c) => !CATEGORY_ORDER.includes(c)).sort(),
  ]
})
const categoryName = (c: string) => {
  const key = `store.categories.${c}`
  return te(key) ? t(key) : c
}

/** The installed release is newer than the one the trust level was given to. */
function unreviewed(item: Item): boolean {
  const v = item.installed?.source?.version ?? item.installed?.manifest.version
  return (
    !!item.reviewedVersion && !!v && v.replace(/^v/, '') !== item.reviewedVersion.replace(/^v/, '')
  )
}

async function refreshIndex() {
  refreshingIndex.value = true
  try {
    index.value = await api.refreshPluginIndex()
    catalog.value = await api.pluginCatalog()
  } catch (e) {
    errors.value = { ...errors.value, _index: message(e) }
  } finally {
    refreshingIndex.value = false
  }
}

const repoName = (url?: string) => url?.replace(/^https:\/\/(www\.)?github\.com\//, '') ?? ''

onMounted(load)
</script>

<template>
  <component
    :is="page ? 'div' : ModalDialog"
    v-bind="page ? { class: 'store-page' } : { title: t('store.title'), xl: true }"
    @close="emit('close')"
  >
    <div class="store">
      <div class="toolbar">
        <label class="search">
          <Search :size="16" />
          <input
            v-model="query"
            type="search"
            data-test="store-search"
            :placeholder="t('store.search')"
            :aria-label="t('store.search')"
            autofocus
          />
        </label>
        <button
          class="btn primary icon add"
          type="button"
          data-test="store-add"
          :aria-pressed="adding"
          :aria-label="t('store.addByUrl')"
          :title="t('store.addByUrl')"
          @click="toggleAdd"
        >
          <component :is="adding ? X : Plus" :size="18" />
        </button>
      </div>

      <form
        v-if="adding"
        class="add-url card"
        data-test="store-add-form"
        @submit.prevent="addFromURL"
      >
        <div class="add-head">
          <strong>{{ t('store.addByUrl') }}</strong>
          <span class="muted">{{ t('store.addHint') }}</span>
        </div>
        <div class="add-row">
          <input
            ref="urlInput"
            v-model="addURL"
            class="input grow"
            type="url"
            required
            data-test="store-url"
            placeholder="https://github.com/user/omini-plugin-…"
            :aria-label="t('plugins.url')"
          />
          <input
            v-model="addVersion"
            class="input version"
            :placeholder="t('plugins.latest')"
            :aria-label="t('plugins.version')"
          />
          <button class="btn primary" type="submit" :disabled="busy === '_url' || !addURL">
            <Download :size="15" />
            {{ busy === '_url' ? t('plugins.installing') : t('plugins.install') }}
          </button>
        </div>
        <p class="warn-text">{{ t('plugins.trustHint.unverified') }}</p>
        <p v-if="errors._url" class="alert error" role="alert">{{ errors._url }}</p>
      </form>

      <div class="tabs" role="tablist">
        <button
          v-for="f in ['all', 'installed', 'available'] as const"
          :key="f"
          role="tab"
          class="tab"
          :class="{ active: filter === f }"
          :aria-selected="filter === f"
          :data-test="`store-filter-${f}`"
          @click="filter = f"
        >
          {{ t(`store.filter.${f}`) }} <span class="count">{{ counts[f] }}</span>
        </button>
      </div>

      <div class="chips" role="group" :aria-label="t('store.category')">
        <button
          class="chip-btn"
          :class="{ on: category === undefined }"
          type="button"
          data-test="store-category-all"
          @click="category = undefined"
        >
          {{ t('store.filter.all') }}
        </button>
        <button
          v-for="c in categories"
          :key="c"
          class="chip-btn"
          :class="{ on: category === c }"
          type="button"
          :data-test="`store-category-${c}`"
          @click="category = category === c ? undefined : c"
        >
          {{ categoryName(c) }}
        </button>
        <select
          v-model="trustFilter"
          class="select trust-pick"
          data-test="store-trust"
          :aria-label="t('store.trust')"
        >
          <option value="">{{ t('store.anyTrust') }}</option>
          <option
            v-for="tr in ['plug-and-play', 'stable', 'experimental', 'unverified'] as const"
            :key="tr"
            :value="tr"
          >
            {{ t(`plugins.trust.${tr}`) }}
          </option>
        </select>
      </div>

      <p v-if="loading" class="muted center">{{ t('common.loading') }}</p>
      <div v-else-if="shown.length" class="grid">
        <article v-for="item in shown" :key="item.id" class="plugin card" data-test="store-card">
          <div class="top">
            <span class="logo">
              <IntegrationLogo :type="item.id" :icon="item.icon" />
            </span>
            <div class="title">
              <strong>{{ item.name }}</strong>
              <span v-if="item.builtin" class="badge builtin">{{ t('store.builtin') }}</span>
              <span v-else-if="item.dev" class="badge">{{ t('plugins.dev') }}</span>
              <PluginTrustBadges v-else :publisher="item.publisher" :trust="item.trust" />
            </div>
          </div>
          <p class="desc">{{ item.description }}</p>
          <a
            v-if="item.url"
            class="repo muted"
            :href="item.url"
            target="_blank"
            rel="noopener noreferrer"
          >
            <ExternalLink :size="12" /> {{ repoName(item.url) }}
          </a>
          <p v-if="item.reviewedVersion" class="review muted small" data-test="store-reviewed">
            {{ t('store.reviewed', { version: item.reviewedVersion }) }}
            <template v-if="item.reviewedAt"> · {{ item.reviewedAt }}</template>
            <a
              v-if="item.knownIssues"
              :href="item.knownIssues"
              target="_blank"
              rel="noopener noreferrer"
              class="issues"
              >{{ t('store.knownIssues') }}</a
            >
          </p>
          <p v-if="unreviewed(item)" class="warn-text small" data-test="store-unreviewed">
            {{ t('store.unreviewed') }}
          </p>
          <p v-if="errors[item.id]" class="alert error" role="alert">{{ errors[item.id] }}</p>
          <div class="actions">
            <template v-if="item.builtin">
              <span class="installed">
                <Check :size="14" />
                {{ t('store.includedInOmini') }}
              </span>
            </template>
            <template v-else-if="item.installed">
              <span class="installed" data-test="store-installed">
                <Check :size="14" />
                {{ t('store.installed') }}
                <span class="muted">{{
                  item.installed.source?.version ?? item.installed.manifest.version
                }}</span>
              </span>
              <span class="grow" />
              <template v-if="!item.dev && item.url">
                <button
                  class="btn ghost icon small"
                  type="button"
                  data-test="store-update"
                  :title="t('plugins.update')"
                  :aria-label="t('plugins.update')"
                  :disabled="!!busy"
                  @click="install(item)"
                >
                  <RefreshCw :size="14" :class="{ spin: busy === item.id }" />
                </button>
                <button
                  class="btn ghost icon small danger"
                  type="button"
                  data-test="store-remove"
                  :title="
                    confirmRemove === item.id ? t('plugins.confirmRemove') : t('plugins.remove')
                  "
                  :aria-label="t('plugins.remove')"
                  :disabled="!!busy"
                  @click="remove(item)"
                >
                  <Trash2 :size="14" />
                </button>
              </template>
            </template>
            <template v-else>
              <span class="grow" />
              <button
                class="btn small primary"
                type="button"
                data-test="store-install"
                :disabled="!!busy"
                @click="install(item)"
              >
                <Download :size="14" />
                {{ busy === item.id ? t('plugins.installing') : t('plugins.install') }}
              </button>
            </template>
          </div>
          <p v-if="confirmRemove === item.id" class="muted small">
            {{ t('plugins.confirmRemove') }}
          </p>
          <button
            v-if="item.builtin || item.installed"
            class="btn primary wide-btn"
            type="button"
            data-test="store-use"
            :disabled="item.added"
            :title="item.added ? t('store.addedHint') : undefined"
            @click="emit('add', item.id)"
          >
            {{ item.added ? t('store.added') : t('store.addIntegration') }}
          </button>
        </article>
      </div>
      <div v-else class="empty">
        <Puzzle :size="28" />
        <p>{{ query ? t('store.noMatch', { q: query }) : t('store.empty') }}</p>
        <button class="btn" type="button" @click="toggleAdd">
          <Plus :size="15" /> {{ t('store.addByUrl') }}
        </button>
      </div>

      <p v-if="page && index" class="index muted small" data-test="store-index">
        <template v-if="index.url">
          {{
            index.fetched_at
              ? t('store.indexUpdated', {
                  ago: formatAgo(index.fetched_at, locale),
                  n: index.plugins,
                })
              : t('store.indexShipped', { n: index.plugins })
          }}
          <button class="link" type="button" :disabled="refreshingIndex" @click="refreshIndex">
            <RefreshCw :size="12" :class="{ spin: refreshingIndex }" />
            {{ t('store.refreshIndex') }}
          </button>
        </template>
        <template v-else>{{ t('store.indexOff', { n: index.plugins }) }}</template>
        <span v-if="index.error || errors._index" class="err">
          · {{ errors._index || index.error }}</span
        >
        ·
        <a
          href="https://github.com/riccardoalv/omini/blob/main/docs/plugin-review.md"
          target="_blank"
          rel="noopener noreferrer"
          >{{ t('store.howReviewed') }}</a
        >
      </p>
    </div>
  </component>
</template>

<style scoped>
.store-page .store {
  max-width: 1200px;
  margin: 0 auto;
}
.chips {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
}
.chip-btn {
  padding: 4px 10px;
  border: 1px solid var(--border);
  border-radius: 999px;
  background: transparent;
  color: var(--text-muted);
  font-size: 12.5px;
  cursor: pointer;
}
.chip-btn.on {
  border-color: var(--accent);
  background: var(--accent-soft);
  color: var(--accent);
}
.trust-pick {
  width: auto;
  margin-left: auto;
}
.review .issues {
  margin-left: 6px;
}
.index {
  margin-top: 8px;
  text-align: center;
}
.index .err {
  color: var(--danger);
}
.index .link {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  margin-left: 6px;
}
.store {
  display: flex;
  flex-direction: column;
  gap: 14px;
  min-height: 100%;
}
.toolbar {
  display: flex;
  gap: 8px;
}
.search {
  display: flex;
  flex: 1;
  align-items: center;
  gap: 8px;
  padding: 0 12px;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--bg-sunken);
  color: var(--text-muted);
}
.search:focus-within {
  border-color: var(--accent);
  box-shadow: 0 0 0 3px var(--accent-soft);
}
.search input {
  flex: 1;
  height: 40px;
  border: 0;
  background: transparent;
  color: var(--text);
  font: inherit;
  font-size: 14.5px;
  outline: none;
}
.add {
  width: 42px;
  height: 42px;
  border-radius: var(--radius);
}
.add-url {
  padding: 14px;
  border-color: color-mix(in srgb, var(--accent) 40%, var(--border));
}
.add-head {
  display: flex;
  flex-direction: column;
  gap: 2px;
  margin-bottom: 10px;
  font-size: 13px;
}
.add-row {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
.grow {
  flex: 1;
  min-width: 0;
}
.version {
  width: 120px;
}
.warn-text {
  margin: 8px 0 0;
  font-size: 12px;
  color: var(--warn);
}
.tabs {
  display: flex;
  gap: 4px;
  border-bottom: 1px solid var(--border);
}
.tab {
  padding: 8px 12px;
  border: 0;
  border-bottom: 2px solid transparent;
  background: transparent;
  color: var(--text-muted);
  font: inherit;
  font-size: 13px;
  cursor: pointer;
}
.tab.active {
  border-bottom-color: var(--accent);
  color: var(--text);
}
.count {
  margin-left: 4px;
  padding: 0 6px;
  border-radius: 999px;
  background: var(--surface-hover);
  font-size: 11px;
}
.grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(270px, 1fr));
  gap: 12px;
}
.plugin {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 14px;
  transition:
    border-color 0.15s,
    transform 0.15s;
}
.plugin:hover {
  border-color: var(--border-strong);
  transform: translateY(-1px);
}
.top {
  display: flex;
  align-items: center;
  gap: 12px;
}
.logo {
  display: grid;
  flex: none;
  place-items: center;
  width: 48px;
  height: 48px;
  border: 1px solid var(--border);
  border-radius: 12px;
  background: var(--bg-sunken);
  color: var(--text-muted);
}
.title {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 4px;
  min-width: 0;
}
.title strong {
  font-size: 15px;
}
.desc {
  display: -webkit-box;
  margin: 0;
  overflow: hidden;
  color: var(--text-muted);
  font-size: 13px;
  line-height: 1.45;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 3;
}
.repo {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
  text-decoration: none;
}
.repo:hover {
  color: var(--accent);
}
.actions {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: auto;
  padding-top: 6px;
}
.installed {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  color: var(--ok);
  font-size: 12.5px;
  font-weight: 600;
}
.installed {
  min-width: 0;
}
.builtin-icon {
  color: var(--accent);
}
.badge.builtin {
  background: var(--accent-soft);
  color: var(--accent);
}
.installed .muted {
  overflow: hidden;
  font-weight: 400;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.wide-btn {
  justify-content: center;
  width: 100%;
}
.alert {
  margin: 0;
}
.small {
  margin: 0;
  font-size: 12px;
}
.empty,
.center {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
  margin: 40px 0;
  color: var(--text-muted);
  text-align: center;
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
