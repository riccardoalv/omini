<script setup lang="ts">
import { Bell, Info, Map as MapIcon, Palette, UserRound } from 'lucide-vue-next'
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'

import NotificationsSection from '@/components/NotificationsSection.vue'
import { locales } from '@/i18n'
import { api, ApiError } from '@/lib/api'
import { prefs } from '@/lib/prefs'
import { session, setLocale, signOut } from '@/lib/session'

/**
 * Settings: a menu of sections on the left, the section on the right
 * (`?section=` keeps it in the address, so a link can open notifications).
 */
const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const version = ref('')

// Changing the admin password (Account).
const pw = ref({
  open: false,
  current: '',
  next: '',
  confirm: '',
  busy: false,
  error: '',
  saved: false,
})
function openPassword() {
  pw.value = {
    open: true,
    current: '',
    next: '',
    confirm: '',
    busy: false,
    error: '',
    saved: false,
  }
}
async function savePassword() {
  const p = pw.value
  p.error = ''
  if (p.next.length < 8) return (p.error = t('settings.password.tooShort'))
  if (p.next !== p.confirm) return (p.error = t('settings.password.mismatch'))
  p.busy = true
  try {
    await api.changePassword(p.current, p.next)
    pw.value = {
      open: false,
      current: '',
      next: '',
      confirm: '',
      busy: false,
      error: '',
      saved: true,
    }
  } catch (e) {
    p.error =
      e instanceof ApiError && e.status === 403
        ? t('settings.password.wrong')
        : e instanceof ApiError
          ? e.message
          : t('common.error')
  } finally {
    p.busy = false
  }
}

const sections = [
  { id: 'general', icon: Palette },
  { id: 'map', icon: MapIcon },
  { id: 'notifications', icon: Bell },
  { id: 'account', icon: UserRound },
  { id: 'about', icon: Info },
] as const
type Section = (typeof sections)[number]['id']

const current = computed<Section>(() => {
  const s = route.query.section
  return sections.some((x) => x.id === s) ? (s as Section) : 'general'
})
function go(id: Section) {
  void router.replace({ query: { ...route.query, section: id } })
}

onMounted(async () => {
  try {
    version.value = (await api.health()).version
  } catch {
    // version is informative only
  }
})
</script>

<template>
  <div class="page settings">
    <header class="page-header">
      <div>
        <h1>{{ t('settings.title') }}</h1>
        <p class="muted">{{ t('settings.subtitle') }}</p>
      </div>
    </header>

    <div class="layout">
      <nav class="menu" :aria-label="t('settings.title')">
        <button
          v-for="s in sections"
          :key="s.id"
          type="button"
          class="menu-item"
          :class="{ current: current === s.id }"
          :aria-current="current === s.id ? 'page' : undefined"
          :data-test="`section-${s.id}`"
          @click="go(s.id)"
        >
          <component :is="s.icon" :size="18" />
          <span>
            <strong>{{ t(`settings.sections.${s.id}.title`) }}</strong>
            <small class="muted">{{ t(`settings.sections.${s.id}.hint`) }}</small>
          </span>
        </button>
      </nav>

      <div class="content">
        <header class="content-head">
          <h2>{{ t(`settings.sections.${current}.title`) }}</h2>
          <p class="muted">{{ t(`settings.sections.${current}.hint`) }}</p>
        </header>

        <template v-if="current === 'general'">
          <div class="rows card">
            <div class="row">
              <div class="row-text">
                <label for="s-theme">{{ t('settings.theme') }}</label>
                <small class="muted">{{ t('settings.themeHint') }}</small>
              </div>
              <select id="s-theme" v-model="prefs.theme" class="select">
                <option value="system">{{ t('settings.themeSystem') }}</option>
                <option value="dark">{{ t('settings.themeDark') }}</option>
                <option value="light">{{ t('settings.themeLight') }}</option>
              </select>
            </div>
            <div class="row">
              <div class="row-text">
                <label for="s-locale">{{ t('settings.language') }}</label>
                <small class="muted">{{ t('settings.languageHint') }}</small>
              </div>
              <select
                id="s-locale"
                :value="prefs.locale"
                class="select"
                @change="setLocale(($event.target as HTMLSelectElement).value)"
              >
                <option v-for="l in locales" :key="l.code" :value="l.code">{{ l.name }}</option>
              </select>
            </div>
          </div>
        </template>

        <template v-else-if="current === 'map'">
          <div class="rows card">
            <div class="row">
              <div class="row-text">
                <label for="s-threshold">{{ t('settings.collapseThreshold') }}</label>
                <small class="muted">{{ t('settings.collapseHint') }}</small>
              </div>
              <input
                id="s-threshold"
                v-model.number="prefs.collapseThreshold"
                class="input short"
                type="number"
                min="2"
                max="100"
              />
            </div>
          </div>
        </template>

        <NotificationsSection v-else-if="current === 'notifications'" />

        <template v-else-if="current === 'account'">
          <div class="rows card">
            <div class="row">
              <div class="row-text">
                <span class="label">{{
                  t('settings.signedInAs', { name: session.username })
                }}</span>
                <small class="muted">{{ t('settings.accountHint') }}</small>
              </div>
              <button class="btn" data-test="sign-out" @click="signOut">
                {{ t('auth.signOut') }}
              </button>
            </div>
            <div class="row">
              <div class="row-text">
                <span class="label">{{ t('settings.password.title') }}</span>
                <small class="muted">{{ t('settings.password.hint') }}</small>
                <small v-if="pw.saved" class="ok" data-test="password-saved">{{
                  t('settings.password.saved')
                }}</small>
              </div>
              <button v-if="!pw.open" class="btn" data-test="change-password" @click="openPassword">
                {{ t('settings.password.change') }}
              </button>
            </div>
            <form
              v-if="pw.open"
              class="row password"
              data-test="password-form"
              @submit.prevent="savePassword"
            >
              <input
                v-model="pw.current"
                class="input"
                type="password"
                autocomplete="current-password"
                :placeholder="t('settings.password.current')"
                :aria-label="t('settings.password.current')"
                required
              />
              <input
                v-model="pw.next"
                class="input"
                type="password"
                autocomplete="new-password"
                :placeholder="t('settings.password.new')"
                :aria-label="t('settings.password.new')"
                required
              />
              <input
                v-model="pw.confirm"
                class="input"
                type="password"
                autocomplete="new-password"
                :placeholder="t('settings.password.confirm')"
                :aria-label="t('settings.password.confirm')"
                required
              />
              <p v-if="pw.error" class="error" role="alert">{{ pw.error }}</p>
              <div class="actions">
                <button class="btn" type="button" @click="pw.open = false">
                  {{ t('settings.password.cancel') }}
                </button>
                <button class="btn primary" type="submit" :disabled="pw.busy">
                  {{ t('settings.password.save') }}
                </button>
              </div>
            </form>
          </div>
        </template>

        <template v-else>
          <div class="rows card">
            <div class="row">
              <div class="row-text">
                <span class="label">Omini</span>
                <small class="muted">{{ t('settings.version', { v: version || '…' }) }}</small>
              </div>
              <a
                class="btn"
                href="https://github.com/riccardoalv/omini"
                target="_blank"
                rel="noopener"
                >GitHub</a
              >
            </div>
          </div>
        </template>
      </div>
    </div>
  </div>
</template>

<style scoped>
.settings {
  max-width: 1280px;
}
.layout {
  display: grid;
  grid-template-columns: 260px minmax(0, 1fr);
  gap: 28px;
  align-items: start;
}
.menu {
  position: sticky;
  top: 20px;
  display: grid;
  gap: 4px;
}
.menu-item {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  width: 100%;
  padding: 10px 12px;
  border: 1px solid transparent;
  border-radius: var(--radius);
  background: transparent;
  color: var(--text-muted);
  font: inherit;
  text-align: left;
  cursor: pointer;
}
.menu-item > svg {
  flex: none;
  margin-top: 2px;
}
.menu-item span {
  display: grid;
  gap: 2px;
}
.menu-item strong {
  color: var(--text);
  font-size: 14px;
  font-weight: 600;
}
.menu-item small {
  font-size: 12px;
  line-height: 1.35;
}
.menu-item:hover {
  background: var(--surface-hover);
}
.menu-item.current {
  border-color: var(--border);
  background: var(--bg-elevated);
  color: var(--accent);
}
.content-head {
  margin-bottom: 16px;
}
.content-head h2 {
  margin: 0;
  font-size: 18px;
}
.content-head p {
  margin: 4px 0 0;
}
.rows {
  display: grid;
}
.row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 24px;
  padding: 16px 20px;
}
.row + .row {
  border-top: 1px solid var(--border);
}
.row-text {
  display: grid;
  gap: 3px;
  min-width: 0;
}
.row-text label,
.row-text .label {
  font-weight: 600;
}
.row .select {
  width: 240px;
  flex: none;
}
.short {
  width: 100px;
  flex: none;
}
@media (max-width: 860px) {
  .layout {
    grid-template-columns: 1fr;
    gap: 16px;
  }
  .menu {
    position: static;
    display: flex;
    overflow-x: auto;
  }
  .menu-item {
    flex: none;
    width: auto;
    align-items: center;
  }
  .menu-item small {
    display: none;
  }
  .row {
    flex-direction: column;
    align-items: stretch;
    gap: 10px;
  }
  .row .select {
    width: 100%;
  }
}
.row.password {
  flex-direction: column;
  align-items: stretch;
  gap: 8px;
}
.row.password .input {
  max-width: 360px;
}
.row.password .actions {
  display: flex;
  gap: 8px;
}
.row .ok {
  color: var(--ok);
}
.row .error {
  margin: 0;
  color: var(--danger);
}
</style>
