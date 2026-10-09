<script setup lang="ts">
import { X } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

import DevicesList from '@/components/DevicesList.vue'

/** The device list, a drawer over the map ("N devices" in the toolbar opens it). */
const emit = defineEmits<{ close: []; select: [id: string]; changed: [] }>()
const { t } = useI18n()
</script>

<template>
  <aside class="devices-drawer card" data-test="devices-drawer">
    <header>
      <h2>{{ t('devices.title') }}</h2>
      <button
        class="btn ghost icon small"
        type="button"
        :aria-label="t('common.close')"
        @click="emit('close')"
      >
        <X :size="16" />
      </button>
    </header>
    <DevicesList @select="(id) => emit('select', id)" @changed="emit('changed')" />
  </aside>
</template>

<style scoped>
.devices-drawer {
  position: absolute;
  top: 56px;
  left: 12px;
  bottom: 12px;
  z-index: 15;
  display: flex;
  flex-direction: column;
  width: min(820px, calc(100% - 24px));
  padding: 14px 16px;
  overflow: auto;
  box-shadow: var(--shadow);
}
.devices-drawer header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 10px;
}
.devices-drawer h2 {
  margin: 0;
  font-size: 16px;
}
</style>
