<script lang="ts">
/** The menu's tabs. */
export type MenuTab = 'devices' | 'people' | 'vlans' | 'services'
</script>

<script setup lang="ts">
import { EyeOff, Pause, WifiOff, X } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

import DevicesList from '@/components/DevicesList.vue'
import ToggleSwitch from '@/components/ToggleSwitch.vue'
import PeopleTab from '@/components/map/PeopleTab.vue'
import ServicesTab from '@/components/map/ServicesTab.vue'
import VlansTab from '@/components/map/VlansTab.vue'
import { displayName } from '@/lib/names'
import type { NetworkGroup } from '@/lib/networks'
import { prefs } from '@/lib/prefs'
import type { TopoNode } from '@/lib/types'

/**
 * The map's menu, a drawer over the map (the menu button in the toolbar opens
 * it), in tabs: the device list (with the offline and hidden switches), the
 * people and their devices, the network's VLANs and subnets (a click
 * highlights one on the map) and its DNS and DHCP.
 */
defineProps<{
  summary: { devices: number; clients: number }
  networkGroups: NetworkGroup[]
  /** Network key → color. */
  colors: Map<string, string>
  offlineCount: number
  hiddenCount: number
  /** Devices whose internet a router pauses, and the rule set by hand that does it. */
  paused?: { node: TopoNode; rule?: string }[]
}>()
const emit = defineEmits<{ close: []; select: [id: string]; changed: [] }>()
const tab = defineModel<MenuTab>('tab', { default: 'devices' })
const TABS: MenuTab[] = ['devices', 'people', 'vlans', 'services']
const netFilter = defineModel<string | undefined>('netFilter')
const showHidden = defineModel<boolean>('showHidden', { required: true })
const { t } = useI18n()

const setShowOffline = (show: boolean) => {
  prefs.hideOffline = !show
}
/** Picking a network highlights it and zooms the map to it: the menu steps aside. */
function pick(key: string | undefined) {
  netFilter.value = key
  if (key) emit('close')
}
</script>

<template>
  <aside class="devices-drawer card" data-test="devices-drawer">
    <header class="head">
      <div class="tabs" role="tablist">
        <button
          v-for="tb in TABS"
          :key="tb"
          type="button"
          role="tab"
          class="tab"
          :class="{ active: tab === tb }"
          :aria-selected="tab === tb"
          :data-test="`menu-tab-${tb}`"
          @click="tab = tb"
        >
          {{ t(`mapMenu.${tb}`) }}
        </button>
      </div>
      <button
        class="btn ghost icon small"
        type="button"
        :aria-label="t('common.close')"
        @click="emit('close')"
      >
        <X :size="16" />
      </button>
    </header>

    <template v-if="tab === 'devices'">
      <div class="stats">
        <span class="stat"
          ><strong>{{ summary.devices }}</strong>
          {{ t('mapMenu.equipment', summary.devices) }}</span
        >
        <span class="stat"
          ><strong>{{ summary.clients }}</strong> {{ t('mapMenu.clients', summary.clients) }}</span
        >
        <span class="grow" />
        <label class="switch-row">
          <WifiOff :size="15" />
          {{ t('mapMenu.offline') }}
          <span v-if="offlineCount" class="count">{{ offlineCount }}</span>
          <ToggleSwitch
            data-test="toggle-offline"
            :model-value="!prefs.hideOffline"
            :label="t('mapMenu.offline')"
            @update:model-value="setShowOffline"
          />
        </label>
        <label v-if="hiddenCount" class="switch-row">
          <EyeOff :size="15" />
          {{ t('mapMenu.hidden') }}
          <span class="count">{{ hiddenCount }}</span>
          <ToggleSwitch
            v-model="showHidden"
            data-test="toggle-hidden"
            :label="t('mapMenu.hidden')"
          />
        </label>
      </div>
      <p v-if="paused?.length" class="paused" data-test="paused-list">
        <Pause :size="14" />
        {{ t('host.paused') }}:
        <template v-for="(p, i) in paused" :key="p.node.id">
          <button class="link" type="button" @click="emit('select', p.node.id)">
            {{ displayName(p.node, t) }}</button
          ><small v-if="p.rule" class="muted"> ({{ t('host.manual') + ': ' + p.rule }})</small
          >{{ i < paused.length - 1 ? ', ' : '' }}
        </template>
      </p>
      <DevicesList @select="(id) => emit('select', id)" @changed="emit('changed')" />
    </template>

    <PeopleTab
      v-else-if="tab === 'people'"
      :paused-ids="paused?.map((p) => p.node.id)"
      @select="(id) => emit('select', id)"
      @changed="emit('changed')"
    />
    <VlansTab
      v-else-if="tab === 'vlans'"
      :groups="networkGroups"
      :colors="colors"
      :picked="netFilter"
      @select="(id) => emit('select', id)"
      @pick="pick"
    />
    <ServicesTab v-else-if="tab === 'services'" @select="(id) => emit('select', id)" />
  </aside>
</template>

<style scoped>
.paused {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 4px;
  margin: 0 0 10px;
  padding: 8px 10px;
  border-radius: 8px;
  background: var(--warn-soft);
  font-size: 13px;
}
.devices-drawer {
  position: absolute;
  top: 56px;
  left: 12px;
  bottom: 12px;
  z-index: 15;
  display: flex;
  flex-direction: column;
  width: min(820px, calc(100% - 24px));
  padding: 6px 16px 14px;
  overflow: auto;
  box-shadow: var(--shadow);
}
.head {
  position: sticky;
  top: -6px;
  z-index: 1;
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin: -6px -16px 14px;
  padding: 6px 12px 0;
  border-bottom: 1px solid var(--border);
  background: var(--bg-elevated);
}
.tabs {
  display: flex;
  gap: 2px;
}
.tab {
  position: relative;
  padding: 10px 14px;
  border: 0;
  border-bottom: 2px solid transparent;
  margin-bottom: -1px;
  background: none;
  color: var(--text-muted);
  font: inherit;
  font-weight: 600;
  cursor: pointer;
}
.tab:hover {
  color: var(--text);
}
.tab.active {
  border-bottom-color: var(--accent);
  color: var(--text);
}
.stats {
  display: flex;
  gap: 8px;
  margin-bottom: 12px;
}
.stat {
  padding: 6px 12px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface);
  color: var(--text-muted);
  font-size: 12.5px;
}
.stat strong {
  margin-right: 2px;
  color: var(--text);
  font-size: 15px;
}

.stats {
  flex-wrap: wrap;
  align-items: center;
}
.grow {
  flex: 1;
}
.switch-row {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  color: var(--text-muted);
  font-size: 13px;
  cursor: pointer;
}
.count {
  padding: 1px 8px;
  border-radius: 999px;
  background: var(--surface-hover);
  color: var(--text-muted);
  font-size: 11.5px;
  font-weight: 600;
}
</style>
