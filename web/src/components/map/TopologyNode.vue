<script setup lang="ts">
import { Handle, Position } from '@vue-flow/core'
import { Pin, SignalLow } from 'lucide-vue-next'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import DeviceIcon from '@/components/DeviceIcon.vue'
import NodeIcon from '@/components/NodeIcon.vue'
import type { ClientGroup } from '@/lib/graph'
import type { TopoNode } from '@/lib/types'

export interface NodeData {
  node?: TopoNode
  group?: ClientGroup
  error?: boolean // integration of this device failed
  direction?: 'RIGHT' | 'DOWN' // where edges enter and leave
}

const props = defineProps<{ data: NodeData; selected?: boolean }>()
const { t } = useI18n()

const n = computed(() => props.data.node)
const variant = computed(() => (props.data.group ? 'group' : (n.value?.kind ?? 'client')))
const label = computed(() => {
  if (n.value?.kind === 'segment' && n.value.label === 'Unmanaged segment') return t('map.segment')
  return n.value?.label ?? ''
})
const sub = computed(() => {
  const node = n.value
  if (!node) return ''
  if (node.kind === 'segment') return t('map.clients', { n: node.mac_count ?? 0 })
  if (node.kind === 'client') return node.hostname && node.ip ? node.ip : ''
  return [node.ip, node.model ?? node.vendor].filter(Boolean).join(' · ')
})
const horizontal = computed(() => props.data.direction !== 'DOWN')
const weak = computed(() => (n.value?.signal_dbm ?? 0) < -75)
</script>

<template>
  <div
    class="topo-node"
    :class="[variant, { offline: n && !n.online, selected, error: data.error }]"
    :title="label"
  >
    <Handle type="target" :position="horizontal ? Position.Left : Position.Top" class="handle" />

    <template v-if="data.group">
      <NodeIcon name="users" :size="16" />
      <div class="text">
        <strong>{{ t('map.groupLabel', { n: data.group.clients.length }) }}</strong>
        <span class="sub">{{ t('map.groupOnline', { n: data.group.online }) }}</span>
      </div>
    </template>

    <template v-else-if="n">
      <span class="icon">
        <DeviceIcon :device="n" :size="variant === 'client' ? 17 : 20" />
      </span>
      <div class="text">
        <strong>{{ label }}</strong>
        <span v-if="sub" class="sub">{{ sub }}</span>
      </div>
      <SignalLow v-if="weak" class="warn-icon" :size="14" />
      <Pin v-if="n.pinned" class="pin" :size="12" />
      <span
        v-if="variant === 'device'"
        class="dot"
        :class="{ online: n.online && !data.error, error: data.error }"
      />
    </template>

    <Handle
      type="source"
      :position="horizontal ? Position.Right : Position.Bottom"
      class="handle"
    />
  </div>
</template>

<style scoped>
.topo-node {
  display: flex;
  align-items: center;
  gap: 10px;
  height: 100%;
  padding: 0 12px;
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  color: var(--text);
  box-shadow: 0 1px 2px rgb(0 0 0 / 0.15);
  cursor: pointer;
  transition:
    border-color 0.15s,
    box-shadow 0.15s;
}
.topo-node:hover {
  border-color: var(--border-strong);
}
.topo-node.selected {
  border-color: var(--accent);
  box-shadow: 0 0 0 3px var(--accent-soft);
}
.topo-node.offline {
  opacity: 0.5;
}
.topo-node.error {
  border-color: var(--danger);
}
.topo-node.unmanaged,
.topo-node.segment {
  border-style: dashed;
  background: var(--bg);
}
.topo-node.client {
  border-radius: 999px;
  gap: 8px;
  padding: 0 12px 0 6px;
}
.topo-node.group {
  justify-content: center;
  border-radius: 999px;
  background: var(--accent-soft);
  border-color: transparent;
  color: var(--accent);
}
.icon {
  display: grid;
  place-items: center;
  width: 34px;
  height: 34px;
  flex: none;
  border-radius: 8px;
  background: var(--surface-hover);
  color: var(--text-muted);
}
.device .icon {
  background: var(--accent-soft);
  color: var(--accent);
}
.client .icon {
  width: 26px;
  height: 26px;
  border-radius: 50%;
}
.text {
  display: flex;
  flex-direction: column;
  min-width: 0;
  flex: 1;
  line-height: 1.25;
}
strong {
  font-weight: 600;
  font-size: 13px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.client strong {
  font-weight: 500;
  font-size: 12.5px;
}
.sub {
  color: var(--text-muted);
  font-size: 11.5px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.group .sub {
  color: inherit;
  opacity: 0.8;
}
.warn-icon {
  color: var(--warn);
  flex: none;
}
.pin {
  color: var(--text-muted);
  flex: none;
}
.handle {
  opacity: 0;
  pointer-events: none;
}
</style>
