<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import { areaCss, MIN_AREA_SIZE } from '@/lib/areas'
import type { MapArea } from '@/lib/types'

const props = defineProps<{ area: MapArea; editing: boolean; zoom: number }>()
const emit = defineEmits<{
  rename: [name: string]
  cancelRename: []
  startRename: []
  resize: [size: { width: number; height: number }]
  resizeEnd: []
}>()
const { t } = useI18n()

// Inline rename.
const draft = ref('')
const input = ref<HTMLInputElement>()
watch(
  () => props.editing,
  async (on) => {
    if (!on) return
    draft.value = props.area.name
    await nextTick()
    focusInput()
  },
  { immediate: true },
)
// A new node stays hidden until Vue Flow has measured it: retry for a moment.
function focusInput(tries = 20) {
  const el = input.value
  if (!el) return
  el.focus()
  if (document.activeElement === el) el.select()
  else if (tries > 0) setTimeout(() => focusInput(tries - 1), 25)
}
function commit() {
  if (!props.editing) return
  const name = draft.value.trim()
  if (name && name !== props.area.name) emit('rename', name)
  else emit('cancelRename')
}

// Resizing from the bottom-right corner (screen pixels → map units with the zoom).
let start: { x: number; y: number; width: number; height: number } | undefined
function onResizeStart(e: PointerEvent) {
  start = { x: e.clientX, y: e.clientY, width: props.area.width, height: props.area.height }
  ;(e.target as Element).setPointerCapture?.(e.pointerId)
}
function onResizeMove(e: PointerEvent) {
  if (!start) return
  const zoom = props.zoom || 1
  emit('resize', {
    width: Math.max(MIN_AREA_SIZE, Math.round(start.width + (e.clientX - start.x) / zoom)),
    height: Math.max(MIN_AREA_SIZE, Math.round(start.height + (e.clientY - start.y) / zoom)),
  })
}
function onResizeEnd() {
  if (!start) return
  start = undefined
  emit('resizeEnd')
}
</script>

<template>
  <div class="area" :class="{ 'is-hidden': area.hidden }" :style="{ '--c': areaCss(area.color) }">
    <div class="area-title" :title="t('map.areas.dragHint')" @dblclick.stop="emit('startRename')">
      <input
        v-if="editing"
        ref="input"
        v-model="draft"
        class="nodrag"
        maxlength="60"
        :aria-label="t('map.areas.name')"
        @keydown.enter.prevent="commit"
        @keydown.esc.prevent="emit('cancelRename')"
        @blur="commit"
      />
      <span v-else>{{ area.name }}</span>
    </div>
    <div
      v-if="!area.auto"
      class="resize nodrag nopan"
      data-test="resize"
      :title="t('map.areas.resize')"
      @pointerdown.stop.prevent="onResizeStart"
      @pointermove="onResizeMove"
      @pointerup="onResizeEnd"
      @pointercancel="onResizeEnd"
    />
  </div>
</template>

<style scoped>
.area {
  --c: var(--text-muted);
  position: relative;
  width: 100%;
  height: 100%;
  border: 1.5px dashed color-mix(in srgb, var(--c) 70%, transparent);
  border-radius: var(--radius);
  background: color-mix(in srgb, var(--c) 7%, transparent);
  pointer-events: none; /* the inside of an area never blocks the map or its nodes */
}
/* A hidden area, shown for a while with "Show hidden". */
.area.is-hidden {
  opacity: 0.45;
  border-style: dotted;
}
/* The title sits above the rectangle, so devices inside never cover it. */
.area-title {
  position: absolute;
  bottom: calc(100% + 4px);
  left: 0;
  max-width: 100%;
  padding: 4px 12px;
  border-radius: var(--radius-sm);
  background: color-mix(in srgb, var(--c) 22%, var(--bg-elevated));
  color: var(--text);
  font-size: 13px;
  font-weight: 600;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  cursor: grab;
  pointer-events: auto;
}
.area-title input {
  width: 180px;
  padding: 0 4px;
  border: 1px solid var(--accent);
  border-radius: 4px;
  background: var(--bg);
  color: var(--text);
  font: inherit;
}
.resize {
  position: absolute;
  right: -6px;
  bottom: -6px;
  width: 16px;
  height: 16px;
  cursor: nwse-resize;
  pointer-events: auto;
}
.resize::after {
  content: '';
  position: absolute;
  right: 6px;
  bottom: 6px;
  width: 8px;
  height: 8px;
  border-right: 2px solid var(--c);
  border-bottom: 2px solid var(--c);
  opacity: 0;
  transition: opacity 0.15s;
}
.area-title:hover ~ .resize::after,
.resize:hover::after {
  opacity: 1;
}
</style>
