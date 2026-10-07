<script setup lang="ts">
import { ChevronsDownUp, ChevronsUpDown, Info } from 'lucide-vue-next'
import { onBeforeUnmount, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'

defineProps<{ x: number; y: number; canCollapse: boolean; canExpand: boolean }>()
const emit = defineEmits<{ collapse: []; expand: []; details: []; close: [] }>()
const { t } = useI18n()

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') emit('close')
}
function onOutside() {
  emit('close')
}
onMounted(() => {
  window.addEventListener('keydown', onKey)
  // Next tick: the right-click that opened the menu must not close it.
  setTimeout(() => window.addEventListener('pointerdown', onOutside), 0)
})
onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKey)
  window.removeEventListener('pointerdown', onOutside)
})
</script>

<template>
  <div class="menu card" role="menu" :style="{ left: `${x}px`, top: `${y}px` }" @pointerdown.stop>
    <button v-if="canCollapse" role="menuitem" data-test="collapse" @click="emit('collapse')">
      <ChevronsDownUp :size="15" />{{ t('map.collapseChildren') }}
    </button>
    <button v-if="canExpand" role="menuitem" data-test="expand" @click="emit('expand')">
      <ChevronsUpDown :size="15" />{{ t('map.expandChildren') }}
    </button>
    <button role="menuitem" data-test="details" @click="emit('details')">
      <Info :size="15" />{{ t('map.details') }}
    </button>
  </div>
</template>

<style scoped>
.menu {
  position: absolute;
  z-index: 20;
  min-width: 190px;
  padding: 4px;
  box-shadow: var(--shadow);
}
button {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 7px 10px;
  border: 0;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text);
  font: inherit;
  text-align: left;
  cursor: pointer;
}
button:hover,
button:focus-visible {
  background: var(--surface-hover);
  outline: none;
}
</style>
