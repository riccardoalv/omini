<script setup lang="ts">
import { ChevronsDownUp, Pencil, Trash2 } from 'lucide-vue-next'
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import ColorPicker from '@/components/ColorPicker.vue'
import { areaCss, areaHex } from '@/lib/areas'
import { AREA_COLORS } from '@/lib/types'

const props = defineProps<{ x: number; y: number; color: string }>()
const emit = defineEmits<{
  rename: []
  collapse: []
  color: [color: string]
  /** The color being picked, shown on the area before it is applied (undefined: back). */
  preview: [color: string | undefined]
  delete: []
  close: []
}>()
const { t } = useI18n()

const custom = () => !(AREA_COLORS as readonly string[]).includes(props.color)
const picking = ref(false)
const draft = ref(areaHex(props.color))
function togglePicker() {
  picking.value = !picking.value
  draft.value = areaHex(props.color)
  if (!picking.value) emit('preview', undefined)
}
function onDraft(c: string) {
  draft.value = c
  emit('preview', c)
}

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
    <button role="menuitem" data-test="collapse-area" @click="emit('collapse')">
      <ChevronsDownUp :size="15" />{{ t('map.areas.collapse') }}
    </button>
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
        :style="{ '--c': areaCss(c) }"
        :title="t(`map.areas.colors.${c}`)"
        :aria-label="t(`map.areas.colors.${c}`)"
        :aria-pressed="c === color"
        @click="emit('color', c)"
      />
      <!-- Any color: opens the picker below. -->
      <button
        class="swatch picker"
        :class="{ current: custom(), open: picking }"
        :style="custom() ? { '--c': areaHex(color) } : undefined"
        data-test="custom-color"
        :title="t('map.areas.customColor')"
        :aria-label="t('map.areas.customColor')"
        :aria-expanded="picking"
        @click="togglePicker"
      />
    </div>
    <div v-if="picking" class="picker-panel">
      <ColorPicker
        :model-value="draft"
        @update:model-value="onDraft"
        @apply="(c) => emit('color', c)"
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
  position: relative;
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
/* The custom color: a rainbow until one is picked. */
.picker:not(.current) {
  background: conic-gradient(#f85149, #d29922, #3fb950, #2dd4bf, #4c8dff, #a371f7, #f85149);
}
.picker.open {
  border-color: var(--accent);
}
.picker-panel {
  padding: 4px 10px 8px;
}
</style>
