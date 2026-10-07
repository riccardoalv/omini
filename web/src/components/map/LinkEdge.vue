<script setup lang="ts">
import type { EdgeProps } from '@vue-flow/core'
import { BaseEdge, EdgeLabelRenderer, getSmoothStepPath, Position } from '@vue-flow/core'
import { computed } from 'vue'

import { formatSpeed } from '@/lib/format'
import { speedColor } from '@/lib/speed'
import type { LinkLabel } from '@/lib/traffic'

export type LinkData = LinkLabel

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

// A link shows its maximum speed only; the traffic is shown on the devices.
const label = computed(() => {
  const d = props.data
  if (!d || d.hidden) return undefined
  const speed = formatSpeed(d.speed)
  if (!speed) return undefined
  const vertical = props.targetPosition === Position.Top
  const at =
    d.at === 'source'
      ? {
          x: vertical ? props.sourceX : props.sourceX + 26,
          y: vertical ? props.sourceY + 16 : props.sourceY,
        }
      : {
          x: vertical ? props.targetX : props.targetX - 26,
          y: vertical ? props.targetY - 16 : props.targetY,
        }
  return { speed, color: speedColor(d.speed), ...at }
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
      <span v-if="label.speed" class="speed">{{ label.speed }}</span>
    </div>
  </EdgeLabelRenderer>
</template>

<style scoped>
.link-speed {
  position: absolute;
  display: inline-flex;
  align-items: center;
  gap: 5px;
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
