<script setup lang="ts">
import {
  ArrowDownFromLine,
  ArrowRightFromLine,
  Bell,
  Download,
  Eye,
  EyeOff,
  LayoutGrid,
  List,
  RefreshCw,
  SquareDashed,
} from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

import type { AutoGroup } from '@/lib/autoAreas'
import { formatAgo } from '@/lib/format'
import { prefs } from '@/lib/prefs'

/**
 * The map's toolbar: what is on the map (devices, alerts, failing
 * integrations, last update) on the left, the map's controls on the right.
 */
defineProps<{
  summary: { devices: number; clients: number; problems: number }
  /** Open critical and warning alerts not dismissed. */
  attention: number
  generatedAt?: string
  now: number
  offlineCount: number
  networkGroups: AutoGroup[]
  hiddenCount: number
  exporting: boolean
  refreshing: boolean
}>()
const emit = defineEmits<{ export: []; resetLayout: []; refresh: [] }>()
const devicesOpen = defineModel<boolean>('devicesOpen', { required: true })
const netFilter = defineModel<string | undefined>('netFilter')
const showHidden = defineModel<boolean>('showHidden', { required: true })
const drawing = defineModel<boolean>('drawing', { required: true })
const { t, locale } = useI18n()

function toggleDirection() {
  prefs.layoutDirection = prefs.layoutDirection === 'RIGHT' ? 'DOWN' : 'RIGHT'
}
</script>

<template>
  <div class="toolbar">
    <div class="chips">
      <button
        type="button"
        class="chip chip-button"
        :class="{ active: devicesOpen }"
        data-test="devices-chip"
        :aria-expanded="devicesOpen"
        :title="t('devices.open')"
        @click="devicesOpen = !devicesOpen"
      >
        <List :size="12" />
        {{ t('map.devices', summary.devices) }} · {{ t('map.clients', summary.clients) }}
      </button>
      <RouterLink v-if="attention" to="/alerts" class="chip problem" data-test="alerts-chip">
        <Bell :size="12" />
        {{ t('map.alerts', { n: attention }, attention) }}
      </RouterLink>
      <RouterLink v-if="summary.problems" to="/integrations" class="chip problem">
        {{ t('map.problems', summary.problems) }}
      </RouterLink>
      <span v-if="generatedAt" class="updated muted">
        {{ t('map.updated', { ago: formatAgo(generatedAt, locale, now) }) }}
      </span>
    </div>
    <div class="buttons">
      <button
        class="btn small"
        :class="{ active: prefs.hideOffline }"
        data-test="toggle-offline"
        :aria-pressed="prefs.hideOffline"
        :title="prefs.hideOffline ? t('map.showOffline') : t('map.hideOffline')"
        @click="prefs.hideOffline = !prefs.hideOffline"
      >
        <component :is="prefs.hideOffline ? Eye : EyeOff" :size="15" />
        <span class="label">
          {{ prefs.hideOffline ? t('map.showOffline') : t('map.hideOffline') }}
          <template v-if="offlineCount"> ({{ offlineCount }})</template>
        </span>
      </button>
      <select
        v-if="networkGroups.length"
        v-model="netFilter"
        class="select small net-pick"
        data-test="net-filter"
        :class="{ active: netFilter }"
        :aria-label="t('map.netFilter')"
      >
        <option :value="undefined">{{ t('map.allNetworks') }}</option>
        <option v-for="g in networkGroups" :key="g.key" :value="g.key">{{ g.name }}</option>
      </select>
      <button
        v-if="hiddenCount"
        class="btn small"
        :class="{ active: showHidden }"
        data-test="toggle-hidden"
        :aria-pressed="showHidden"
        @click="showHidden = !showHidden"
      >
        <component :is="showHidden ? EyeOff : Eye" :size="15" />
        <span class="label">
          {{ showHidden ? t('map.hideHidden') : t('map.showHidden') }} ({{ hiddenCount }})
        </span>
      </button>
      <button
        class="btn small"
        :title="prefs.layoutDirection === 'RIGHT' ? t('map.topDown') : t('map.leftToRight')"
        data-test="direction"
        @click="toggleDirection"
      >
        <component
          :is="prefs.layoutDirection === 'RIGHT' ? ArrowDownFromLine : ArrowRightFromLine"
          :size="15"
        />
        <span class="label">
          {{ prefs.layoutDirection === 'RIGHT' ? t('map.topDown') : t('map.leftToRight') }}
        </span>
      </button>
      <button
        class="btn small"
        :class="{ active: drawing }"
        data-test="new-area"
        :aria-pressed="drawing"
        :title="t('map.areas.drawHint')"
        @click="drawing = !drawing"
      >
        <SquareDashed :size="15" /><span class="label">{{ t('map.areas.new') }}</span>
      </button>
      <button
        class="btn small"
        data-test="export"
        :disabled="exporting"
        aria-haspopup="dialog"
        @click="emit('export')"
      >
        <Download :size="15" /><span class="label">{{ t('map.export') }}</span>
      </button>
      <button class="btn small" :title="t('map.resetLayout')" @click="emit('resetLayout')">
        <LayoutGrid :size="15" /><span class="label">{{ t('map.resetLayout') }}</span>
      </button>
      <button class="btn small" :disabled="refreshing" @click="emit('refresh')">
        <RefreshCw :size="15" :class="{ spin: refreshing }" />
        <span class="label">{{ t('map.refresh') }}</span>
      </button>
    </div>
  </div>
</template>

<style scoped>
.toolbar {
  position: absolute;
  top: 12px;
  left: 12px;
  right: 12px;
  z-index: 4;
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
  pointer-events: none;
  flex-wrap: wrap;
}
.toolbar > * {
  pointer-events: auto;
}
.chips,
.buttons {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
}
.chip {
  padding: 5px 12px;
  border: 1px solid var(--border);
  border-radius: 999px;
  background: var(--bg-elevated);
  font-size: 13px;
  font-weight: 500;
  text-decoration: none;
  color: var(--text);
}
.chip-button {
  cursor: pointer;
  font: inherit;
  font-size: inherit;
}
.chip-button:hover,
.chip-button.active {
  border-color: var(--accent);
  color: var(--accent);
}
.chip.problem {
  background: var(--danger-soft);
  border-color: transparent;
  color: var(--danger);
}
.updated {
  font-size: 12.5px;
  margin-left: 4px;
}
.spin {
  animation: spin 0.8s linear infinite;
}
.btn.active {
  background: var(--accent-soft);
  border-color: var(--accent);
  color: var(--accent);
}
@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}
.net-pick {
  width: auto;
  max-width: 260px;
  pointer-events: auto;
}
.net-pick.active {
  border-color: var(--accent);
  color: var(--accent);
}

@media (max-width: 640px) {
  .label {
    display: none;
  }
}
</style>
