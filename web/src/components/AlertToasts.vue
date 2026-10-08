<script setup lang="ts">
import { AlertOctagon, AlertTriangle, Lightbulb, MapPin, X } from 'lucide-vue-next'
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'

import { alertsState, alertText } from '@/lib/alerts'
import { ui } from '@/lib/ui'
import type { Alert } from '@/lib/types'

/**
 * Popups of the open critical and warning alerts, in a corner of every screen
 * (but the Alerts one): what happened, a tip on what to do, "show on the
 * map" and a close button. A closed popup stays closed until its alert
 * resolves and opens again (it gets a new id); remembered per browser.
 */
const MAX = 3
const KEY = 'omini.closedAlerts'
const { t, locale } = useI18n()
const route = useRoute()
const router = useRouter()

function loadClosed(): number[] {
  try {
    const v = JSON.parse(localStorage.getItem(KEY) ?? '[]')
    return Array.isArray(v) ? v.filter((x) => typeof x === 'number') : []
  } catch {
    return []
  }
}
const closed = ref<number[]>(loadClosed())
function close(a: Alert) {
  // Keep only ids of alerts still open: the list never grows forever.
  const open = new Set(alertsState.list.map((x) => x.id))
  closed.value = [...closed.value.filter((id) => open.has(id)), a.id]
  try {
    localStorage.setItem(KEY, JSON.stringify(closed.value))
  } catch {
    // remembered for this visit only
  }
}

const rank = { critical: 0, warning: 1, info: 2 }
const pending = computed(() =>
  alertsState.list
    .filter(
      (a) =>
        !a.resolved_at && !a.dismissed && a.severity !== 'info' && !closed.value.includes(a.id),
    )
    .sort((a, b) => rank[a.severity] - rank[b.severity] || b.opened_at.localeCompare(a.opened_at)),
)
const shown = computed(() => (route.path === '/alerts' ? [] : pending.value.slice(0, MAX)))
const more = computed(() =>
  route.path === '/alerts' ? 0 : pending.value.length - shown.value.length,
)

function tip(a: Alert): string {
  const key = `insights.tips.${a.rule}`
  const text = t(key)
  return text === key ? '' : text
}
function showOnMap(a: Alert) {
  void router.push({ path: '/', query: { node: a.node_id } })
}
</script>

<template>
  <div
    v-if="shown.length"
    class="toasts"
    role="region"
    :aria-label="t('toasts.label')"
    :style="ui.panelWidth ? { right: `${ui.panelWidth + 28}px` } : undefined"
  >
    <article
      v-for="a in shown"
      :key="a.id"
      class="toast card"
      :class="a.severity"
      role="status"
      data-test="toast"
    >
      <component
        :is="a.severity === 'critical' ? AlertOctagon : AlertTriangle"
        :size="18"
        class="icon"
      />
      <div class="body">
        <strong>{{ alertText(a, t, locale).title }}</strong>
        <p class="muted">{{ alertText(a, t, locale).detail }}</p>
        <p v-if="tip(a)" class="tip"><Lightbulb :size="13" />{{ tip(a) }}</p>
        <button
          v-if="a.node_id"
          class="link small"
          type="button"
          data-test="toast-map"
          @click="showOnMap(a)"
        >
          <MapPin :size="13" />{{ t('insights.onMap') }}
        </button>
      </div>
      <button
        class="btn ghost icon small close"
        type="button"
        data-test="toast-close"
        :aria-label="t('toasts.close')"
        @click="close(a)"
      >
        <X :size="15" />
      </button>
    </article>
    <RouterLink v-if="more > 0" to="/alerts" class="more card" data-test="toast-more">
      {{ t('toasts.more', { n: more }, more) }}
    </RouterLink>
  </div>
</template>

<style scoped>
.toasts {
  position: fixed;
  right: 16px;
  bottom: 16px;
  z-index: 50;
  display: grid;
  gap: 10px;
  width: min(380px, calc(100vw - 32px));
}
.toast {
  --c: var(--warn);
  display: flex;
  align-items: flex-start;
  gap: 10px;
  padding: 12px 10px 12px 14px;
  border-left: 3px solid var(--c);
  box-shadow: var(--shadow);
  animation: in 0.2s ease-out;
}
.toast.critical {
  --c: var(--danger);
}
.icon {
  flex: none;
  margin-top: 1px;
  color: var(--c);
}
.body {
  display: grid;
  flex: 1;
  gap: 4px;
  min-width: 0;
}
.body strong {
  font-size: 13.5px;
}
.body p {
  margin: 0;
  font-size: 12.5px;
  line-height: 1.4;
}
.tip {
  display: flex;
  gap: 6px;
  color: var(--text);
}
.tip svg {
  flex: none;
  margin-top: 2px;
  color: var(--accent);
}
.body .link {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  justify-self: start;
  font-size: 12.5px;
}
.close {
  flex: none;
}
.more {
  padding: 8px 12px;
  color: var(--accent);
  font-size: 13px;
  text-align: center;
  text-decoration: none;
}
@keyframes in {
  from {
    opacity: 0;
    transform: translateY(8px);
  }
}
@media (prefers-reduced-motion: reduce) {
  .toast {
    animation: none;
  }
}
</style>
