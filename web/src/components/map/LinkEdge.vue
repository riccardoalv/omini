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

// A link shows its maximum speed only (the traffic is shown on the devices),
// with the port's name when it has one: "LAN · 10G".
const label = computed(() => {
  const d = props.data
  if (!d || d.hidden) return undefined
  const speed = formatSpeed(d.speed)
  if (!speed && !d.name) return undefined
  const vertical = props.targetPosition === Position.Top
  // The pill grows away from the node it sits next to.
  const at =
    d.at === 'source'
      ? vertical
        ? { x: props.sourceX, y: props.sourceY + 16, shift: '-50%, -50%' }
        : { x: props.sourceX + 10, y: props.sourceY, shift: '0, -50%' }
      : vertical
        ? { x: props.targetX, y: props.targetY - 16, shift: '-50%, -50%' }
        : { x: props.targetX - 10, y: props.targetY, shift: '-100%, -50%' }
  return { speed, name: d.name, color: speedColor(d.speed), ...at }
})
</script>

<template>
  <BaseEdge :id="id" :path="path[0]" :style="style" :marker-end="markerEnd" />
  <EdgeLabelRenderer v-if="label">
    <div
      class="link-speed nodrag nopan"
      data-test="link-speed"
      :style="{
        transform: `translate(${label.x}px, ${label.y}px) translate(${label.shift})`,
        '--c': label.color,
      }"
    >
      <span v-if="label.name" class="name" data-test="link-name" :title="label.name">{{
        label.name
      }}</span>
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
.name {
  max-width: 140px;
  overflow: hidden;
  color: var(--text);
  font-family: var(--font);
  font-weight: 500;
  text-overflow: ellipsis;
}
.name + .speed {
  padding-left: 5px;
  border-left: 1px solid color-mix(in srgb, var(--c) 45%, transparent);
}
</style>
