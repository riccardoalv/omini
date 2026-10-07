<script setup lang="ts">
import { Check, ExternalLink, Pencil, X } from 'lucide-vue-next'
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
  /** Names the user gave the ports. */
  labels?: Record<string, string>
}>()
const emit = defineEmits<{
  select: [id: string]
  /** Rename a port (empty: back to the device's own name). */
  label: [port: string, label: string]
}>()
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

// Clicking a port opens its card: details and what can be done with it.
const selected = ref<string>()
const current = computed(() => props.ports.find((p) => p.name === selected.value))
const naming = ref(false)
const draft = ref('')
function toggle(p: Interface) {
  hovered.value = undefined
  naming.value = false
  selected.value = selected.value === p.name ? undefined : p.name
}
function startNaming(p: Interface) {
  draft.value = props.labels?.[p.name] ?? ''
  naming.value = true
}
function saveName(p: Interface) {
  emit('label', p.name, draft.value.trim())
  naming.value = false
}
function openLinked(p: Interface) {
  const other = props.links[p.name]
  if (other) emit('select', other.id)
}

/** What a port's tooltip and card list. */
function details(p: Interface) {
  const out: { key: string; label: string; value: string; mono?: boolean; err?: boolean }[] = []
  out.push({
    key: 'status',
    label: t('panel.status'),
    value: p.up ? portStatus(p) : t('panel.portDown'),
  })
  if (p.connector)
    out.push({ key: 'connector', label: t('panel.connector'), value: connectorName(p) })
  if (p.description && p.description !== props.labels?.[p.name])
    out.push({ key: 'description', label: t('panel.deviceName'), value: p.description })
  if (p.media) out.push({ key: 'media', label: t('panel.media'), value: p.media, mono: true })
  const other = props.links[p.name]
  if (other) out.push({ key: 'linked', label: t('panel.connectedTo'), value: other.label })
  if (p.ips?.length) out.push({ key: 'ips', label: 'IP', value: p.ips.join(', '), mono: true })
  if (p.mac) out.push({ key: 'mac', label: 'MAC', value: p.mac, mono: true })
  if (p.rx_bytes !== undefined)
    out.push({
      key: 'traffic',
      label: t('panel.traffic'),
      value: `↓ ${formatBytes(p.rx_bytes)} · ↑ ${formatBytes(p.tx_bytes)}`,
    })
  if (p.rx_errors || p.tx_errors)
    out.push({
      key: 'errors',
      label: t('panel.errors'),
      value: String((p.rx_errors ?? 0) + (p.tx_errors ?? 0)),
      err: true,
    })
  return out
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
          :class="{ up: p.up, linked: !!links[p.name], selected: selected === p.name }"
          :style="{ '--c': p.up ? speedColor(p.speed_mbps) : undefined }"
          :data-port="p.name"
          :aria-label="`${p.name}: ${p.up ? formatSpeed(p.speed_mbps) || t('panel.portUp') : t('panel.portDown')}`"
          :aria-pressed="selected === p.name"
          @mouseenter="!selected && show(p, $event)"
          @mouseleave="hovered = undefined"
          @click="toggle(p)"
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
          <span class="name" :title="labels?.[p.name] ? `${labels[p.name]} (${p.name})` : p.name">{{
            labels?.[p.name] || shortName(p.name)
          }}</span>
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
      v-if="hovered && !current"
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
        <strong>{{ labels?.[hovered.name] || hovered.name }}</strong>
        <span v-if="labels?.[hovered.name]" class="muted mono">{{ hovered.name }}</span>
      </div>
      <dl>
        <template v-for="d in details(hovered)" :key="d.key">
          <dt>{{ d.label }}</dt>
          <dd :class="{ mono: d.mono, err: d.err }">{{ d.value }}</dd>
        </template>
      </dl>
      <p class="tip-hint">{{ t('panel.portClickHint') }}</p>
    </div>

    <section v-if="current" class="port-card" data-test="port-card">
      <header>
        <span
          class="dot"
          :style="{ background: current.up ? speedColor(current.speed_mbps) : undefined }"
        />
        <form v-if="naming" class="name-form" @submit.prevent="saveName(current)">
          <input
            v-model="draft"
            class="input"
            maxlength="80"
            autofocus
            data-test="port-label-input"
            :placeholder="current.name"
            :aria-label="t('panel.portLabel')"
            @keydown.esc.prevent="naming = false"
          />
          <button class="btn icon" type="submit" :aria-label="t('common.save')">
            <Check :size="14" />
          </button>
        </form>
        <template v-else>
          <strong>{{ labels?.[current.name] || current.name }}</strong>
          <span v-if="labels?.[current.name]" class="muted mono">{{ current.name }}</span>
        </template>
        <button
          class="btn ghost icon close"
          type="button"
          :aria-label="t('common.close')"
          @click="selected = undefined"
        >
          <X :size="14" />
        </button>
      </header>
      <dl>
        <template v-for="d in details(current)" :key="d.key">
          <dt>{{ d.label }}</dt>
          <dd :class="{ mono: d.mono, err: d.err }">{{ d.value }}</dd>
        </template>
      </dl>
      <div class="card-actions">
        <button
          v-if="!naming"
          class="btn small"
          type="button"
          data-test="edit-port"
          @click="startNaming(current)"
        >
          <Pencil :size="13" />{{ t('panel.portLabel') }}
        </button>
        <button
          v-if="links[current.name]"
          class="btn small"
          type="button"
          data-test="open-linked"
          @click="openLinked(current)"
        >
          <ExternalLink :size="13" />{{
            t('panel.openLinked', { name: links[current.name]!.label })
          }}
        </button>
      </div>
    </section>
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
  gap: 4px;
}
.port {
  --c: var(--border-strong);
  position: relative;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 2px;
  flex: 0 1 60px;
  min-width: 36px;
  padding: 6px 2px 4px;
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
  width: 100%;
  max-width: 44px;
  height: auto;
  aspect-ratio: 36 / 30;
}
.port.selected {
  border-color: var(--accent);
  background: var(--accent-soft);
}
.tip-hint {
  margin: 6px 0 0;
  color: var(--text-muted);
  font-size: 11.5px;
}
.port-card {
  margin-top: 10px;
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface-hover);
  font-size: 12.5px;
}
.port-card header {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
}
.port-card .close {
  margin-left: auto;
  width: 24px;
  height: 24px;
}
.name-form {
  display: flex;
  flex: 1;
  gap: 4px;
}
.name-form .input {
  padding: 3px 6px;
  font-size: 12.5px;
}
.card-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 10px;
}
.card-actions .btn {
  display: inline-flex;
  align-items: center;
  gap: 5px;
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
