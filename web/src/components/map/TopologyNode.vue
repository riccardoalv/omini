<script setup lang="ts">
import { Handle, Position } from '@vue-flow/core'
import { AlertTriangle, Pin, SignalLow, Wifi } from 'lucide-vue-next'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import DeviceIcon from '@/components/DeviceIcon.vue'
import NodeIcon from '@/components/NodeIcon.vue'
import type { ClientGroup } from '@/lib/graph'
import { formatSpeed } from '@/lib/format'
import { displayName } from '@/lib/names'
import { type Flow, formatRate } from '@/lib/traffic'
import type { Severity, TopoNode } from '@/lib/types'

export interface NodeData {
  node?: TopoNode
  group?: ClientGroup
  error?: boolean // integration of this device failed
  direction?: 'RIGHT' | 'DOWN' // where edges enter and leave
  /** Internet traffic of a router/firewall or of a WAN (bits/s). */
  flow?: Flow
  /** The most severe open alert about this node. */
  alert?: Severity
}

const props = defineProps<{ data: NodeData; selected?: boolean }>()
const emit = defineEmits<{ toggle: [] }>()

// Middle click (mouse wheel) expands or groups the node's children.
function onAuxClick(e: MouseEvent) {
  if (e.button === 1) emit('toggle')
}
const { t } = useI18n()

const n = computed(() => props.data.node)
const variant = computed(() => (props.data.group ? 'group' : (n.value?.kind ?? 'client')))
const label = computed(() => {
  const node = n.value
  if (!node) return ''
  if (node.kind === 'segment' && node.label === 'Unmanaged segment') return t('map.segment')
  return displayName(node, t)
})
// Gray only when nothing could be identified.
const identified = computed(() => {
  const node = n.value
  return !!node && (node.type !== 'unknown' || !!node.brand || !!node.product || !!node.icon)
})
const sub = computed(() => {
  const node = n.value
  if (!node) return ''
  if (node.kind === 'segment') return t('map.clients', { n: node.mac_count ?? 0 })
  if (node.kind === 'app') return node.port ? `:${node.port}` : ''
  if (node.kind === 'wan') {
    // "2.5G · 30 ms": speed of the uplink and latency of its gateway.
    const rtt = node.wan?.gateways?.find((g) => g.rtt_ms !== undefined)?.rtt_ms
    return [formatSpeed(node.wan?.speed_mbps), rtt !== undefined ? `${Math.round(rtt)} ms` : '']
      .filter(Boolean)
      .join(' · ')
  }
  if (node.kind === 'client') return label.value !== node.ip ? (node.ip ?? '') : ''
  return [node.ip, node.model ?? node.vendor].filter(Boolean).join(' · ')
})
/** WAN status: the worst of its gateways (up, degraded, down). */
const wanStatus = computed(() => {
  const gws = n.value?.wan?.gateways ?? []
  if (!n.value?.online) return 'down'
  if (gws.some((g) => g.status === 'degraded' || g.status === 'down')) return 'degraded'
  return 'up'
})
const horizontal = computed(() => props.data.direction !== 'DOWN')
const weak = computed(() => (n.value?.signal_dbm ?? 0) < -75)
</script>

<template>
  <div
    class="topo-node"
    :class="[variant, { offline: n && !n.online, selected, error: data.error }]"
    :title="label"
    @mousedown.middle.prevent
    @auxclick.prevent="onAuxClick"
  >
    <Handle type="target" :position="horizontal ? Position.Left : Position.Top" class="handle" />
    <!-- On top; a compact one on a client (stacked close together). -->
    <div
      v-if="data.flow"
      class="flow-badge"
      :class="{ compact: variant === 'client' }"
      data-test="node-flow"
      :title="t('map.trafficHint')"
    >
      <span class="down">↓ {{ formatRate(data.flow.down) }}</span>
      <span class="up">↑ {{ formatRate(data.flow.up) }}</span>
    </div>

    <!-- An open alert (critical or warning): details in the panel and in Alerts. -->
    <span
      v-if="data.alert && data.alert !== 'info'"
      class="alert-mark"
      :class="data.alert"
      data-test="node-alert"
      :title="t(`insights.severity.${data.alert}`)"
    >
      <AlertTriangle :size="11" :stroke-width="2.5" />
    </span>

    <template v-if="data.group">
      <NodeIcon name="users" :size="16" />
      <div class="text">
        <strong v-if="data.group.area">{{ data.group.area.name }}</strong>
        <strong v-else>{{
          t(
            data.group.clients.every((c) => c.kind === 'app') ? 'map.appsLabel' : 'map.groupLabel',
            { n: data.group.clients.length },
          )
        }}</strong>
        <span v-if="data.group.area" class="sub">{{
          t('map.areas.devices', { n: data.group.clients.length, online: data.group.online })
        }}</span>
        <span v-else class="sub">{{ t('map.groupOnline', { n: data.group.online }) }}</span>
      </div>
    </template>

    <!-- A Wi-Fi network of an access point: only a picture between it and its clients. -->
    <template v-else-if="n && n.kind === 'ssid'">
      <Wifi :size="13" class="ssid-icon" :class="`band-${n.band ?? 'none'}`" />
      <span class="ssid-name" data-test="ssid">{{ n.label }}</span>
    </template>

    <template v-else-if="n">
      <span class="icon" :class="{ identified }">
        <DeviceIcon :device="n" :size="variant === 'client' || variant === 'app' ? 17 : 20" />
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
      <span v-else-if="variant === 'wan'" class="dot" :class="`wan-${wanStatus}`" />
    </template>

    <Handle
      type="source"
      :position="horizontal ? Position.Right : Position.Bottom"
      class="handle"
    />
  </div>
</template>

<style scoped>
.alert-mark {
  position: absolute;
  top: -7px;
  left: -7px;
  z-index: 1;
  display: grid;
  place-items: center;
  width: 18px;
  height: 18px;
  border: 2px solid var(--bg);
  border-radius: 50%;
  color: #fff;
  pointer-events: none;
}
.alert-mark.critical {
  background: var(--danger);
}
.alert-mark.warning {
  background: var(--warn);
}
.flow-badge {
  position: absolute;
  bottom: calc(100% + 6px);
  left: 50%;
  display: flex;
  gap: 8px;
  padding: 2px 9px;
  border: 1px solid var(--border-strong);
  border-radius: 999px;
  background: var(--bg-elevated);
  box-shadow: var(--shadow);
  font-family: var(--mono);
  font-size: 11px;
  white-space: nowrap;
  transform: translateX(-50%);
  pointer-events: none;
}
.flow-badge.compact {
  bottom: calc(100% + 2px); /* fits in the gap between stacked clients */
  gap: 6px;
  padding: 0 7px;
  font-size: 10px;
  line-height: 15px;
}
.flow-badge .down {
  color: var(--speed-1g);
}
.flow-badge .up {
  color: var(--speed-5g);
}
.topo-node.wan {
  border-color: color-mix(in srgb, var(--accent) 45%, var(--border));
  background: linear-gradient(
    135deg,
    color-mix(in srgb, var(--accent) 12%, var(--bg-elevated)),
    var(--bg-elevated)
  );
}
.topo-node.wan .icon {
  color: var(--accent);
}
.dot.wan-up {
  background: var(--ok);
}
.dot.wan-degraded {
  background: var(--warn);
}
.dot.wan-down {
  background: var(--danger);
}
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
.topo-node.app {
  border-radius: 999px;
  gap: 8px;
  padding: 0 12px 0 6px;
  border-style: dashed;
}
.app .icon {
  width: 24px;
  height: 24px;
  border-radius: 6px;
  background: transparent;
}
.client .icon.identified {
  background: var(--accent-soft);
  color: var(--text);
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
.topo-node.ssid {
  gap: 6px;
  padding: 0 10px;
  border-radius: 999px;
  border-style: dashed;
  cursor: pointer; /* a click folds or unfolds its clients */
  font-size: 11.5px;
}
.ssid-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.ssid-icon {
  flex: none;
  color: var(--text-muted);
}
.band-2\.4ghz {
  color: var(--speed-100m);
}
.band-5ghz {
  color: var(--speed-5g);
}
.band-6ghz {
  color: var(--speed-10g);
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
