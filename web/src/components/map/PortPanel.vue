<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { formatBytes, formatSpeed } from '@/lib/format'
import { speedColor, speedTier, SPEED_TIERS } from '@/lib/speed'
import type { Interface, TopoNode } from '@/lib/types'

/**
 * Front view of a device: one RJ45 port per physical interface, colored by
 * link speed (empty when down). Hover or focus a port for its details; click
 * it to open what is connected to it.
 */
const props = defineProps<{
  ports: Interface[]
  /** Node connected to each port, by port name. */
  links: Record<string, TopoNode | undefined>
}>()
const emit = defineEmits<{ select: [id: string] }>()
const { t } = useI18n()

/** More than 8 ports: two rows, odd ports on top like on a switch. */
const rows = computed(() => {
  const list = props.ports.map((p, i) => ({ p, n: i + 1 }))
  if (list.length <= 8) return [list]
  return [list.filter((x) => x.n % 2 === 1), list.filter((x) => x.n % 2 === 0)]
})

const legend = computed(() => {
  const present = new Set(props.ports.filter((p) => p.up).map((p) => speedTier(p.speed_mbps)))
  return SPEED_TIERS.filter((tier) => present.has(tier))
})

const hovered = ref<Interface>()
const tip = ref({ top: 0 })
const root = ref<HTMLElement>()

function show(p: Interface, e: Event) {
  hovered.value = p
  const el = e.currentTarget as HTMLElement
  const box = root.value?.getBoundingClientRect()
  const r = el.getBoundingClientRect()
  // Full width under the port's row: the panel is narrow, a centered tip would be cut.
  tip.value = { top: r.bottom - (box?.top ?? 0) + 6 }
}

function open(p: Interface) {
  const other = props.links[p.name]
  if (other) emit('select', other.id)
}

/** "2.5G · full duplex" */
function portStatus(p: Interface) {
  const speed = formatSpeed(p.speed_mbps) || t('panel.portUp')
  if (!p.duplex) return speed
  return `${speed} · ${p.duplex === 'full' ? t('panel.fullDuplex') : t('panel.halfDuplex')}`
}

/** "SFP+", "SFP28", "QSFP+", "RJ45": the cage generation follows the speed. */
function connectorName(p: Interface) {
  const speed = p.speed_mbps ?? 0
  if (p.connector === 'sfp') return speed >= 25_000 ? 'SFP28' : speed >= 10_000 ? 'SFP+' : 'SFP'
  if (p.connector === 'qsfp') return speed >= 100_000 ? 'QSFP28' : 'QSFP+'
  return 'RJ45'
}

/** Short label under a port: "igb0" stays, "GigabitEthernet0/12" → "12". */
function shortName(name: string) {
  if (name.length <= 8) return name
  const m = /(\d+)$/.exec(name)
  return m ? m[1] : name.slice(0, 6)
}
</script>

<template>
  <div ref="root" class="faceplate-wrap">
    <div class="faceplate" data-test="faceplate">
      <div v-for="(row, ri) in rows" :key="ri" class="row">
        <button
          v-for="{ p } in row"
          :key="p.name"
          type="button"
          class="port"
          :class="{ up: p.up, linked: !!links[p.name] }"
          :style="{ '--c': p.up ? speedColor(p.speed_mbps) : undefined }"
          :data-port="p.name"
          :aria-label="`${p.name}: ${p.up ? formatSpeed(p.speed_mbps) || t('panel.portUp') : t('panel.portDown')}`"
          @mouseenter="show(p, $event)"
          @focus="show(p, $event)"
          @mouseleave="hovered = undefined"
          @blur="hovered = undefined"
          @click="open(p)"
        >
          <!-- SFP / QSFP: a cage with the module's latch; otherwise an RJ45 jack. -->
          <svg
            v-if="p.connector === 'sfp' || p.connector === 'qsfp'"
            viewBox="0 0 36 30"
            aria-hidden="true"
            class="cage"
          >
            <rect class="jack" x="3" y="6" width="30" height="18" rx="1.5" />
            <rect class="slot" x="7" y="10" width="22" height="10" rx="1" />
            <path class="latch" d="M11 6V3h14v3" />
          </svg>
          <svg v-else viewBox="0 0 36 30" aria-hidden="true">
            <path class="jack" d="M3 3h30v18h-7v4h-4v3h-8v-3h-4v-4H3z" stroke-linejoin="round" />
            <rect
              v-for="i in 6"
              :key="i"
              class="pin"
              :x="7 + (i - 1) * 4.2"
              y="6"
              width="1.6"
              height="5"
            />
          </svg>
          <span class="led" />
          <span class="name" :title="p.name">{{ shortName(p.name) }}</span>
          <span class="speed">{{ p.up ? formatSpeed(p.speed_mbps) || '·' : '—' }}</span>
        </button>
      </div>
    </div>

    <div v-if="legend.length" class="legend">
      <span v-for="tier in legend" :key="tier" class="chip">
        <span class="swatch" :style="{ background: `var(--speed-${tier.replace('.', '_')})` }" />
        {{
          tier === 'unknown'
            ? t('panel.speedUnknown')
            : tier.toUpperCase().replace('M', ' Mbps').replace('G', ' Gbps')
        }}
      </span>
      <span class="chip">
        <span class="swatch off" />
        {{ t('panel.portDown') }}
      </span>
    </div>

    <div
      v-if="hovered"
      class="tip card"
      role="tooltip"
      data-test="port-tip"
      :style="{ top: `${tip.top}px` }"
    >
      <div class="tip-head">
        <span
          class="dot"
          :style="{ background: hovered.up ? speedColor(hovered.speed_mbps) : undefined }"
        />
        <strong>{{ hovered.name }}</strong>
        <span v-if="hovered.description" class="muted">{{ hovered.description }}</span>
      </div>
      <dl>
        <dt>{{ t('panel.status') }}</dt>
        <dd>
          <template v-if="hovered.up">{{ portStatus(hovered) }}</template>
          <template v-else>{{ t('panel.portDown') }}</template>
        </dd>
        <template v-if="hovered.connector">
          <dt>{{ t('panel.connector') }}</dt>
          <dd>{{ connectorName(hovered) }}</dd>
        </template>
        <template v-if="hovered.media">
          <dt>{{ t('panel.media') }}</dt>
          <dd class="mono">{{ hovered.media }}</dd>
        </template>
        <template v-if="links[hovered.name]">
          <dt>{{ t('panel.connectedTo') }}</dt>
          <dd>{{ links[hovered.name]!.label }}</dd>
        </template>
        <template v-if="hovered.ips?.length">
          <dt>IP</dt>
          <dd class="mono">{{ hovered.ips.join(', ') }}</dd>
        </template>
        <template v-if="hovered.mac">
          <dt>MAC</dt>
          <dd class="mono">{{ hovered.mac }}</dd>
        </template>
        <template v-if="hovered.rx_bytes !== undefined">
          <dt>{{ t('panel.traffic') }}</dt>
          <dd>↓ {{ formatBytes(hovered.rx_bytes) }} · ↑ {{ formatBytes(hovered.tx_bytes) }}</dd>
        </template>
        <template v-if="hovered.rx_errors || hovered.tx_errors">
          <dt>{{ t('panel.errors') }}</dt>
          <dd class="err">{{ (hovered.rx_errors ?? 0) + (hovered.tx_errors ?? 0) }}</dd>
        </template>
      </dl>
    </div>
  </div>
</template>

<style scoped>
.faceplate-wrap {
  position: relative;
}
.faceplate {
  display: grid;
  gap: 6px;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background:
    linear-gradient(180deg, color-mix(in srgb, var(--surface) 92%, white 8%), var(--surface)),
    var(--surface);
  box-shadow:
    inset 0 1px 0 color-mix(in srgb, white 6%, transparent),
    0 2px 6px rgb(0 0 0 / 0.25);
  overflow-x: auto;
}
.row {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
.port {
  --c: var(--border-strong);
  position: relative;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 2px;
  width: 60px;
  padding: 6px 3px 4px;
  border: 1px solid transparent;
  border-radius: 6px;
  background: transparent;
  color: var(--text-muted);
  font: inherit;
  cursor: default;
}
.port.linked {
  cursor: pointer;
}
.port:hover,
.port:focus-visible {
  border-color: var(--border-strong);
  background: var(--surface-hover);
  outline: none;
}
.port svg {
  width: 44px;
  height: 36px;
}
.jack {
  fill: var(--bg-sunken);
  stroke: var(--c);
  stroke-width: 1.6;
}
.port.up .jack {
  fill: color-mix(in srgb, var(--c) 28%, var(--bg-sunken));
  filter: drop-shadow(0 0 3px color-mix(in srgb, var(--c) 55%, transparent));
}
.pin {
  fill: var(--border-strong);
}
.slot {
  fill: var(--bg);
  stroke: var(--border-strong);
  stroke-width: 1;
}
.port.up .slot {
  fill: color-mix(in srgb, var(--c) 55%, var(--bg));
  stroke: var(--c);
}
.latch {
  fill: none;
  stroke: var(--c);
  stroke-width: 1.6;
}
.port.up .pin {
  fill: color-mix(in srgb, var(--c) 70%, white 30%);
}
.led {
  position: absolute;
  top: 2px;
  right: 3px;
  width: 5px;
  height: 5px;
  border-radius: 50%;
  background: var(--border);
}
.port.up .led {
  background: var(--c);
  box-shadow: 0 0 5px var(--c);
}
.name {
  max-width: 100%;
  overflow: hidden;
  font-family: var(--mono);
  font-size: 11px;
  color: var(--text);
  text-overflow: ellipsis;
  white-space: nowrap;
}
.speed {
  font-size: 10.5px;
  font-weight: 600;
  color: var(--c);
}
.port:not(.up) .speed {
  color: var(--text-muted);
}
.legend {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin: 8px 2px 4px;
  font-size: 11.5px;
  color: var(--text-muted);
}
.chip {
  display: inline-flex;
  align-items: center;
  gap: 5px;
}
.swatch {
  width: 10px;
  height: 10px;
  border-radius: 3px;
}
.swatch.off {
  border: 1.5px solid var(--border-strong);
}
.tip {
  position: absolute;
  z-index: 30;
  left: 0;
  right: 0;
  padding: 10px 12px;
  box-shadow: var(--shadow);
  font-size: 12.5px;
  pointer-events: none;
}
.tip-head {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 6px;
}
.dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--border-strong);
}
dl {
  display: grid;
  grid-template-columns: auto 1fr;
  gap: 3px 10px;
  margin: 0;
}
dt {
  color: var(--text-muted);
}
dd {
  margin: 0;
  overflow-wrap: anywhere;
}
.err {
  color: var(--danger);
}
</style>
