<script setup lang="ts">
import { Pencil, Trash2 } from 'lucide-vue-next'
import { onBeforeUnmount, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'

import { AREA_COLORS, type AreaColor } from '@/lib/types'

defineProps<{ x: number; y: number; color: AreaColor }>()
const emit = defineEmits<{ rename: []; color: [color: AreaColor]; delete: []; close: [] }>()
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
    <button role="menuitem" data-test="rename" @click="emit('rename')">
      <Pencil :size="15" />{{ t('map.areas.rename') }}
    </button>
    <div class="colors" role="group" :aria-label="t('map.areas.color')">
      <button
        v-for="c in AREA_COLORS"
        :key="c"
        class="swatch"
        :class="{ current: c === color }"
        :data-color="c"
        :title="t(`map.areas.colors.${c}`)"
        :aria-label="t(`map.areas.colors.${c}`)"
        :aria-pressed="c === color"
        @click="emit('color', c)"
      />
    </div>
    <button role="menuitem" class="danger" data-test="delete" @click="emit('delete')">
      <Trash2 :size="15" />{{ t('map.areas.delete') }}
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
button[role='menuitem'] {
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
button[role='menuitem']:hover,
button[role='menuitem']:focus-visible {
  background: var(--surface-hover);
  outline: none;
}
.danger {
  color: var(--danger) !important;
}
.colors {
  display: flex;
  gap: 6px;
  padding: 6px 10px;
}
.swatch {
  --c: var(--text-muted);
  width: 18px;
  height: 18px;
  padding: 0;
  border: 2px solid transparent;
  border-radius: 50%;
  background: var(--c);
  cursor: pointer;
}
.swatch.current {
  border-color: var(--text);
}
.swatch[data-color='blue'] {
  --c: #4c8dff;
}
.swatch[data-color='green'] {
  --c: #3fb950;
}
.swatch[data-color='yellow'] {
  --c: #d29922;
}
.swatch[data-color='red'] {
  --c: #f85149;
}
.swatch[data-color='purple'] {
  --c: #a371f7;
}
</style>
