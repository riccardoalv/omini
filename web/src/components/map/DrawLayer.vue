<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { rectFrom } from '@/lib/areas'
import type { Point } from '@/lib/types'

/**
 * Drawing a new area: a layer over the map where a rectangle is dragged. It
 * tells the rectangle drawn (screen coordinates).
 */
const emit = defineEmits<{ draw: [start: Point, end: Point] }>()
const { t } = useI18n()

const layer = ref<HTMLElement>()
const draft = ref<{ start: Point; end: Point }>()
const draftStyle = computed(() => {
  if (!draft.value) return undefined
  const box = layer.value?.getBoundingClientRect()
  const r = rectFrom(draft.value.start, draft.value.end)
  return {
    left: `${r.x - (box?.left ?? 0)}px`,
    top: `${r.y - (box?.top ?? 0)}px`,
    width: `${r.width}px`,
    height: `${r.height}px`,
  }
})

function onDrawStart(e: PointerEvent) {
  const p = { x: e.clientX, y: e.clientY }
  draft.value = { start: p, end: p }
  ;(e.target as Element).setPointerCapture?.(e.pointerId)
}

function onDrawMove(e: PointerEvent) {
  if (draft.value) draft.value = { ...draft.value, end: { x: e.clientX, y: e.clientY } }
}

function onDrawEnd() {
  const d = draft.value
  draft.value = undefined
  if (d) emit('draw', d.start, d.end)
}
</script>

<template>
  <div
    ref="layer"
    class="draw-layer"
    data-test="draw-layer"
    @pointerdown.prevent="onDrawStart"
    @pointermove="onDrawMove"
    @pointerup="onDrawEnd"
  >
    <p class="draw-hint chip">{{ t('map.areas.drawHint') }}</p>
    <div v-if="draftStyle" class="draft" :style="draftStyle" />
  </div>
</template>

<style scoped>
.draw-layer {
  position: absolute;
  inset: 0;
  z-index: 3;
  cursor: crosshair;
}
.draw-hint {
  position: absolute;
  bottom: 16px;
  left: 50%;
  transform: translateX(-50%);
  margin: 0;
  pointer-events: none;
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
.draft {
  position: absolute;
  border: 1.5px dashed var(--accent);
  border-radius: var(--radius);
  background: var(--accent-soft);
}
</style>
