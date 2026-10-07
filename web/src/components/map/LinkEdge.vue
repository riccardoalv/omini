<script setup lang="ts">
import type { EdgeProps } from '@vue-flow/core'
import { BaseEdge, EdgeLabelRenderer, getSmoothStepPath, Position } from '@vue-flow/core'
import { computed } from 'vue'

import { formatSpeed } from '@/lib/format'
import { speedColor } from '@/lib/speed'

export interface LinkData {
  speed?: number
  /** Where the speed goes: next to the device reached (default) or the source. */
  labelAt?: 'target' | 'source'
}

/**
 * A link of the map. Links from one node share their first segment, so the
 * speed is drawn at the end of each link, next to the device it reaches.
 */
const props = defineProps<EdgeProps<LinkData>>()

const path = computed(() =>
  getSmoothStepPath({
    sourceX: props.sourceX,
    sourceY: props.sourceY,
    sourcePosition: props.sourcePosition,
    targetX: props.targetX,
    targetY: props.targetY,
    targetPosition: props.targetPosition,
  }),
)

const label = computed(() => {
  const speed = formatSpeed(props.data?.speed)
  if (!speed) return undefined
  const vertical = props.targetPosition === Position.Top
  const color = speedColor(props.data?.speed)
  if (props.data?.labelAt === 'source') {
    return {
      text: speed,
      color,
      x: vertical ? props.sourceX : props.sourceX + 26,
      y: vertical ? props.sourceY + 14 : props.sourceY,
    }
  }
  return {
    text: speed,
    color,
    x: vertical ? props.targetX : props.targetX - 26,
    y: vertical ? props.targetY - 14 : props.targetY,
  }
})
</script>

<template>
  <BaseEdge :id="id" :path="path[0]" :style="style" :marker-end="markerEnd" />
  <EdgeLabelRenderer v-if="label">
    <div
      class="link-speed nodrag nopan"
      data-test="link-speed"
      :style="{
        transform: `translate(-50%, -50%) translate(${label.x}px, ${label.y}px)`,
        '--c': label.color,
      }"
    >
      {{ label.text }}
    </div>
  </EdgeLabelRenderer>
</template>

<style scoped>
.link-speed {
  position: absolute;
  padding: 1px 6px;
  border: 1px solid var(--c);
  border-radius: 999px;
  background: var(--bg-elevated);
  color: var(--c);
  font-family: var(--mono);
  font-size: 10.5px;
  font-weight: 600;
  line-height: 1.4;
  pointer-events: none;
  white-space: nowrap;
}
</style>
