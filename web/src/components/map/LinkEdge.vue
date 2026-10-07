<script setup lang="ts">
import type { EdgeProps } from '@vue-flow/core'
import { BaseEdge, EdgeLabelRenderer, getSmoothStepPath, Position } from '@vue-flow/core'
import { computed } from 'vue'

import { formatSpeed } from '@/lib/format'
import { speedColor } from '@/lib/speed'
import { motionSeconds, type Flow, type LinkLabel } from '@/lib/traffic'

export type LinkData = Partial<LinkLabel> & {
  /** Traffic moving along the link (animated when `animate`). */
  motion?: Flow
  animate?: boolean
}

/**
 * A link of the map, with its port's name and speed in the middle.
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

// A link shows its maximum speed (the traffic is shown on the devices) with
// its port's name — "Porta LAN | 10G" — in the middle of the wire. Links
// sharing a port show one pill in the middle of the part they share.
const label = computed(() => {
  const d = props.data
  if (!d || d.hidden || !d.at) return undefined
  const speed = formatSpeed(d.speed)
  if (!speed && !d.name) return undefined
  const [, cx, cy] = path.value
  const vertical = props.targetPosition === Position.Top
  const at =
    d.at === 'shared'
      ? vertical
        ? { x: props.sourceX, y: (props.sourceY + cy) / 2 }
        : { x: (props.sourceX + cx) / 2, y: props.sourceY }
      : { x: cx, y: cy }
  return { speed, name: d.name, color: speedColor(d.speed), ...at }
})

// Traffic moving along the wire: dashes run towards the device for the
// download and back for the upload, faster with more traffic.
const motion = computed(() => {
  const d = props.data
  if (!d?.animate || !d.motion) return []
  const out: { dir: 'down' | 'up'; seconds: number }[] = []
  if (d.motion.down > 1000) out.push({ dir: 'down', seconds: motionSeconds(d.motion.down) })
  if (d.motion.up > 1000) out.push({ dir: 'up', seconds: motionSeconds(d.motion.up) })
  return out
})
</script>

<template>
  <BaseEdge :id="id" :path="path[0]" :style="style" :marker-end="markerEnd" />
  <path
    v-for="m in motion"
    :key="m.dir"
    class="flow-motion"
    :class="m.dir"
    :d="path[0]"
    :style="{ animationDuration: `${m.seconds}s` }"
    data-test="link-motion"
  />
  <EdgeLabelRenderer v-if="label">
    <div
      class="link-speed nodrag nopan"
      data-test="link-speed"
      :style="{
        transform: `translate(-50%, -50%) translate(${label.x}px, ${label.y}px)`,
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
.flow-motion {
  fill: none;
  stroke-width: 2.5;
  stroke-linecap: round;
  stroke-dasharray: 1 15;
  pointer-events: none;
  animation: flow-down linear infinite;
}
.flow-motion.down {
  stroke: #38bdf8;
}
.flow-motion.up {
  stroke: #a78bfa;
  stroke-dashoffset: 8;
  animation-name: flow-up;
}
@keyframes flow-down {
  from {
    stroke-dashoffset: 16;
  }
  to {
    stroke-dashoffset: 0;
  }
}
@keyframes flow-up {
  from {
    stroke-dashoffset: 8;
  }
  to {
    stroke-dashoffset: 24;
  }
}
@media (prefers-reduced-motion: reduce) {
  .flow-motion {
    animation: none;
  }
}
:global(.exporting) .flow-motion {
  display: none;
}
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
